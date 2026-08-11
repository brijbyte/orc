import type { SessionRow } from "./types";

export function sessionTitle(row: SessionRow | null): string {
  const title = (row?.title || row?.id.slice(0, 8) || "session")
    .replace(/\s+/g, " ")
    .trim();
  return title.length > 64 ? title.slice(0, 63) + "…" : title;
}
