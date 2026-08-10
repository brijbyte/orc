import { useState } from "react";
import { GitBranch } from "lucide-react";
import { api } from "../lib/api";
import { setThemePref, themePref, type ThemePref } from "../lib/theme";
import type { Model } from "../lib/types";
import { Button } from "../ui/Button";
import { Select } from "../ui/Select";
import s from "./StatusBar.module.css";

// StatusBar renders "model · effort · rest…" with model and effort as
// selects that dispatch the matching slash command, and the theme
// switcher at the right edge.
export function StatusBar({
  sid,
  status,
  models,
  onOpenGit,
}: {
  sid: string;
  status: string;
  models: Model[];
  onOpenGit: () => void;
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
            <Select
              value={model}
              options={modelOpts}
              onChange={(v) => api.send(sid, "/model " + v)}
            />
            {" · "}
            <Select
              value={effort}
              options={["low", "medium", "high"].map((value) => ({ value }))}
              onChange={(v) => api.send(sid, "/effort " + v)}
            />
            {rest.length > 0 && " · " + rest.join(" · ")}
          </span>
        )}
        <Button small className={s.git} onClick={onOpenGit}>
          <GitBranch size={13} strokeWidth={1.8} aria-hidden /> Git
        </Button>
        <Select
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
