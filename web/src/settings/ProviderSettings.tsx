import { useState } from "react";
import { Check, ExternalLink, LogIn } from "lucide-react";
import { Button } from "../ui/Button";
import { SettingsSection } from "./SettingsSection";
import { useSettings } from "./SettingsContext";
import s from "./ProviderSettings.module.css";

export function ProviderSettings() {
  const { providerAuth, startProviderLogin, completeProviderLogin } =
    useSettings();
  const [url, setURL] = useState("");
  const [callback, setCallback] = useState("");
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");

  if (!providerAuth) return null;

  const providerName = providerAuth.provider
    ? providerAuth.provider[0].toUpperCase() + providerAuth.provider.slice(1)
    : "Provider";
  const expires = providerAuth.expires_at
    ? new Date(providerAuth.expires_at)
    : null;
  const expired = !!expires && expires.getTime() <= Date.now();

  const start = async () => {
    setBusy(true);
    setMessage("");
    try {
      const nextURL = await startProviderLogin();
      setURL(nextURL);
      window.open(nextURL, "_blank", "noopener,noreferrer");
    } catch {
      setMessage("could not start sign-in");
    } finally {
      setBusy(false);
    }
  };

  const complete = async (event: React.FormEvent) => {
    event.preventDefault();
    setBusy(true);
    setMessage("");
    try {
      await completeProviderLogin(callback);
      setURL("");
      setCallback("");
      setMessage("signed in");
    } catch (err) {
      setMessage(err instanceof Error ? err.message : "sign-in failed");
    } finally {
      setBusy(false);
    }
  };

  return (
    <SettingsSection
      icon={LogIn}
      title={`${providerName} sign-in`}
      description="Manage the account orc uses for model requests."
    >
      <div className={s.status}>
        <span
          className={providerAuth.authenticated ? s.ok : s.off}
          data-expired={expired || undefined}
        >
          {providerAuth.authenticated ? "authenticated" : "not authenticated"}
        </span>
        {expires && (
          <span>
            token {expired ? "expired" : "expires"} {expires.toLocaleString()}
          </span>
        )}
      </div>
      {providerAuth.supported ? (
        <>
          <div className={s.actions}>
            <Button small outline disabled={busy} onClick={start}>
              <LogIn size={12} strokeWidth={1.8} aria-hidden />
              {providerAuth.authenticated ? "sign in again" : "start sign-in"}
            </Button>
            {url && (
              <Button
                small
                onClick={() =>
                  window.open(url, "_blank", "noopener,noreferrer")
                }
              >
                <ExternalLink size={12} strokeWidth={1.8} aria-hidden />
                open sign-in page
              </Button>
            )}
          </div>
          {url && (
            <form className={s.flow} onSubmit={complete}>
              <p>
                After ChatGPT redirects to localhost, copy the callback URL from
                the browser address bar and paste it here. The code alone also
                works.
              </p>
              <label>
                <span>callback URL or code</span>
                <input
                  value={callback}
                  autoComplete="off"
                  spellCheck={false}
                  onChange={(event) => setCallback(event.target.value)}
                />
              </label>
              <Button
                type="submit"
                small
                outline
                tone="success"
                disabled={busy || !callback.trim()}
              >
                <Check size={12} strokeWidth={1.8} aria-hidden />
                {busy ? "signing in…" : "complete sign-in"}
              </Button>
            </form>
          )}
        </>
      ) : (
        <span className={s.off}>
          web sign-in is unavailable for this provider.
        </span>
      )}
      {message && (
        <span className={message === "signed in" ? s.ok : s.error}>
          {message}
        </span>
      )}
    </SettingsSection>
  );
}
