import { api } from "./api";
import { apply } from "./events";
import type { Block, Ev } from "./types";

// Per-session event streams and block state, held outside the render tree:
// every open tab keeps streaming while only the active view is mounted.

export type SessionState = {
  blocks: Block[] | null;
  busy: boolean;
  status: string;
  err: string;
};

type Entry = {
  state: SessionState;
  es?: EventSource;
  subs: Set<() => void>;
  opened: boolean;
};

const entries = new Map<string, Entry>();
const initial: SessionState = { blocks: null, busy: false, status: "", err: "" };

function entry(sid: string): Entry {
  let e = entries.get(sid);
  if (!e) {
    e = { state: initial, subs: new Set(), opened: false };
    entries.set(sid, e);
  }
  return e;
}

function set(e: Entry, patch: Partial<SessionState>) {
  e.state = { ...e.state, ...patch };
  e.subs.forEach((fn) => fn());
}

// ensure revives the session on the server and starts its stream, once.
export function ensure(sid: string, onOpened?: () => void) {
  const e = entry(sid);
  if (e.opened) return;
  e.opened = true;
  let lastID = 0;
  const onEvent = (ev: Ev) => {
    lastID = ev.id;
    if (ev.type === "busy") set(e, { busy: ev.data.busy });
    else if (ev.type === "status") set(e, { status: ev.data.text });
    else set(e, { blocks: apply([...(e.state.blocks ?? [])], ev) });
  };
  api
    .open(sid)
    .then(() => {
      onOpened?.();
      return api.state(sid);
    })
    .then((state) => {
      const seed: Block[] = [];
      for (const ev of state.events ?? []) {
        lastID = ev.id;
        if (ev.type !== "busy" && ev.type !== "status") apply(seed, ev);
      }
      set(e, { blocks: seed, busy: !!state.busy, status: state.status ?? "" });
      e.es = api.events(sid, lastID);
      e.es.onmessage = (m) => onEvent(JSON.parse(m.data));
    })
    .catch(() => set(e, { err: "cannot open this session" }));
}

// drop closes the stream and forgets the state (tab closed, not the runtime).
export function drop(sid: string) {
  entries.get(sid)?.es?.close();
  entries.delete(sid);
  scrolls.delete(sid);
}

// scroll positions per session, surviving view unmounts
const scrolls = new Map<string, { top: number; stick: boolean }>();

export function saveScroll(sid: string, top: number, stick: boolean) {
  scrolls.set(sid, { top, stick });
}

export function getScroll(sid: string) {
  return scrolls.get(sid);
}

export function subscribe(sid: string, fn: () => void): () => void {
  const e = entry(sid);
  e.subs.add(fn);
  return () => e.subs.delete(fn);
}

export function snapshot(sid: string): SessionState {
  return entry(sid).state;
}
