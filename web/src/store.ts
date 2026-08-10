import { api, type EventStream } from "./api";
import { apply } from "./events";
import type { Block, Ev } from "./types";

// Per-session event streams and block state, held outside the render tree:
// every open tab keeps streaming while only the active view is mounted.

export type SessionState = {
  blocks: Block[] | null;
  busy: boolean;
  status: string;
  err: string;
  hasMore: boolean;
  loadingOlder: boolean;
};

type Entry = {
  state: SessionState;
  events: Ev[];
  before: number;
  es?: EventStream;
  subs: Set<() => void>;
  ready?: Promise<void>; // resolves once the seed from /state is applied
};

const entries = new Map<string, Entry>();
const initial: SessionState = {
  blocks: null,
  busy: false,
  status: "",
  err: "",
  hasMore: false,
  loadingOlder: false,
};

function entry(sid: string): Entry {
  let e = entries.get(sid);
  if (!e) {
    e = { state: initial, events: [], before: 0, subs: new Set() };
    entries.set(sid, e);
  }
  return e;
}

function set(e: Entry, patch: Partial<SessionState>) {
  e.state = { ...e.state, ...patch };
  e.subs.forEach((fn) => fn());
}

// ensure revives the session on the server and starts its stream, once.
// The returned promise settles when the seed is applied (never rejects:
// failures land in state.err), so route loaders can await it.
export function ensure(sid: string, onOpened?: () => void): Promise<void> {
  const e = entry(sid);
  if (e.ready) return e.ready;
  let lastID = 0;
  const pending = new Map<number, Ev>();
  const commit = (ev: Ev) => {
    lastID = ev.id;
    e.events.push(ev);
    if (ev.type === "busy") set(e, { busy: ev.data.busy });
    else if (ev.type === "status") set(e, { status: ev.data.text });
    else set(e, { blocks: apply([...(e.state.blocks ?? [])], ev) });
  };
  const onEvent = (ev: Ev) => {
    if (ev.id <= lastID) return;
    pending.set(ev.id, ev);
    for (let next = pending.get(lastID + 1); next; next = pending.get(lastID + 1)) {
      pending.delete(next.id);
      commit(next);
    }
  };
  let catchingUp = false;
  const catchUp = () => {
    if (catchingUp) return;
    catchingUp = true;
    api
      .catchup(sid, lastID)
      .then((page) => (page.events ?? []).forEach(onEvent))
      .catch(() => {})
      .finally(() => {
        catchingUp = false;
      });
  };
  e.ready = api
    .open(sid)
    .then(() => {
      onOpened?.();
      return api.state(sid);
    })
    .then((state) => {
      const seed: Block[] = [];
      e.events = state.events ?? [];
      e.before = state.before ?? 0;
      lastID = state.last_id ?? 0;
      for (const ev of e.events) {
        if (ev.type !== "busy" && ev.type !== "status") apply(seed, ev);
      }
      set(e, {
        blocks: seed,
        busy: !!state.busy,
        status: state.status ?? "",
        hasMore: !!state.has_more,
      });
      e.es = api.events(sid, lastID);
      e.es.onmessage = (m) => onEvent(JSON.parse(m.data));
      let opened = false;
      e.es.onopen = () => {
        if (opened) catchUp();
        opened = true;
      };
    })
    .catch(() => set(e, { err: "cannot open this session" }));
  return e.ready;
}

export function loadOlder(sid: string): Promise<void> {
  const e = entry(sid);
  if (!e.state.hasMore || e.state.loadingOlder || !e.before) return Promise.resolve();
  set(e, { loadingOlder: true });
  return api
    .history(sid, e.before)
    .then((page) => {
      const byID = new Map<number, Ev>();
      for (const ev of [...(page.events ?? []), ...e.events]) byID.set(ev.id, ev);
      e.events = [...byID.values()].sort((a, b) => a.id - b.id);
      e.before = page.before ?? e.before;
      const blocks: Block[] = [];
      for (const ev of e.events) apply(blocks, ev);
      set(e, {
        blocks,
        hasMore: !!page.has_more,
        loadingOlder: false,
      });
    })
    .catch(() => set(e, { loadingOlder: false }));
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
  return entries.get(sid)?.state ?? initial;
}
