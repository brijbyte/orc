import { useEffect, useMemo, useState } from "react";
import { Dialog } from "@base-ui/react/dialog";
import {
  BellRing,
  Check,
  KeyRound,
  Palette,
  Plus,
  Settings,
  Trash2,
  X,
} from "lucide-react";
import { api } from "../lib/api";
import { setThemePref, themePref, type ThemePref } from "../lib/theme";
import type { Model, NotifyChannel, NotifyType } from "../lib/types";
import { Button } from "../ui/Button";
import d from "../ui/dialog.module.css";
import s from "./SettingsDialog.module.css";

type TestState = "idle" | "busy" | "ok" | string;

export function SettingsDialog({
  open,
  models,
  onClose,
}: {
  open: boolean;
  models: Model[];
  onClose: () => void;
}) {
  const [types, setTypes] = useState<NotifyType[]>([]);
  const [channels, setChannels] = useState<NotifyChannel[]>([]);
  const [model, setModel] = useState("");
  const [effort, setEffort] = useState("");
  const [theme, setTheme] = useState<ThemePref>(themePref());
  const [tests, setTests] = useState<Record<number, TestState>>({});
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);
  const [currentPassword, setCurrentPassword] = useState("");
  const [nextPassword, setNextPassword] = useState("");
  const [passwordState, setPasswordState] = useState("");
  const [passwordBusy, setPasswordBusy] = useState(false);

  useEffect(() => {
    if (!open) return;
    setError("");
    setTests({});
    setTheme(themePref());
    setCurrentPassword("");
    setNextPassword("");
    setPasswordState("");
    Promise.all([api.settings(), api.notify()])
      .then(([settings, notifications]) => {
        setModel(settings.model ?? "");
        setEffort(settings.effort ?? "");
        setTypes(notifications.types ?? []);
        setChannels(notifications.channels ?? []);
      })
      .catch(() => setError("cannot load settings"));
  }, [open]);

  const efforts = useMemo(() => {
    const values = models.find((item) => item.slug === model)?.efforts ?? [
      "low",
      "medium",
      "high",
    ];
    return values.includes(effort) || !effort ? values : [effort, ...values];
  }, [models, model, effort]);

  const modelOptions = models.some((item) => item.slug === model)
    ? models
    : model
      ? [{ slug: model, name: model, description: "" }, ...models]
      : models;

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
    setTests((states) => ({ ...states, [index]: "busy" }));
    api
      .notifyTest(channels[index])
      .then(() => setTests((states) => ({ ...states, [index]: "ok" })))
      .catch((err: Error) =>
        setTests((states) => ({
          ...states,
          [index]: err.message || "failed",
        })),
      );
  };

  const save = () => {
    setSaving(true);
    setError("");
    Promise.all([api.settingsSave(model, effort), api.notifySave(channels)])
      .then(onClose)
      .catch((err: Error) => setError(err.message || "save failed"))
      .finally(() => setSaving(false));
  };

  const changePassword = (event: React.FormEvent) => {
    event.preventDefault();
    setPasswordBusy(true);
    setPasswordState("");
    api
      .changePassword(currentPassword, nextPassword)
      .then(() => {
        setPasswordState("changed — sign in again");
        window.setTimeout(() => window.location.reload(), 900);
      })
      .catch((err: Error) =>
        setPasswordState(err.message || "password change failed"),
      )
      .finally(() => setPasswordBusy(false));
  };

  return (
    <Dialog.Root open={open} onOpenChange={(isOpen) => !isOpen && onClose()}>
      <Dialog.Portal>
        <Dialog.Backdrop className={d.overlay} />
        <Dialog.Popup className={d.popup}>
          <Dialog.Title className={s.title}>
            <Settings size={15} strokeWidth={1.8} aria-hidden />
            settings
          </Dialog.Title>
          <Dialog.Description className={s.desc}>
            defaults and preferences for this orc server.
          </Dialog.Description>

          <section className={s.section}>
            <h2 className={s.sectionTitle}>
              <Palette size={14} strokeWidth={1.8} aria-hidden />
              general
            </h2>
            <label className={s.setting}>
              <span>default model</span>
              <select
                value={model}
                onChange={(event) => {
                  const nextModel = event.target.value;
                  const nextEfforts = models.find(
                    (item) => item.slug === nextModel,
                  )?.efforts ?? ["low", "medium", "high"];
                  setModel(nextModel);
                  if (!nextEfforts.includes(effort))
                    setEffort(
                      nextEfforts.includes("medium")
                        ? "medium"
                        : (nextEfforts[0] ?? effort),
                    );
                }}
              >
                {modelOptions.map((item) => (
                  <option key={item.slug} value={item.slug}>
                    {item.name || item.slug}
                  </option>
                ))}
              </select>
            </label>
            <label className={s.setting}>
              <span>default effort</span>
              <select
                value={effort}
                onChange={(event) => setEffort(event.target.value)}
              >
                {efforts.map((value) => (
                  <option key={value} value={value}>
                    {value}
                  </option>
                ))}
              </select>
            </label>
            <p className={s.hint}>applies to new sessions.</p>
            <label className={s.setting}>
              <span>theme</span>
              <select
                value={theme}
                onChange={(event) => {
                  const value = event.target.value as ThemePref;
                  setTheme(value);
                  setThemePref(value);
                }}
              >
                <option value="system">system</option>
                <option value="light">light</option>
                <option value="dark">dark</option>
              </select>
            </label>
          </section>

          <section className={s.section}>
            <h2 className={s.sectionTitle}>
              <KeyRound size={14} strokeWidth={1.8} aria-hidden />
              password
            </h2>
            <form className={s.password} onSubmit={changePassword}>
              <label className={s.setting}>
                <span>current password</span>
                <input
                  type="password"
                  autoComplete="current-password"
                  value={currentPassword}
                  onChange={(event) => setCurrentPassword(event.target.value)}
                />
              </label>
              <label className={s.setting}>
                <span>new password</span>
                <input
                  type="password"
                  autoComplete="new-password"
                  minLength={8}
                  maxLength={72}
                  value={nextPassword}
                  onChange={(event) => setNextPassword(event.target.value)}
                />
              </label>
              <div className={s.row}>
                <Button
                  type="submit"
                  small
                  outline
                  disabled={
                    passwordBusy || !currentPassword || nextPassword.length < 8
                  }
                >
                  {passwordBusy ? "changing…" : "change password"}
                </Button>
                {passwordState && (
                  <span
                    className={
                      passwordState.startsWith("changed") ? s.ok : s.err
                    }
                  >
                    {passwordState}
                  </span>
                )}
              </div>
            </form>
          </section>

          <section className={s.section}>
            <h2 className={s.sectionTitle}>
              <BellRing size={14} strokeWidth={1.8} aria-hidden />
              notification channels
            </h2>
            <p className={s.hint}>
              agents use these channels to reach you when the UI is closed.
            </p>
            {channels.map((ch, i) => {
              const type = types.find((item) => item.id === ch.type);
              const state = tests[i] ?? "idle";
              return (
                <div className={s.channel} key={i}>
                  <div className={s.row}>
                    <label className={s.toggle}>
                      <input
                        type="checkbox"
                        checked={ch.enabled}
                        onChange={(event) =>
                          patch(i, { enabled: event.target.checked })
                        }
                      />
                      enabled
                    </label>
                    <input
                      className={s.name}
                      value={ch.name}
                      aria-label="channel name"
                      onChange={(event) =>
                        patch(i, { name: event.target.value })
                      }
                    />
                    <span className={s.type}>{type?.label ?? ch.type}</span>
                    <Button
                      icon
                      tone="danger"
                      tip="remove channel"
                      onClick={() =>
                        setChannels((list) =>
                          list.filter((_, index) => index !== i),
                        )
                      }
                    >
                      <Trash2 size={12} />
                    </Button>
                  </div>
                  {(type?.fields ?? []).map((field) => (
                    <label className={s.field} key={field.key}>
                      <span>
                        {field.label}
                        {field.optional ? "" : " *"}
                      </span>
                      <input
                        type={field.secret ? "password" : "text"}
                        value={ch.settings?.[field.key] ?? ""}
                        placeholder={field.placeholder}
                        onChange={(event) =>
                          setField(i, field.key, event.target.value)
                        }
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
                        <Check size={12} strokeWidth={2} aria-hidden />{" "}
                        delivered
                      </span>
                    )}
                    {state !== "idle" && state !== "busy" && state !== "ok" && (
                      <span className={s.err}>{state}</span>
                    )}
                  </div>
                </div>
              );
            })}
            <div className={s.adds}>
              {types.map((type) => (
                <Button small outline key={type.id} onClick={() => add(type)}>
                  <Plus size={12} strokeWidth={1.8} aria-hidden />
                  add {type.label} channel
                </Button>
              ))}
            </div>
          </section>

          {error && <span className={s.err}>{error}</span>}
          <div className={d.foot}>
            <Dialog.Close render={<Button outline />}>
              <X size={13} strokeWidth={1.8} aria-hidden />
              cancel
            </Dialog.Close>
            <Button
              outline
              tone="success"
              disabled={saving || !model || !effort}
              onClick={save}
            >
              <Check size={13} strokeWidth={1.8} aria-hidden />
              {saving ? "saving…" : "save"}
            </Button>
          </div>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
