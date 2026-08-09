import { useState } from "react";
import { api } from "./api";
import { setThemePref, themePref, type ThemePref } from "./theme";
import type { Model } from "./types";
import { Sel } from "./ui";
import s from "./StatusBar.module.css";

// StatusBar renders "model · effort · rest…" with model and effort as
// selects that dispatch the matching slash command, and the theme
// switcher at the right edge.
export function StatusBar({
  sid,
  status,
  models,
}: {
  sid: string;
  status: string;
  models: Model[];
}) {
  const [theme, setTheme] = useState<ThemePref>(themePref());
  const [model, effort, ...rest] = status.split(" · ");
  const modelOpts = models.map((m) => ({
    value: m.slug,
    title: m.description,
  }));
  if (model && !models.some((m) => m.slug === model))
    modelOpts.unshift({ value: model, title: "" });
  return (
    <footer className={s.footer}>
      <div>
        {status && (
          <span className={s.stat}>
            <Sel
              value={model}
              options={modelOpts}
              onChange={(v) => api.send(sid, "/model " + v)}
            />
            {" · "}
            <Sel
              value={effort}
              options={["low", "medium", "high"].map((value) => ({ value }))}
              onChange={(v) => api.send(sid, "/effort " + v)}
            />
            {rest.length > 0 && " · " + rest.join(" · ")}
          </span>
        )}
        <Sel
          className={s.themeSel}
          value={theme}
          options={["system", "light", "dark"].map((value) => ({ value }))}
          onChange={(v) => {
            setThemePref(v as ThemePref);
            setTheme(v as ThemePref);
          }}
        />
      </div>
    </footer>
  );
}
