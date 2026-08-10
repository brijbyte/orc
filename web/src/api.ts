export type AttachedFile = { name: string; type: string; data: string };

// The hash carries only the auth token ("#<token>"), so it never reaches
// the server; the active session lives in the path (/s/:sid). Legacy
// "#<token>/<session>" links still parse — App migrates them.
const token = window.location.hash.slice(1).split("/")[0];

const auth = { Authorization: `Bearer ${token}` };

const get = (url: string) =>
  fetch(url, { headers: auth }).then((r) => {
    if (!r.ok) throw new Error(String(r.status));
    return r.json();
  });

const post = (url: string, body?: unknown) =>
  fetch(url, {
    method: "POST",
    headers: auth,
    body: body === undefined ? undefined : JSON.stringify(body),
  }).then((r) => {
    if (!r.ok) throw new Error(String(r.status));
    return r.status === 204 ? null : r.json();
  });

export type EventStream = {
  onmessage?: (message: { data: string }) => void;
  onopen?: () => void;
  close: () => void;
};

class AuthEventStream implements EventStream {
  onmessage?: (message: { data: string }) => void;
  onopen?: () => void;
  private controller = new AbortController();

  constructor(
    private id: string,
    private cursor: number,
  ) {
    queueMicrotask(() => void this.run());
  }

  close = () => this.controller.abort();

  private async run() {
    while (!this.controller.signal.aborted) {
      try {
        const response = await fetch(
          `/api/sessions/${this.id}/events?after=${this.cursor}`,
          { headers: auth, signal: this.controller.signal, cache: "no-store" },
        );
        if (!response.ok || !response.body) throw new Error(String(response.status));
        this.onopen?.();
        const reader = response.body.getReader();
        const decoder = new TextDecoder();
        let buffer = "";
        for (;;) {
          const { value, done } = await reader.read();
          if (done) break;
          buffer += decoder.decode(value, { stream: true });
          let end = buffer.indexOf("\n\n");
          while (end >= 0) {
            const frame = buffer.slice(0, end);
            buffer = buffer.slice(end + 2);
            const data = frame
              .split("\n")
              .filter((line) => line.startsWith("data: "))
              .map((line) => line.slice(6))
              .join("\n");
            if (data) {
              const event = JSON.parse(data);
              if (typeof event.id === "number") this.cursor = event.id;
              this.onmessage?.({ data });
            }
            end = buffer.indexOf("\n\n");
          }
        }
      } catch {
        if (this.controller.signal.aborted) return;
      }
      await new Promise((resolve) => setTimeout(resolve, 1000));
    }
  }
}

export const api = {
  sessions: () => get("/api/sessions"),
  create: (cwd?: string) => post("/api/sessions", cwd ? { cwd } : {}),
  open: (id: string) => post(`/api/sessions/${id}/open`),
  stop: (id: string) =>
    fetch(`/api/sessions/${id}`, { method: "DELETE", headers: auth }),
  remove: (id: string) =>
    fetch(`/api/sessions/${id}?purge=1`, { method: "DELETE", headers: auth }),
  pin: (id: string, pinned: boolean) =>
    post(`/api/sessions/${id}/pin`, { pinned }),
  state: (id: string) => get(`/api/sessions/${id}/state`),
  history: (id: string, before: number) =>
    get(`/api/sessions/${id}/history?before=${before}`),
  catchup: (id: string, after: number) =>
    get(`/api/sessions/${id}/catchup?after=${after}`),
  file: (id: string, ref: string) =>
    fetch(`/api/sessions/${id}/file`, {
      headers: { ...auth, "X-Orc-File-Ref": ref },
      cache: "no-store",
    }).then((response) => {
      if (!response.ok) throw new Error(String(response.status));
      return response.json();
    }),
  models: () => get("/api/models"),
  dirs: (path?: string) =>
    get(`/api/dirs${path ? `?path=${encodeURIComponent(path)}` : ""}`),
  mkdir: (path: string) => post("/api/dirs", { path }),
  send: (id: string, text: string, files?: AttachedFile[]) =>
    post(`/api/sessions/${id}/input`, files?.length ? { text, files } : { text }),
  interrupt: (id: string) =>
    fetch(`/api/sessions/${id}/interrupt`, { method: "POST", headers: auth }),
  events: (id: string, after: number) => new AuthEventStream(id, after),
};

// legacySession reads a session id from an old-style "#token/session" hash.
export function legacySession(): string {
  return window.location.hash.slice(1).split("/")[1] ?? "";
}

// tokenHash is the fragment every in-app navigation must carry along.
export function tokenHash(): string {
  return token ? "#" + token : "";
}
