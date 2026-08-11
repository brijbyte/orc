import { useEffect, useState } from "react";
import { Dialog } from "@base-ui/react/dialog";
import {
  Activity,
  BellRing,
  Boxes,
  Check,
  KeyRound,
  Palette,
  Search,
  Settings,
  X,
  type LucideIcon,
} from "lucide-react";
import type { NotifyChannel, Settings as SettingsData } from "../lib/types";
import { Button } from "../ui/Button";
import d from "../ui/dialog.module.css";
import { DiagnosticsSettings } from "./DiagnosticsSettings";
import { GeneralSettings } from "./GeneralSettings";
import { NotificationSettings } from "./NotificationSettings";
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
  const [channels, setChannels] = useState<NotifyChannel[]>([]);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState("");
  const [pane, setPane] = useState<Pane>("general");
  const [filter, setFilter] = useState("");

  useEffect(() => {
    if (!open || !data) return;
    setModel(data.model);
    setEffort(data.effort);
    setChannels(data.channels);
    setSaveError("");
    setFilter("");
  }, [open, data]);

  const defaults: Partial<SettingsData> = {};
  if (data && model !== data.model) defaults.model = model;
  if (data && effort !== data.effort) defaults.effort = effort;
  const defaultsDirty = Object.keys(defaults).length > 0;
  const channelsDirty = !!data && channels !== data.channels;
  const dirty = defaultsDirty || channelsDirty;

  const submit = async () => {
    if (!data || !dirty) return;
    setSaving(true);
    setSaveError("");
    try {
      await save(
        defaultsDirty ? defaults : undefined,
        channelsDirty ? channels : undefined,
      );
      closeDialog();
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : "save failed");
    } finally {
      setSaving(false);
    }
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
  const query = filter.trim().toLowerCase();
  const visibleItems = items.filter(
    (item) =>
      item.label.toLowerCase().includes(query) ||
      item.description.toLowerCase().includes(query),
  );
  const activeItem = items.find((item) => item.id === pane) ?? items[0];
  const ActiveIcon = activeItem.icon;
  const error = loadError || saveError;

  return (
    <Dialog.Root
      open={open}
      onOpenChange={(isOpen) => !isOpen && closeDialog()}
    >
      <Dialog.Portal>
        <Dialog.Backdrop className={d.overlay} />
        <Dialog.Popup className={`${d.popup} ${s.popup}`}>
          <aside className={s.sidebar}>
            <Dialog.Title className={s.title}>
              <Settings size={17} strokeWidth={1.8} aria-hidden />
              Settings
            </Dialog.Title>
            <Dialog.Description className={s.srOnly}>
              Defaults and preferences for this orc server.
            </Dialog.Description>
            <label className={s.search}>
              <Search size={14} strokeWidth={1.8} aria-hidden />
              <input
                value={filter}
                placeholder="Search"
                aria-label="search settings"
                onChange={(event) => setFilter(event.target.value)}
              />
            </label>
            <nav aria-label="settings sections">
              <ul className={s.navList}>
                {visibleItems.map((item) => {
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
              {!visibleItems.length && (
                <p className={s.noResults}>No matching settings</p>
              )}
            </nav>
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
              <Dialog.Close
                render={<Button icon />}
                aria-label="close settings"
              >
                <X size={16} strokeWidth={1.8} aria-hidden />
              </Dialog.Close>
            </header>

            <div className={s.content}>
              {loading && <span className={s.loading}>loading settings…</span>}
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

            <footer className={`${d.foot} ${s.footer}`}>
              {error && <span className={s.error}>{error}</span>}
              <Dialog.Close render={<Button outline />}>Cancel</Dialog.Close>
              <Button
                outline
                tone="success"
                disabled={
                  loading || saving || !data || !model || !effort || !dirty
                }
                onClick={submit}
              >
                <Check size={13} strokeWidth={1.8} aria-hidden />
                {saving ? "saving…" : "save"}
              </Button>
            </footer>
          </div>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
