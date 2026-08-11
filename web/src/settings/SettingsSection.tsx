import type { LucideIcon } from "lucide-react";
import s from "./SettingsSection.module.css";

export function SettingsSection({
  icon: Icon,
  title,
  description,
  tone = "blue",
  children,
}: {
  icon: LucideIcon;
  title: string;
  description?: string;
  tone?: "blue" | "purple" | "orange" | "red" | "green";
  children: React.ReactNode;
}) {
  return (
    <section className={s.section}>
      <header className={s.hero}>
        <span className={s.icon} data-tone={tone}>
          <Icon size={26} strokeWidth={1.7} aria-hidden />
        </span>
        <h2>{title}</h2>
        {description && <p>{description}</p>}
      </header>
      <div className={s.card}>{children}</div>
    </section>
  );
}
