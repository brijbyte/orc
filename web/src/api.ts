const token = window.location.hash.slice(1);

const auth = { Authorization: `Bearer ${token}` };

export const api = {
  state: () =>
    fetch(`/api/state?token=${token}`).then((r) => {
      if (!r.ok) throw new Error(String(r.status));
      return r.json();
    }),
  models: () => fetch(`/api/models?token=${token}`).then((r) => r.json()),
  send: (text: string) =>
    fetch("/api/input", {
      method: "POST",
      headers: auth,
      body: JSON.stringify({ text }),
    }),
  interrupt: () => fetch("/api/interrupt", { method: "POST", headers: auth }),
  events: (after: number) =>
    new EventSource(`/api/events?token=${token}&after=${after}`),
};
