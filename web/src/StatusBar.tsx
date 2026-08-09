import { api } from "./api";
import type { Model } from "./types";

// StatusBar renders "model · effort · rest…" with model and effort as
// selects that dispatch the matching slash command.
export function StatusBar({
  status,
  models,
}: {
  status: string;
  models: Model[];
}) {
  if (!status) return <footer />;
  const [model, effort, ...rest] = status.split(" · ");
  return (
    <footer>
      <div>
        <select
          className="statSel"
          value={model}
          onChange={(e) => api.send("/model " + e.target.value)}
        >
          {!models.some((m) => m.slug === model) && (
            <option value={model}>{model}</option>
          )}
          {models.map((m) => (
            <option key={m.slug} value={m.slug} title={m.description}>
              {m.slug}
            </option>
          ))}
        </select>
        {" · "}
        <select
          className="statSel"
          value={effort}
          onChange={(e) => api.send("/effort " + e.target.value)}
        >
          {["low", "medium", "high"].map((x) => (
            <option key={x} value={x}>
              {x}
            </option>
          ))}
        </select>
        {rest.length > 0 && " · " + rest.join(" · ")}
      </div>
    </footer>
  );
}
