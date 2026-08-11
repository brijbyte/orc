import { SettingsSection } from "./SettingsSection";
import { useSettings } from "./SettingsContext";
import s from "./DiagnosticsSettings.module.css";

function formatUptime(total: number) {
  const days = Math.floor(total / 86400);
  const hours = Math.floor((total % 86400) / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  const seconds = Math.floor(total % 60);
  return [
    days && `${days}d`,
    (days || hours) && `${hours}h`,
    (days || hours || minutes) && `${minutes}m`,
    `${seconds}s`,
  ]
    .filter(Boolean)
    .join(" ");
}

export function DiagnosticsSettings() {
  const { diagnostics } = useSettings();
  if (!diagnostics)
    return (
      <SettingsSection>
        <span className={s.loading}>loading diagnostics…</span>
      </SettingsSection>
    );

  const timer = diagnostics.update_timer;
  const next = timer.next_at ? new Date(timer.next_at) : null;

  return (
    <SettingsSection>
      <dl className={s.list}>
        <div>
          <dt>version</dt>
          <dd>{diagnostics.version}</dd>
        </div>
        <div>
          <dt>uptime</dt>
          <dd>{formatUptime(diagnostics.uptime_seconds)}</dd>
        </div>
        <div>
          <dt>update timer</dt>
          <dd>
            {timer.available
              ? [timer.active, timer.enabled].filter(Boolean).join(" · ")
              : "not installed"}
          </dd>
        </div>
        {next && (
          <div>
            <dt>next update</dt>
            <dd>{next.toLocaleString()}</dd>
          </div>
        )}
      </dl>
    </SettingsSection>
  );
}
