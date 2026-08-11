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
      <h2 className={s.title}>
        <Icon size={14} strokeWidth={1.8} aria-hidden />
        {title}
      </h2>
      {description && <p className={s.description}>{description}</p>}
      {children}
    </section>
  );
}
