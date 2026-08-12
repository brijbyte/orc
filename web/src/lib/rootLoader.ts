import { api, APIError } from "./api";
import type { Model, SessionRow } from "./types";

export type RootData = {
  authenticated: boolean;
  dead: boolean;
  rows: SessionRow[];
  cwd: string;
  home: string;
  model: string;
  effort: string;
  models: Model[];
};

// Preserve the last shell snapshot through transient server failures.
let lastKnown: RootData | null = null;

// models are static per server run: fetch once, not on every revalidation
let modelsOnce: Promise<{ models: Model[] }> | null = null;
const loadModels = () =>
  (modelsOnce ??= api.models().catch(() => {
    modelsOnce = null; // retry on the next revalidation
    return { models: [] };
  }));

// rootLoader fetches the session list and models before the shell renders.
// A dead server is data, so the poll can keep retrying.
export async function rootLoader(): Promise<RootData> {
  try {
    const s = await api.sessions();
    const m = await loadModels();
    return (lastKnown = {
      authenticated: true,
      dead: false,
      rows: s.sessions ?? [],
      cwd: s.cwd ?? "",
      home: s.home ?? "",
      model: s.model ?? "",
      effort: s.effort ?? "",
      models: m.models ?? [],
    });
  } catch (error) {
    const unauthorized = error instanceof APIError && error.status === 401;
    if (!unauthorized && lastKnown)
      return { ...lastKnown, authenticated: true, dead: true };
    return {
      authenticated: !unauthorized,
      dead: !unauthorized,
      rows: [],
      cwd: "",
      home: "",
      model: "",
      effort: "",
      models: [],
    };
  }
}
