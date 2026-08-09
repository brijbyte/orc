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
  models: () => get("/api/models"),
  dirs: (path?: string) =>
    get(`/api/dirs${path ? `?path=${encodeURIComponent(path)}` : ""}`),
  mkdir: (path: string) => post("/api/dirs", { path }),
  send: (id: string, text: string, files?: AttachedFile[]) =>
    post(`/api/sessions/${id}/input`, files?.length ? { text, files } : { text }),
  interrupt: (id: string) =>
    fetch(`/api/sessions/${id}/interrupt`, { method: "POST", headers: auth }),
  events: (id: string, after: number) =>
    new EventSource(`/api/sessions/${id}/events?token=${token}&after=${after}`),
};

// legacySession reads a session id from an old-style "#token/session" hash.
export function legacySession(): string {
  return window.location.hash.slice(1).split("/")[1] ?? "";
}

// tokenHash is the fragment every in-app navigation must carry along.
export function tokenHash(): string {
  return token ? "#" + token : "";
}
