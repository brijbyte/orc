import {
  useCallback,
  useEffect,
  useRef,
  useState,
  useSyncExternalStore,
} from "react";
import { CircleAlert, LoaderCircle, Paperclip } from "lucide-react";
import { Navigate, useOutletContext, useParams } from "react-router";
import * as store from "../lib/store";
import { api } from "../lib/api";
import { revalidateSoon } from "../lib/revalidate";
import { modShortcut, overlayOpen } from "../lib/shortcuts";
import type { Block, ComposerAttachment, SessionRow } from "../lib/types";
import { useSettings } from "../settings/SettingsContext";

const fileMax = 16 << 20; // per-file cap, matches the server's request cap
const hasFiles = (dt: DataTransfer) => dt.types.includes("Files");

// a failed turn commits nothing, so its error notice can be retried as-is
const failed = (b?: Block) => b?.kind === "notice" && b.text.startsWith("❌");
import { Transcript } from "./Transcript";
import { InputBar } from "./InputBar";
import { StatusBar } from "./StatusBar";
import { FileDrawer } from "./FileDrawer";
import { GitDrawer } from "./GitDrawer";
import { CwdPicker } from "../sidebar/CwdPicker";
import s from "./SessionView.module.css";

export type SessionOutletContext = {
  session: SessionRow | null;
  openTerminal: () => void;
  toggleTerminal: () => void;
};

// SessionRoute adapts /s/:sid: the loader has already seeded the store,
// the key remounts the view (and its local state) per session.
export function SessionRoute() {
  const { sid = "" } = useParams();
  const context = useOutletContext<SessionOutletContext>();
  // /open may resolve sid to a newer chain member; the URL must follow so
  // every session-scoped call targets an id the server can resolve.
  const canonical = useSyncExternalStore(
    useCallback((fn: () => void) => store.subscribe(sid, fn), [sid]),
    useCallback(() => store.snapshot(sid).canonical, [sid]),
  );
  if (canonical && canonical !== sid)
    return <Navigate to={`/s/${canonical}`} replace />;
  return <SessionView key={sid} sid={sid} context={context} />;
}

