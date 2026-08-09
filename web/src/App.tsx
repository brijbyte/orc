import {
  Component,
  Suspense,
  use,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { api } from "./api";
import { apply } from "./events";
import type { Block, Ev, Model } from "./types";
import { Transcript } from "./Transcript";
import { InputBar } from "./InputBar";
import { StatusBar } from "./StatusBar";

// One-shot init: fires at module load so the fetch overlaps the first
// render; Session suspends on it.
const initPromise = Promise.all([
  api.state(),
  api.models().catch(() => ({ models: [] as Model[] })),
]);

// Session owns the live state: the event stream feeding the block list,
// busy/status flags, and the model list for the status bar.
function Session() {
  const [state, modelsData] = use(initPromise);

  const folded = useMemo(() => {
    const blocks: Block[] = [];
    let lastID = 0;
    for (const ev of state.events ?? []) {
      lastID = ev.id;
      if (ev.type !== "busy" && ev.type !== "status") apply(blocks, ev);
    }
    return { blocks, lastID };
  }, [state]);

  const [blocks, setBlocks] = useState<Block[]>(folded.blocks);
  const [busy, setBusy] = useState(!!state.busy);
  const [status, setStatus] = useState(state.status ?? "");
  const [models] = useState<Model[]>(modelsData.models ?? []);
  const lastID = useRef(folded.lastID);

  const onEvent = useCallback((ev: Ev) => {
    lastID.current = ev.id;
    if (ev.type === "busy") setBusy(ev.data.busy);
    else if (ev.type === "status") setStatus(ev.data.text);
    else setBlocks((prev) => apply([...prev], ev));
  }, []);

  useEffect(() => {
    const es = api.events(lastID.current);
    es.onmessage = (m) => onEvent(JSON.parse(m.data));
    return () => es.close();
  }, [onEvent]);

  return (
    <div className="app">
      <Transcript blocks={blocks} />
      <InputBar busy={busy} />
      <StatusBar status={status} models={models} />
    </div>
  );
}

// Boundary catches a rejected init (bad token, server gone).
class Boundary extends Component<{ children: ReactNode }, { err: boolean }> {
  state = { err: false };
  static getDerivedStateFromError() {
    return { err: true };
  }
  render() {
    if (this.state.err)
      return (
        <div className="dead">
          🧌 cannot reach orc — is it still running? (check the URL token)
        </div>
      );
    return this.props.children;
  }
}

export default function App() {
  return (
    <Boundary>
      <Suspense fallback={<div className="loader">🧌 loading session…</div>}>
        <Session />
      </Suspense>
    </Boundary>
  );
}
