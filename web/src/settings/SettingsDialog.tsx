import { useEffect, useState } from "react";
import { Dialog } from "@base-ui/react/dialog";
import { Check, Settings, X } from "lucide-react";
import type { NotifyChannel } from "../lib/types";
import { Button } from "../ui/Button";
import d from "../ui/dialog.module.css";
import { GeneralSettings } from "./GeneralSettings";
import { NotificationSettings } from "./NotificationSettings";
import { PasswordSettings } from "./PasswordSettings";
import { useSettings } from "./SettingsContext";
import s from "./SettingsDialog.module.css";

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

  useEffect(() => {
    if (!open || !data) return;
    setModel(data.model);
    setEffort(data.effort);
    setChannels(data.channels);
    setSaveError("");
  }, [open, data]);

  const submit = () => {
    setSaving(true);
    setSaveError("");
    save({ model, effort }, channels)
      .then(closeDialog)
      .catch((err: Error) => setSaveError(err.message || "save failed"))
      .finally(() => setSaving(false));
  };

  const error = loadError || saveError;

  return (
    <Dialog.Root
      open={open}
      onOpenChange={(isOpen) => !isOpen && closeDialog()}
    >
      <Dialog.Portal>
        <Dialog.Backdrop className={d.overlay} />
        <Dialog.Popup className={d.popup}>
          <Dialog.Title className={s.title}>
            <Settings size={15} strokeWidth={1.8} aria-hidden />
            settings
          </Dialog.Title>
          <Dialog.Description className={s.description}>
            defaults and preferences for this orc server.
          </Dialog.Description>

          {loading && <span className={s.loading}>loading…</span>}
          {open && data && (
            <>
              <GeneralSettings
                model={model}
                effort={effort}
                onModelChange={setModel}
                onEffortChange={setEffort}
              />
              <PasswordSettings />
              <NotificationSettings
                channels={channels}
                onChange={setChannels}
              />
            </>
          )}

          {error && <span className={s.error}>{error}</span>}
          <div className={d.foot}>
            <Dialog.Close render={<Button outline />}>
              <X size={13} strokeWidth={1.8} aria-hidden />
              cancel
            </Dialog.Close>
            <Button
              outline
              tone="success"
              disabled={loading || saving || !data || !model || !effort}
              onClick={submit}
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