// SessionView renders the active session from the store. Only the active
// view is mounted; streams live in store.ts and keep running across
// switches (App calls store.ensure for every open tab).
export function SessionView({
  sid,
  context,
}: {
  sid: string;
  context: SessionOutletContext;
}) {
  const { models } = useSettings();
  const { openTerminal, toggleTerminal } = context;
  const selected = context.session;
  const [files, setFiles] = useState<ComposerAttachment[]>([]);
  const [attachmentError, setAttachmentError] = useState("");
  const [dragging, setDragging] = useState(false);
  const [complete, setComplete] = useState(false);
  const [compacting, setCompacting] = useState(false);
  const [cwdOpen, setCwdOpen] = useState(false);
  const [gitOpen, setGitOpen] = useState(false);
  const [gitRequest, setGitRequest] = useState(0);
  const [draft, setDraft] = useState<{ text: string; request: number }>();
  const [file, setFile] = useState<{
    path: string;
    ref: string;
    line?: number;
    request: number;
  } | null>(null);
  const dragDepth = useRef(0);
  const wasBusy = useRef(false);

  const { blocks, busy, status, err, hasMore, loadingOlder } =
    useSyncExternalStore(
      useCallback((fn: () => void) => store.subscribe(sid, fn), [sid]),
      useCallback(() => store.snapshot(sid), [sid]),
    );

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (overlayOpen()) return;
      if (modShortcut(event, "g")) {
        event.preventDefault();
        setGitOpen(true);
      } else if (modShortcut(event, "`")) {
        event.preventDefault();
        toggleTerminal();
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [toggleTerminal]);

  useEffect(() => {
    const didComplete = !busy && wasBusy.current;
    if (busy) setComplete(false);
    else if (didComplete) setComplete(true);
    wasBusy.current = busy;
    if (!didComplete) return;
    if (compacting) {
      setCompacting(false);
      revalidateSoon();
    }
    const timer = window.setTimeout(() => setComplete(false), 1400);
    return () => clearTimeout(timer);
  }, [busy, compacting]);

  const addFiles = (list: FileList | null) => {
    if (!list) return;
    const ok = [...list].filter((file) => file.size <= fileMax);
    setAttachmentError(
      ok.length < list.length ? "Files over 16 MB were skipped." : "",
    );
    setFiles((current) => {
      const keys = new Set(
        current
          .filter((file): file is File => file instanceof File)
          .map((file) => `${file.name}:${file.size}:${file.lastModified}`),
      );
      const added = ok.filter((file) => {
        const key = `${file.name}:${file.size}:${file.lastModified}`;
        if (keys.has(key)) return false;
        keys.add(key);
        return true;
      });
      return added.length ? [...current, ...added] : current;
    });
  };

  const openFile = (path: string, ref: string, line?: number) => {
    setFile((current) => ({
      path,
      ref,
      line,
      request: (current?.request ?? 0) + 1,
    }));
  };

  return (
    <div
      className={s.app}
      data-dragging={dragging || undefined}
      onDragEnter={(e) => {
        if (!hasFiles(e.dataTransfer)) return;
        e.preventDefault();
        if (++dragDepth.current === 1) setDragging(true);
      }}
      onDragOver={(e) => {
        if (!hasFiles(e.dataTransfer)) return;
        e.preventDefault();
        e.dataTransfer.dropEffect = "copy";
      }}
      onDragLeave={(e) => {
        if (!hasFiles(e.dataTransfer)) return;
        if (--dragDepth.current === 0) setDragging(false);
      }}
      onDrop={(e) => {
        if (!hasFiles(e.dataTransfer)) return;
        e.preventDefault();
        dragDepth.current = 0;
        setDragging(false);
        addFiles(e.dataTransfer.files);
      }}
    >
      {err ? (
        <div className={s.dead}>
          <CircleAlert size={17} strokeWidth={1.8} aria-hidden />
          {err}
        </div>
      ) : blocks === null ? (
        <div className={s.loader}>
          <LoaderCircle size={17} strokeWidth={1.8} aria-hidden />
          loading session…
        </div>
      ) : (
        <>
          <Transcript
            sid={sid}
            blocks={blocks}
            hasMore={hasMore}
            loadingOlder={loadingOlder}
            onOpenFile={openFile}
            onRetry={
              !busy && failed(blocks[blocks.length - 1])
                ? () => void api.retry(sid).catch(() => {})
                : undefined
            }
          />
          <InputBar
            sid={sid}
            busy={busy}
            complete={complete}
            files={files}
            setFiles={setFiles}
            addFiles={addFiles}
            attachmentError={attachmentError}
            draft={draft}
          />
          <StatusBar
            sid={sid}
            status={status}
            models={models}
            compactDisabled={busy || compacting || blocks.length === 0}
            canWake={!busy && !!selected?.routine && !!selected.wake}
            cwdDisabled={busy}
            onCompact={() => {
              setCompacting(true);
              void api.compact(sid).catch(() => setCompacting(false));
            }}
            onWake={() =>
              void api
                .wake(sid)
                .then(revalidateSoon)
                .catch(() => {})
            }
            onChangeCwd={() => setCwdOpen(true)}
            onOpenGit={() => setGitOpen(true)}
            onOpenTerminal={openTerminal}
          />
        </>
      )}
      <CwdPicker
        open={cwdOpen}
        start={selected?.cwd ?? ""}
        onCancel={() => setCwdOpen(false)}
        onPick={(path) => {
          void api
            .cwd(sid, path)
            .then(() => {
              setCwdOpen(false);
              revalidateSoon();
            })
            .catch(() => {});
        }}
      />
      <FileDrawer
        sid={sid}
        path={file?.path ?? ""}
        fileRef={file?.ref ?? ""}
        line={file?.line}
        request={file?.request ?? 0}
        onClose={() => setFile(null)}
        onSaved={() => setGitRequest((value) => value + 1)}
      />
      <GitDrawer
        sid={sid}
        open={gitOpen}
        request={(blocks?.length ?? 0) + gitRequest}
        onClose={() => setGitOpen(false)}
        onOpenFile={openFile}
        onAddContext={(context, text) => {
          setFiles((current) => [...current, context]);
          setDraft((current) => ({
            text,
            request: (current?.request ?? 0) + 1,
          }));
          setGitOpen(false);
        }}
      />
      <div
        className={s.dropzone}
        data-active={dragging || undefined}
        aria-hidden={!dragging}
      >
        <Paperclip size={18} strokeWidth={1.8} aria-hidden />
        drop to attach
      </div>
    </div>
  );
}
