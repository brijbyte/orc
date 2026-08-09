import { useState } from "react";
import { api } from "./api";
import { setThemePref, themePref, type ThemePref } from "./theme";
import type { Model } from "./types";

// StatusBar renders "model · effort · rest…" with model and effort as
// selects that dispatch the matching slash command, and the theme
// switcher at the right edge.
export function StatusBar({
  status,
  models,
}: {
  status: string;
  models: Model[];
}) {
  const [theme, setTheme] = useState<ThemePref>(themePref());
  const [model, effort, ...rest] = status.split(" · ");
  return (
    <footer>
      <div>
        {status && (
          <span className="stat">
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
          </span>
        )}
        <select
          className="statSel themeSel"
          value={theme}
          onChange={(e) => {
            const p = e.target.value as ThemePref;
            setThemePref(p);
            setTheme(p);
          }}
        >
          {["system", "light", "dark"].map((x) => (
            <option key={x} value={x}>
              {x}
            </option>
          ))}
        </select>
      </div>
    </footer>
  );
}
