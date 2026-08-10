import {
  useCallback,
  useEffect,
  useRef,
  useState,
  useSyncExternalStore,
} from "react";
import { useOutletContext, useParams } from "react-router";
import * as store from "../lib/store";
import { api } from "../lib/api";
import { revalidateSoon } from "../lib/revalidate";
import type { Block } from "../lib/types";

const fileMax = 16 << 20; // per-file cap, matches the server's request cap
const hasFiles = (dt: DataTransfer) => dt.types.includes("Files");

// a failed turn commits nothing, so its error notice can be retried as-is
const failed = (b?: Block) => b?.kind === "notice" && b.text.startsWith("❌");
import type { Model } from "../lib/types";
import { Transcript } from "./Transcript";
import { InputBar } from "./InputBar";
import { StatusBar } from "./StatusBar";
import { FileDrawer } from "./FileDrawer";
import { GitDrawer } from "./GitDrawer";
import s from "./SessionView.module.css";

// SessionRoute adapts /s/:sid: the loader has already seeded the store,
// the key remounts the view (and its local state) per session.
export function SessionRoute() {
  const { sid = "" } = useParams();
  const models = useOutletContext<Model[]>();
  return <SessionView key={sid} sid={sid} models={models} />;
}

// SessionView renders the active session from the store. Only the active
// view is mounted; streams live in store.ts and keep running across
// switches (App calls store.ensure for every open tab).
export function SessionView({ sid, models }: { sid: string; models: Model[] }) {
  const [files, setFiles] = useState<File[]>([]);
  const [dragging, setDragging] = useState(false);
  const [complete, setComplete] = useState(false);
  const [compacting, setCompacting] = useState(false);
  const [gitOpen, setGitOpen] = useState(false);
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
    const ok = [...list].filter((f) => f.size <= fileMax);
    if (ok.length < list.length) alert("files over 16 MB were skipped");
    if (ok.length) setFiles((prev) => [...prev, ...ok]);
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
        <div className={s.dead}>🧌 {err}</div>
      ) : blocks === null ? (
        <div className={s.loader}>🧌 loading session…</div>
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
            draft={draft}
          />
          <StatusBar
            sid={sid}
            status={status}
            models={models}
            compactDisabled={busy || compacting || blocks.length === 0}
            onCompact={() => {
              setCompacting(true);
              void api.compact(sid).catch(() => setCompacting(false));
            }}
            onOpenGit={() => setGitOpen(true)}
          />
        </>
      )}
      <FileDrawer
        sid={sid}
        path={file?.path ?? ""}
        fileRef={file?.ref ?? ""}
        line={file?.line}
        request={file?.request ?? 0}
        onClose={() => setFile(null)}
      />
      <GitDrawer
        sid={sid}
        open={gitOpen}
        request={blocks?.length ?? 0}
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
        📎 drop to attach
      </div>
    </div>
  );
}
