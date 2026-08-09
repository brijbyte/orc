import { useCallback, useRef, useState, useSyncExternalStore } from "react";
import { useOutletContext, useParams } from "react-router";
import * as store from "./store";

const fileMax = 16 << 20; // per-file cap, matches the server's request cap
import type { Model } from "./types";
import { Transcript } from "./Transcript";
import { InputBar } from "./InputBar";
import { StatusBar } from "./StatusBar";
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
  const dragDepth = useRef(0);

  const { blocks, busy, status, err, hasMore, loadingOlder } = useSyncExternalStore(
    useCallback((fn: () => void) => store.subscribe(sid, fn), [sid]),
    useCallback(() => store.snapshot(sid), [sid]),
  );

  const addFiles = (list: FileList | null) => {
    if (!list) return;
    const ok = [...list].filter((f) => f.size <= fileMax);
    if (ok.length < list.length) alert("files over 16 MB were skipped");
    if (ok.length) setFiles((prev) => [...prev, ...ok]);
  };

  return (
    <div
      className={s.app}
      onDragEnter={(e) => {
        e.preventDefault();
        if (++dragDepth.current === 1) setDragging(true);
      }}
      onDragOver={(e) => e.preventDefault()}
      onDragLeave={() => {
        if (--dragDepth.current === 0) setDragging(false);
      }}
      onDrop={(e) => {
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
          />
          <InputBar
            sid={sid}
            busy={busy}
            files={files}
            setFiles={setFiles}
            addFiles={addFiles}
          />
          <StatusBar sid={sid} status={status} models={models} />
        </>
      )}
      <div className={s.dropzone} data-active={dragging || undefined} aria-hidden={!dragging}>
        📎 drop to attach
      </div>
    </div>
  );
}
