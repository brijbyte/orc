import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "./api";
import { apply } from "./events";
import type { Block, Ev, Model } from "./types";
import { Transcript } from "./Transcript";
import { InputBar } from "./InputBar";
import { StatusBar } from "./StatusBar";

// App owns the session state: the event stream feeding the block list,
// busy/status flags, and the model list for the status bar.
export default function App() {
  const [blocks, setBlocks] = useState<Block[]>([]);
  const [busy, setBusy] = useState(false);
  const [status, setStatus] = useState("");
  const [dead, setDead] = useState(false);
  const [models, setModels] = useState<Model[]>([]);
  const lastID = useRef(0);

  const onEvent = useCallback((ev: Ev) => {
    lastID.current = ev.id;
    if (ev.type === "busy") setBusy(ev.data.busy);
    else if (ev.type === "status") setStatus(ev.data.text);
    else setBlocks((prev) => apply([...prev], ev));
  }, []);

  useEffect(() => {
    let es: EventSource | null = null;
    api
      .state()
      .then((s) => {
        const bs: Block[] = [];
        for (const ev of s.events ?? []) {
          lastID.current = ev.id;
          if (ev.type !== "busy" && ev.type !== "status") apply(bs, ev);
        }
        setBlocks(bs);
        setBusy(s.busy);
        setStatus(s.status);
        es = api.events(lastID.current);
        es.onmessage = (m) => onEvent(JSON.parse(m.data));
      })
      .catch(() => setDead(true));
    api
      .models()
      .then((d) => setModels(d.models ?? []))
      .catch(() => {});
    return () => es?.close();
  }, [onEvent]);

  if (dead)
    return (
      <div className="dead">
        🧌 cannot reach orc — is it still running? (check the URL token)
      </div>
    );

  return (
    <div className="app">
      <Transcript blocks={blocks} />
      <InputBar busy={busy} />
      <StatusBar status={status} models={models} />
    </div>
  );
}
