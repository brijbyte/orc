import { useState } from "react";
import { PreviewCard } from "@base-ui/react/preview-card";
import {
  AlarmClock,
  GitBranch,
  Minimize2,
  SlidersHorizontal,
  SquareTerminal,
} from "lucide-react";
import { api } from "../lib/api";
import type { Model } from "../lib/types";
import { Button } from "../ui/Button";
import { Select } from "../ui/Select";
import s from "./StatusBar.module.css";

// StatusBar renders "model · effort · rest…" with model and effort as
// selects that dispatch the matching slash command.
export function StatusBar({
  sid,
  status,
  models,
  compactDisabled,
  canWake,
  onCompact,
  onWake,
  onOpenGit,
  onOpenTerminal,
}: {
  sid: string;
  status: string;
  models: Model[];
  compactDisabled: boolean;
  canWake: boolean;
  onCompact: () => void;
  onWake: () => void;
  onOpenGit: () => void;
  onOpenTerminal: () => void;
}) {
  const [mobileOpen, setMobileOpen] = useState(false);
  const [model, effort, ...rest] = status.split(" · ");
  const modelOpts = models.map((m) => ({
    value: m.slug,
    label: m.name || m.slug,
    title: m.description,
  }));
  if (model && !models.some((m) => m.slug === model))
    modelOpts.unshift({ value: model, label: model, title: "" });
  const settings = (
    <>
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
    </>
  );
  return (
    <footer className={s.footer}>
      <div className={s.inner}>
        {status && <span className={s.stat}>{settings}</span>}
        {status && (
          <PreviewCard.Root open={mobileOpen} onOpenChange={setMobileOpen}>
            <PreviewCard.Trigger
              delay={0}
              render={
                <Button
                  small
                  outline
                  className={s.mobileStat}
                  aria-label="session settings and context"
                  aria-expanded={mobileOpen}
                  onClick={() => setMobileOpen(true)}
                />
              }
            >
              <SlidersHorizontal size={13} strokeWidth={1.8} aria-hidden />
              session
            </PreviewCard.Trigger>
            <PreviewCard.Portal>
              <PreviewCard.Positioner
                className={s.statPositioner}
                side="top"
                align="start"
                sideOffset={6}
              >
                <PreviewCard.Popup className={s.statPopup}>
                  {settings}
                </PreviewCard.Popup>
              </PreviewCard.Positioner>
            </PreviewCard.Portal>
          </PreviewCard.Root>
        )}
        <span className={s.actions}>
          <Button small outline disabled={compactDisabled} onClick={onCompact}>
            <Minimize2 size={13} strokeWidth={1.8} aria-hidden />
            compact
          </Button>
          {canWake && (
            <Button small outline tone="accent" onClick={onWake}>
              <AlarmClock size={13} strokeWidth={1.8} aria-hidden />
              wake now
            </Button>
          )}
          <Button small tip="open Git (Ctrl/⌘ G)" onClick={onOpenGit}>
            <GitBranch size={13} strokeWidth={1.8} aria-hidden /> Git
          </Button>
          <Button small tip="open terminal (Ctrl/⌘ `)" onClick={onOpenTerminal}>
            <SquareTerminal size={13} strokeWidth={1.8} aria-hidden /> terminal
          </Button>
        </span>
      </div>
    </footer>
  );
}
