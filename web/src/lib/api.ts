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
  fetch(url)
    .then(check)
    .then((response) => response.json());

const post = (url: string, body?: unknown) =>
  fetch(url, {
    method: "POST",
    headers:
      body === undefined ? undefined : { "Content-Type": "application/json" },
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
  browse: (id: string, path?: string) =>
    get(
      `/api/sessions/${id}/browse${path ? `?path=${encodeURIComponent(path)}` : ""}`,
    ),
  file: (id: string, ref: string) =>
    fetch(`/api/sessions/${id}/file`, {
      headers: { "X-Orc-File-Ref": ref },
      cache: "no-store",
    })
      .then(check)
      .then((response) => response.json()),
  saveFile: (id: string, ref: string, revision: string, content: string) =>
    fetch(`/api/sessions/${id}/file`, {
      method: "PUT",
      headers: {
        "Content-Type": "text/plain; charset=utf-8",
        "X-Orc-File-Ref": ref,
        "X-Orc-File-Revision": revision,
      },
      body: content,
    })
      .then(check)
      .then((response) => response.json()),
  gitStatus: (id: string) => get(`/api/sessions/${id}/git/status`),
  gitCompare: (id: string, base: string) =>
    get(`/api/sessions/${id}/git/compare?base=${encodeURIComponent(base)}`),
  gitDiff: (id: string, path: string, base = "", mode = "worktree") => {
    const query = new URLSearchParams({ path });
    if (base) query.set("base", base);
    else query.set("mode", mode);
    return get(`/api/sessions/${id}/git/diff?${query}`);
  },
  gitStage: (id: string, paths: string[], hunks?: number[], hash?: string) =>
    post(`/api/sessions/${id}/git/stage`, { paths, hunks, hash }),
  gitUnstage: (id: string, paths: string[], hunks?: number[], hash?: string) =>
    post(`/api/sessions/${id}/git/unstage`, { paths, hunks, hash }),
  gitCommit: (id: string, message: string) =>
    post(`/api/sessions/${id}/git/commit`, { message }),
  gitSwitch: (id: string, name: string) =>
    post(`/api/sessions/${id}/git/switch`, { name }),
  gitCreateBranch: (id: string, name: string) =>
    post(`/api/sessions/${id}/git/create-branch`, { name }),
  gitDiscard: (id: string, paths: string[], hunks?: number[], hash?: string) =>
    post(`/api/sessions/${id}/git/discard`, {
      paths,
      hunks,
      hash,
      confirm: true,
    }),
  gitRemove: (id: string, paths: string[]) =>
    post(`/api/sessions/${id}/git/remove`, { paths, confirm: true }),
  gitUndoDiscard: (id: string) => post(`/api/sessions/${id}/git/undo-discard`),
  models: () => get("/api/models"),
  dirs: (path?: string) =>
    get(`/api/dirs${path ? `?path=${encodeURIComponent(path)}` : ""}`),
  mkdir: (path: string) => post("/api/dirs", { path }),
  send: (
    id: string,
    text: string,
    files: AttachedFile[] = [],
    paths: string[] = [],
  ) =>
    post(`/api/sessions/${id}/input`, {
      text,
      ...(files.length ? { files } : {}),
      ...(paths.length ? { paths } : {}),
    }),
  compact: (id: string) => post(`/api/sessions/${id}/compact`),
  retry: (id: string) => post(`/api/sessions/${id}/retry`),
  interrupt: (id: string) =>
    fetch(`/api/sessions/${id}/interrupt`, { method: "POST" }),
  events: (id: string, after: number) => new SessionEventStream(id, after),
};
