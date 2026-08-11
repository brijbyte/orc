import { useState } from "react";
import { KeyRound } from "lucide-react";
import { Button } from "../ui/Button";
import { SettingsSection } from "./SettingsSection";
import { useSettings } from "./SettingsContext";
import s from "./PasswordSettings.module.css";

export function PasswordSettings() {
  const { changePassword } = useSettings();
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [state, setState] = useState("");
  const [busy, setBusy] = useState(false);

  const submit = (event: React.FormEvent) => {
    event.preventDefault();
    setBusy(true);
    setState("");
    changePassword(current, next)
      .then(() => {
        setState("changed — sign in again");
        window.setTimeout(() => window.location.reload(), 900);
      })
      .catch((err: Error) => setState(err.message || "password change failed"))
      .finally(() => setBusy(false));
  };

  return (
    <SettingsSection icon={KeyRound} title="password">
      <form className={s.form} onSubmit={submit}>
        <label className={s.setting}>
          <span>current password</span>
          <input
            type="password"
            autoComplete="current-password"
            value={current}
            onChange={(event) => setCurrent(event.target.value)}
          />
        </label>
        <label className={s.setting}>
          <span>new password</span>
          <input
            type="password"
            autoComplete="new-password"
            minLength={8}
            maxLength={72}
            value={next}
            onChange={(event) => setNext(event.target.value)}
          />
        </label>
        <div className={s.action}>
          <Button
            type="submit"
            small
            outline
            disabled={busy || !current || next.length < 8}
          >
            {busy ? "changing…" : "change password"}
          </Button>
          {state && (
            <span className={state.startsWith("changed") ? s.ok : s.error}>
              {state}
            </span>
          )}
        </div>
      </form>
    </SettingsSection>
  );
}
