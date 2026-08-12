import { useEffect, useRef, useState } from "react";
import { Dialog } from "@base-ui/react/dialog";
import {
  Activity,
  BellRing,
  Boxes,
  Check,
  KeyRound,
  LoaderCircle,
  Palette,
  Settings,
  X,
  type LucideIcon,
} from "lucide-react";
import type { Settings as SettingsData } from "../lib/types";
import { Button } from "../ui/Button";
import d from "../ui/dialog.module.css";
import { DiagnosticsSettings } from "./DiagnosticsSettings";
import { GeneralSettings } from "./GeneralSettings";
import {
  NotificationSettings,
  draftChannel,
  plainChannel,
  type DraftNotifyChannel,
} from "./NotificationSettings";
import { PasswordSettings } from "./PasswordSettings";
import { ProvidersSettings } from "./ProvidersSettings";
import { useSettings } from "./SettingsContext";
import s from "./SettingsDialog.module.css";

type Pane =
  "general" | "providers" | "password" | "notifications" | "diagnostics";

type NavItem = {
  id: Pane;
  label: string;
  description: string;
  icon: LucideIcon;
};

const items: NavItem[] = [
  {
    id: "general",
    label: "General",
    description:
      "Choose defaults for new sessions and how orc looks in this browser.",
    icon: Palette,
  },
  {
    id: "providers",
    label: "Providers",
    description: "Manage model providers and their accounts.",
    icon: Boxes,
  },
  {
    id: "password",
    label: "Password",
    description: "Change the password used to access this web interface.",
    icon: KeyRound,
  },
  {
    id: "notifications",
    label: "Notifications",
    description: "Let agents reach you when the web interface is closed.",
    icon: BellRing,
  },
  {
    id: "diagnostics",
    label: "Diagnostics",
    description: "Version, uptime, and update status for this orc server.",
    icon: Activity,
  },
];

export function SettingsDialog() {
  const {
    open,
    closeDialog,
    loading,
    error: loadError,
    data,
    save,
  } = useSettings();
  const [model, setModel] = useState("");
  const [effort, setEffort] = useState("");
  const [channels, setChannels] = useState<DraftNotifyChannel[]>([]);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState("");
  const [pane, setPane] = useState<Pane>("general");
  const draftReady = useRef(false);

  const defaults: Partial<SettingsData> = {};
  if (data && model !== data.model) defaults.model = model;
  if (data && effort !== data.effort) defaults.effort = effort;
  const defaultsDirty = Object.keys(defaults).length > 0;
  const channelValues = channels.map(plainChannel);
  const channelsDirty =
    !!data &&
    JSON.stringify(channelValues) !==
      JSON.stringify(data.channels.map(plainChannel));
  const dirty = defaultsDirty || channelsDirty;

  useEffect(() => {
    if (!open) {
      draftReady.current = false;
      return;
    }
    if (!data || (draftReady.current && dirty)) return;
    setModel(data.model);
    setEffort(data.effort);
    setChannels(data.channels.map(draftChannel));
    setSaveError("");
    draftReady.current = true;
  }, [open, data, dirty]);

  const submit = async () => {
    if (!data || !dirty) return;
    setSaving(true);
    setSaveError("");
    try {
      await save(
        defaultsDirty ? defaults : undefined,
        channelsDirty ? channelValues : undefined,
      );
      closeDialog();
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : "save failed");
    } finally {
      setSaving(false);
    }
  };

  const activeItem = items.find((item) => item.id === pane) ?? items[0];
  const ActiveIcon = activeItem.icon;

  return (
    <Dialog.Root
      open={open}
      onOpenChange={(isOpen) => !isOpen && closeDialog()}
    >
      <Dialog.Portal>
        <Dialog.Backdrop className={d.overlay} />
        <Dialog.Popup className={`${d.popup} ${s.popup}`}>
          <aside className={s.sidebar}>
            <header className={s.titleRow}>
              <Dialog.Title className={s.title}>
                <Settings size={17} strokeWidth={1.8} aria-hidden />
                Settings
              </Dialog.Title>
              <Dialog.Close
                render={<Button icon className={s.close} />}
                aria-label="close settings"
              >
                <X size={16} strokeWidth={1.8} aria-hidden />
              </Dialog.Close>
            </header>
            <Dialog.Description className={s.srOnly}>
              Defaults and preferences for this orc server.
            </Dialog.Description>
            <nav className={s.desktopNav} aria-label="settings sections">
              <ul className={s.navList}>
                {items.map((item) => {
                  const Icon = item.icon;
                  return (
                    <li key={item.id}>
                      <Button
                        nav
                        data-active={pane === item.id || undefined}
                        aria-current={pane === item.id ? "page" : undefined}
                        onClick={() => setPane(item.id)}
                      >
                        <Icon size={17} strokeWidth={1.8} aria-hidden />
                        {item.label}
                      </Button>
                    </li>
                  );
                })}
              </ul>
            </nav>
            <label className={s.mobileNav}>
              <span className={s.srOnly}>Settings section</span>
              <select
                value={pane}
                onChange={(event) => setPane(event.target.value as Pane)}
              >
                {items.map((item) => (
                  <option key={item.id} value={item.id}>
                    {item.label}
                  </option>
                ))}
              </select>
            </label>
          </aside>

          <div className={s.main}>
            <header className={s.toolbar}>
              <ActiveIcon
                className={s.toolbarIcon}
                size={22}
                strokeWidth={1.6}
                aria-hidden
              />
              <div className={s.toolbarCopy}>
                <strong>{activeItem.label}</strong>
                <span>{activeItem.description}</span>
              </div>
              {loading && data && (
                <span className={s.refreshing} role="status">
                  <LoaderCircle size={14} strokeWidth={1.8} aria-hidden />
                  refreshing
                </span>
              )}
            </header>

            <div className={s.content}>
              {!data && loading && (
                <span className={s.loading}>loading settings…</span>
              )}
              {loadError && (
                <div className={s.loadError} role="alert">
                  {loadError}
                </div>
              )}
              {open && data && (
                <>
                  <div hidden={pane !== "general"}>
                    <GeneralSettings
                      model={model}
                      effort={effort}
                      onModelChange={setModel}
                      onEffortChange={setEffort}
                    />
                  </div>
                  <div hidden={pane !== "providers"}>
                    <ProvidersSettings />
                  </div>
                  <div hidden={pane !== "password"}>
                    <PasswordSettings />
                  </div>
                  <div hidden={pane !== "notifications"}>
                    <NotificationSettings
                      channels={channels}
                      onChange={setChannels}
                    />
                  </div>
                  <div hidden={pane !== "diagnostics"}>
                    <DiagnosticsSettings />
                  </div>
                </>
              )}
            </div>

            {dirty && (
              <footer className={s.footer}>
                <div className={s.saveError} aria-live="polite">
                  {saveError}
                </div>
                <div className={s.footerRow}>
                  <span className={s.unsaved}>Unsaved changes</span>
                  <Dialog.Close render={<Button outline />}>
                    Cancel
                  </Dialog.Close>
                  <Button
                    outline
                    tone="accent"
                    disabled={loading || saving || !data || !model || !effort}
                    onClick={submit}
                  >
                    <Check size={13} strokeWidth={1.8} aria-hidden />
                    {saving ? "saving…" : "save"}
                  </Button>
                </div>
              </footer>
            )}
          </div>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
