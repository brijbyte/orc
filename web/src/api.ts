// Hash is "#<token>" or "#<token>/<session id>".
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
  state: (id: string) => get(`/api/sessions/${id}/state`),
  models: () => get("/api/models"),
  dirs: (path?: string) =>
    get(`/api/dirs${path ? `?path=${encodeURIComponent(path)}` : ""}`),
  mkdir: (path: string) => post("/api/dirs", { path }),
  send: (id: string, text: string) =>
    post(`/api/sessions/${id}/input`, { text }),
  interrupt: (id: string) =>
    fetch(`/api/sessions/${id}/interrupt`, { method: "POST", headers: auth }),
  events: (id: string, after: number) =>
    new EventSource(`/api/sessions/${id}/events?token=${token}&after=${after}`),
};

// hashSession reads the active session id from the URL hash.
export function hashSession(): string {
  return window.location.hash.slice(1).split("/")[1] ?? "";
}

// setHashSession records the active session in the hash (token kept).
export function setHashSession(id: string) {
  history.replaceState(null, "", `#${token}${id ? "/" + id : ""}`);
}
