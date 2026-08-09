import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "./api";

const fileMax = 16 << 20; // per-file cap, matches the server's request cap
import { apply } from "./events";
import type { Block, Ev, Model } from "./types";
import { Transcript } from "./Transcript";
import { InputBar } from "./InputBar";
import { StatusBar } from "./StatusBar";

// SessionView drives one open session: revive it on the server, seed the
// block list from /state, then follow the SSE stream. Stays mounted (and
// connected) while hidden so switching back is instant.
export function SessionView({
  sid,
  visible,
  models,
  onOpened,
}: {
  sid: string;
  visible: boolean;
  models: Model[];
  onOpened?: () => void;
}) {
  const [blocks, setBlocks] = useState<Block[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [status, setStatus] = useState("");
  const [err, setErr] = useState("");
  const [files, setFiles] = useState<File[]>([]);
  const [dragging, setDragging] = useState(false);
  const lastID = useRef(0);
  const dragDepth = useRef(0);

  const addFiles = (list: FileList | null) => {
    if (!list) return;
    const ok = [...list].filter((f) => f.size <= fileMax);
    if (ok.length < list.length) alert("files over 16 MB were skipped");
    if (ok.length) setFiles((prev) => [...prev, ...ok]);
  };

  const onEvent = useCallback((ev: Ev) => {
    lastID.current = ev.id;
    if (ev.type === "busy") setBusy(ev.data.busy);
    else if (ev.type === "status") setStatus(ev.data.text);
    else setBlocks((prev) => apply([...(prev ?? [])], ev));
  }, []);

  useEffect(() => {
    let es: EventSource | null = null;
    let gone = false;
    api
      .open(sid)
      .then(() => {
        onOpened?.(); // the session just went live; refresh the sidebar
        return api.state(sid);
      })
      .then((state) => {
        if (gone) return;
        const seed: Block[] = [];
        for (const ev of state.events ?? []) {
          lastID.current = ev.id;
          if (ev.type !== "busy" && ev.type !== "status") apply(seed, ev);
        }
        setBlocks(seed);
        setBusy(!!state.busy);
        setStatus(state.status ?? "");
        es = api.events(sid, lastID.current);
        es.onmessage = (m) => onEvent(JSON.parse(m.data));
      })
      .catch(() => {
        if (!gone) setErr("cannot open this session");
      });
    return () => {
      gone = true;
      es?.close();
    };
  }, [sid, onEvent, onOpened]);

  return (
    <div
      className="app"
      style={visible ? undefined : { display: "none" }}
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
          <Transcript blocks={blocks} />
          <InputBar
            sid={sid}
            busy={busy}
            active={visible}
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
