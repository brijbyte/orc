import s from "./SettingsSection.module.css";

export function SettingsSection({ children }: { children: React.ReactNode }) {
  return (
    <section className={s.section}>
      <div className={s.card}>{children}</div>
    </section>
  );
}
