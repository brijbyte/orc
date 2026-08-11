import { useState } from "react";
import { Check, Plus, Trash2 } from "lucide-react";
import type { NotifyChannel, NotifyType } from "../lib/types";
import { Button } from "../ui/Button";
import { SettingsSection } from "./SettingsSection";
import { useSettings } from "./SettingsContext";
import s from "./NotificationSettings.module.css";

type TestState = "idle" | "busy" | "ok" | string;
export type DraftNotifyChannel = NotifyChannel & { clientId: string };

let nextChannelID = 0;

export function draftChannel(channel: NotifyChannel): DraftNotifyChannel {
  const randomID = globalThis.crypto?.randomUUID?.();
  return {
    ...channel,
    settings: channel.settings ? { ...channel.settings } : undefined,
    clientId: randomID ?? `channel-${Date.now()}-${++nextChannelID}`,
  };
}

export function plainChannel(channel: NotifyChannel): NotifyChannel {
  return {
    type: channel.type,
    name: channel.name,
    enabled: channel.enabled,
    ...(channel.settings ? { settings: channel.settings } : {}),
  };
}

export function NotificationSettings({
  channels,
  onChange,
}: {
  channels: DraftNotifyChannel[];
  onChange: React.Dispatch<React.SetStateAction<DraftNotifyChannel[]>>;
}) {
  const { data, testChannel } = useSettings();
  const types = data?.types ?? [];
  const [tests, setTests] = useState<Record<string, TestState>>({});

  const patch = (id: string, change: Partial<NotifyChannel>) =>
    onChange((list) =>
      list.map((channel) =>
        channel.clientId === id ? { ...channel, ...change } : channel,
      ),
    );

  const setField = (id: string, key: string, value: string) =>
    onChange((list) =>
      list.map((channel) =>
        channel.clientId === id
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
      draftChannel({
        type: type.id,
        name: type.label,
        enabled: true,
        settings: {},
      }),
    ]);

  const test = (channel: DraftNotifyChannel) => {
    const id = channel.clientId;
    setTests((states) => ({ ...states, [id]: "busy" }));
    testChannel(plainChannel(channel))
      .then(() => setTests((states) => ({ ...states, [id]: "ok" })))
      .catch((err: Error) =>
        setTests((states) => ({
          ...states,
          [id]: err.message || "failed",
        })),
      );
  };

  return (
    <SettingsSection>
      {channels.map((channel) => {
        const type = types.find((item) => item.id === channel.type);
        const state = tests[channel.clientId] ?? "idle";
        return (
          <div className={s.channel} key={channel.clientId}>
            <div className={s.row}>
              <label className={s.toggle}>
                <input
                  type="checkbox"
                  checked={channel.enabled}
                  onChange={(event) =>
                    patch(channel.clientId, { enabled: event.target.checked })
                  }
                />
                enabled
              </label>
              <input
                className={s.name}
                value={channel.name}
                aria-label="channel name"
                onChange={(event) =>
                  patch(channel.clientId, { name: event.target.value })
                }
              />
              <span className={s.type}>{type?.label ?? channel.type}</span>
              <Button
                icon
                tone="danger"
                tip="remove channel"
                onClick={() =>
                  onChange((list) =>
                    list.filter((item) => item.clientId !== channel.clientId),
                  )
                }
              >
                <Trash2 size={12} aria-hidden />
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
                    setField(channel.clientId, field.key, event.target.value)
                  }
                />
              </label>
            ))}
            <div className={s.testRow}>
              <Button
                small
                outline
                disabled={state === "busy"}
                onClick={() => test(channel)}
              >
                {state === "busy" ? "testing…" : "send test"}
              </Button>
              <div className={s.feedback} aria-live="polite">
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
