import { useEffect, useState } from "react";
import { Dialog } from "@base-ui/react/dialog";
import { BellRing, Check, Plus, Trash2, X } from "lucide-react";
import { api } from "../lib/api";
import type { NotifyChannel, NotifyType } from "../lib/types";
import { Button } from "../ui/Button";
import d from "../ui/dialog.module.css";
import s from "./SettingsDialog.module.css";

type TestState = "idle" | "busy" | "ok" | string;

// SettingsDialog edits the notification channel list (config.json `notify`).
export function SettingsDialog({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}) {
  const [types, setTypes] = useState<NotifyType[]>([]);
  const [channels, setChannels] = useState<NotifyChannel[]>([]);
  const [tests, setTests] = useState<Record<number, TestState>>({});
  const [error, setError] = useState("");

  useEffect(() => {
    if (!open) return;
    setError("");
    setTests({});
    api
      .notify()
      .then((data) => {
        setTypes(data.types ?? []);
        setChannels(data.channels ?? []);
      })
      .catch(() => setError("cannot load channels"));
  }, [open]);

  const patch = (index: number, change: Partial<NotifyChannel>) =>
    setChannels((list) =>
      list.map((ch, i) => (i === index ? { ...ch, ...change } : ch)),
    );

  const setField = (index: number, key: string, value: string) =>
    setChannels((list) =>
      list.map((ch, i) =>
        i === index
          ? { ...ch, settings: { ...ch.settings, [key]: value } }
          : ch,
      ),
    );

  const add = (type: NotifyType) =>
    setChannels((list) => [
      ...list,
      { type: type.id, name: type.label, enabled: true, settings: {} },
    ]);

  const test = (index: number) => {
    setTests((t) => ({ ...t, [index]: "busy" }));
    api
      .notifyTest(channels[index])
      .then(() => setTests((t) => ({ ...t, [index]: "ok" })))
      .catch((err: Error) =>
        setTests((t) => ({ ...t, [index]: err.message || "failed" })),
      );
  };

  const save = () =>
    api
      .notifySave(channels)
      .then(onClose)
      .catch((err: Error) => setError(err.message || "save failed"));

  return (
    <Dialog.Root open={open} onOpenChange={(isOpen) => !isOpen && onClose()}>
      <Dialog.Portal>
        <Dialog.Backdrop className={d.overlay} />
        <Dialog.Popup className={d.popup}>
          <Dialog.Title className={s.title}>
            <BellRing size={15} strokeWidth={1.8} aria-hidden />
            notification channels
          </Dialog.Title>
          <Dialog.Description className={s.desc}>
            agents use these channels to reach you when the UI is closed.
          </Dialog.Description>
          {channels.map((ch, i) => {
            const type = types.find((t) => t.id === ch.type);
            const state = tests[i] ?? "idle";
            return (
              <section className={s.channel} key={i} aria-label={ch.name}>
                <div className={s.row}>
                  <label className={s.toggle}>
                    <input
                      type="checkbox"
                      checked={ch.enabled}
                      onChange={(e) => patch(i, { enabled: e.target.checked })}
                    />
                    enabled
                  </label>
                  <input
                    className={s.name}
                    value={ch.name}
                    aria-label="channel name"
                    onChange={(e) => patch(i, { name: e.target.value })}
                  />
                  <span className={s.type}>{type?.label ?? ch.type}</span>
                  <Button
                    icon
                    tone="danger"
                    tip="remove channel"
                    onClick={() =>
                      setChannels((list) => list.filter((_, j) => j !== i))
                    }
                  >
                    <Trash2 size={12} />
                  </Button>
                </div>
                {(type?.fields ?? []).map((f) => (
                  <label className={s.field} key={f.key}>
                    <span>
                      {f.label}
                      {f.optional ? "" : " *"}
                    </span>
                    <input
                      type={f.secret ? "password" : "text"}
                      value={ch.settings?.[f.key] ?? ""}
                      placeholder={f.placeholder}
                      onChange={(e) => setField(i, f.key, e.target.value)}
                    />
                  </label>
                ))}
                <div className={s.row}>
                  <Button
                    small
                    outline
                    disabled={state === "busy"}
                    onClick={() => test(i)}
                  >
                    {state === "busy" ? "testing…" : "send test"}
                  </Button>
                  {state === "ok" && (
                    <span className={s.ok}>
                      <Check size={12} strokeWidth={2} aria-hidden /> delivered
                    </span>
                  )}
                  {state !== "idle" && state !== "busy" && state !== "ok" && (
                    <span className={s.err}>{state}</span>
                  )}
                </div>
              </section>
            );
          })}
          {types.map((t) => (
            <Button small outline key={t.id} onClick={() => add(t)}>
              <Plus size={12} strokeWidth={1.8} aria-hidden />
              add {t.label} channel
            </Button>
          ))}
          {error && <span className={s.err}>{error}</span>}
          <div className={d.foot}>
            <Dialog.Close render={<Button outline />}>
              <X size={13} strokeWidth={1.8} aria-hidden />
              cancel
            </Dialog.Close>
            <Button outline tone="success" onClick={save}>
              <Check size={13} strokeWidth={1.8} aria-hidden />
              save
            </Button>
          </div>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
