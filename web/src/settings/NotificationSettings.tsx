import { useState } from "react";
import { BellRing, Check, Plus, Trash2 } from "lucide-react";
import type { NotifyChannel, NotifyType } from "../lib/types";
import { Button } from "../ui/Button";
import { SettingsSection } from "./SettingsSection";
import { useSettings } from "./SettingsContext";
import s from "./NotificationSettings.module.css";

type TestState = "idle" | "busy" | "ok" | string;

export function NotificationSettings({
  channels,
  onChange,
}: {
  channels: NotifyChannel[];
  onChange: React.Dispatch<React.SetStateAction<NotifyChannel[]>>;
}) {
  const { data, testChannel } = useSettings();
  const types = data?.types ?? [];
  const [tests, setTests] = useState<Record<number, TestState>>({});

  const patch = (index: number, change: Partial<NotifyChannel>) =>
    onChange((list) =>
      list.map((channel, i) =>
        i === index ? { ...channel, ...change } : channel,
      ),
    );

  const setField = (index: number, key: string, value: string) =>
    onChange((list) =>
      list.map((channel, i) =>
        i === index
          ? {
              ...channel,
              settings: { ...channel.settings, [key]: value },
            }
          : channel,
      ),
    );

  const add = (type: NotifyType) =>
    onChange((list) => [
      ...list,
      { type: type.id, name: type.label, enabled: true, settings: {} },
    ]);

  const test = (index: number) => {
    setTests((states) => ({ ...states, [index]: "busy" }));
    testChannel(channels[index])
      .then(() => setTests((states) => ({ ...states, [index]: "ok" })))
      .catch((err: Error) =>
        setTests((states) => ({
          ...states,
          [index]: err.message || "failed",
        })),
      );
  };

  return (
    <SettingsSection
      icon={BellRing}
      title="notification channels"
      description="agents use these channels to reach you when the UI is closed."
    >
      {channels.map((channel, index) => {
        const type = types.find((item) => item.id === channel.type);
        const state = tests[index] ?? "idle";
        return (
          <div className={s.channel} key={index}>
            <div className={s.row}>
              <label className={s.toggle}>
                <input
                  type="checkbox"
                  checked={channel.enabled}
                  onChange={(event) =>
                    patch(index, { enabled: event.target.checked })
                  }
                />
                enabled
              </label>
              <input
                className={s.name}
                value={channel.name}
                aria-label="channel name"
                onChange={(event) => patch(index, { name: event.target.value })}
              />
              <span className={s.type}>{type?.label ?? channel.type}</span>
              <Button
                icon
                tone="danger"
                tip="remove channel"
                onClick={() =>
                  onChange((list) => list.filter((_, i) => i !== index))
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
                  value={channel.settings?.[field.key] ?? ""}
                  placeholder={field.placeholder}
                  onChange={(event) =>
                    setField(index, field.key, event.target.value)
                  }
                />
              </label>
            ))}
            <div className={s.row}>
              <Button
                small
                outline
                disabled={state === "busy"}
                onClick={() => test(index)}
              >
                {state === "busy" ? "testing…" : "send test"}
              </Button>
              {state === "ok" && (
                <span className={s.ok}>
                  <Check size={12} strokeWidth={2} aria-hidden /> delivered
                </span>
              )}
              {state !== "idle" && state !== "busy" && state !== "ok" && (
                <span className={s.error}>{state}</span>
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
    </SettingsSection>
  );
}
