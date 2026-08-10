export type AttachedFile = { name: string; type: string; data: string };

export class APIError extends Error {
  constructor(readonly status: number) {
    super(String(status));
  }
}

const check = (response: Response) => {
  if (!response.ok) throw new APIError(response.status);
  return response;
};

const get = (url: string) =>
  fetch(url).then(check).then((response) => response.json());

const post = (url: string, body?: unknown) =>
  fetch(url, {
    method: "POST",
    headers: body === undefined ? undefined : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
    .then(check)
    .then((response) => (response.status === 204 ? null : response.json()));

export type EventStream = {
  onmessage?: (message: { data: string }) => void;
  onopen?: () => void;
  close: () => void;
};

class SessionEventStream implements EventStream {
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
          { signal: this.controller.signal, cache: "no-store" },
        );
        if (!response.ok || !response.body) throw new APIError(response.status);
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
  login: (password: string) => post("/api/login", { password }),
  logout: () => post("/api/logout"),
  sessions: () => get("/api/sessions"),
  create: (cwd?: string) => post("/api/sessions", cwd ? { cwd } : {}),
  open: (id: string) => post(`/api/sessions/${id}/open`),
  stop: (id: string) => fetch(`/api/sessions/${id}`, { method: "DELETE" }),
  remove: (id: string) =>
    fetch(`/api/sessions/${id}?purge=1`, { method: "DELETE" }),
  pin: (id: string, pinned: boolean) =>
    post(`/api/sessions/${id}/pin`, { pinned }),
  state: (id: string) => get(`/api/sessions/${id}/state`),
  history: (id: string, before: number) =>
    get(`/api/sessions/${id}/history?before=${before}`),
  catchup: (id: string, after: number) =>
    get(`/api/sessions/${id}/catchup?after=${after}`),
  file: (id: string, ref: string) =>
    fetch(`/api/sessions/${id}/file`, {
      headers: { "X-Orc-File-Ref": ref },
      cache: "no-store",
    }).then(check).then((response) => response.json()),
  models: () => get("/api/models"),
  dirs: (path?: string) =>
    get(`/api/dirs${path ? `?path=${encodeURIComponent(path)}` : ""}`),
  mkdir: (path: string) => post("/api/dirs", { path }),
  send: (id: string, text: string, files?: AttachedFile[]) =>
    post(`/api/sessions/${id}/input`, files?.length ? { text, files } : { text }),
  interrupt: (id: string) =>
    fetch(`/api/sessions/${id}/interrupt`, { method: "POST" }),
  events: (id: string, after: number) => new SessionEventStream(id, after),
};
