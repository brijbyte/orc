import { useState } from "react";
import { GitBranch, Minimize2, SquareTerminal } from "lucide-react";
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
  compactDisabled,
  onCompact,
  onOpenGit,
  onOpenTerminal,
}: {
  sid: string;
  status: string;
  models: Model[];
  compactDisabled: boolean;
  onCompact: () => void;
  onOpenGit: () => void;
  onOpenTerminal: () => void;
}) {
  const [theme, setTheme] = useState<ThemePref>(themePref());
  const [model, effort, ...rest] = status.split(" · ");
  const modelOpts = models.map((m) => ({
    value: m.slug,
    label: m.name || m.slug,
    title: m.description,
  }));
  if (model && !models.some((m) => m.slug === model))
    modelOpts.unshift({ value: model, label: model, title: "" });
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
              options={(
                models.find((m) => m.slug === model)?.efforts ?? [
                  "low",
                  "medium",
                  "high",
                ]
              ).map((value) => ({ value }))}
              onChange={(v) => api.send(sid, "/effort " + v)}
            />
            {rest.length > 0 && " · " + rest.join(" · ")}
          </span>
        )}
        <Button
          small
          outline
          className={s.compact}
          disabled={compactDisabled}
          onClick={onCompact}
        >
          <Minimize2 size={13} strokeWidth={1.8} aria-hidden />
          compact
        </Button>
        <Button small tip="open Git (Ctrl/⌘ G)" onClick={onOpenGit}>
          <GitBranch size={13} strokeWidth={1.8} aria-hidden /> Git
        </Button>
        <Button small tip="open terminal (Ctrl/⌘ `)" onClick={onOpenTerminal}>
          <SquareTerminal size={13} strokeWidth={1.8} aria-hidden /> terminal
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
