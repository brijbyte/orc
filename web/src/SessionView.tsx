import { useCallback, useRef, useState, useSyncExternalStore } from "react";
import * as store from "./store";

const fileMax = 16 << 20; // per-file cap, matches the server's request cap
import type { Model } from "./types";
import { Transcript } from "./Transcript";
import { InputBar } from "./InputBar";
import { StatusBar } from "./StatusBar";

// SessionView renders the active session from the store. Only the active
// view is mounted; streams live in store.ts and keep running across
// switches (App calls store.ensure for every open tab).
export function SessionView({ sid, models }: { sid: string; models: Model[] }) {
  const [files, setFiles] = useState<File[]>([]);
  const [dragging, setDragging] = useState(false);
  const dragDepth = useRef(0);

  const { blocks, busy, status, err } = useSyncExternalStore(
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
      className="app"
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
        <div className="dead">🧌 {err}</div>
      ) : blocks === null ? (
        <div className="loader">🧌 loading session…</div>
      ) : (
        <>
          <Transcript sid={sid} blocks={blocks} />
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
      {dragging && <div className="dropzone">📎 drop to attach</div>}
    </div>
  );
}
