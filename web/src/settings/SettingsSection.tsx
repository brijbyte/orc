import type { LucideIcon } from "lucide-react";
import s from "./SettingsSection.module.css";

export function SettingsSection({
  icon: Icon,
  title,
  description,
  children,
}: {
  icon: LucideIcon;
  title: string;
  description?: string;
  children: React.ReactNode;
}) {
  return (
    <section className={s.section}>
      <header className={s.hero}>
        <Icon className={s.icon} size={34} strokeWidth={1.5} aria-hidden />
        <h2>{title}</h2>
        {description && <p>{description}</p>}
      </header>
      <div className={s.card}>{children}</div>
    </section>
  );
}
