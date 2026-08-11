import { api, APIError } from "./api";
import type { Model, SessionRow } from "./types";

export type RootData = {
  authenticated: boolean;
  dead: boolean;
  rows: SessionRow[];
  cwd: string;
  home: string;
  models: Model[];
};

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
    return {
      authenticated: true,
      dead: false,
      rows: s.sessions ?? [],
      cwd: s.cwd ?? "",
      home: s.home ?? "",
      models: m.models ?? [],
    };
  } catch (error) {
    return {
      authenticated: !(error instanceof APIError && error.status === 401),
      dead: !(error instanceof APIError && error.status === 401),
      rows: [],
      cwd: "",
      home: "",
      models: [],
    };
  }
}
