import { Tooltip } from "@base-ui/react/tooltip";
import s from "./Button.module.css";

// Button is the app's only button. It is a quiet ghost by default: `outline`
// draws the frame of a committed action, `icon` makes a square tap target,
// `link` an underlined inline affordance, `nav` a full-width navigation row,
// `small` the inline size, and `tone` colors it. `tip` adds an accessible
// tooltip (hover and focus). Callers pass
// className for placement alone (position, margin, reveal-on-hover) — never
// for the button's own look.
export function Button({
  tip,
  outline,
  icon,
  link,
  nav,
  primary,
  small,
  tone,
  className,
  ...props
}: React.ComponentProps<"button"> & {
  tip?: string;
  outline?: boolean;
  icon?: boolean;
  link?: boolean;
  nav?: boolean;
  primary?: boolean;
  small?: boolean;
  tone?: "accent" | "success" | "danger";
}) {
  const cls = [
    s.btn,
    outline && s.outline,
    icon && s.icon,
    link && s.link,
    nav && s.nav,
    primary && s.primary,
    small && s.small,
    tone && s[tone],
    className,
  ]
    .filter(Boolean)
    .join(" ");
  if (!tip) return <button type="button" className={cls} {...props} />;
  return (
    <Tooltip.Root>
      <Tooltip.Trigger
        type="button"
        aria-label={tip}
        className={cls}
        {...props}
      />
      <Tooltip.Portal>
        <Tooltip.Positioner className={s.pop} sideOffset={5}>
          <Tooltip.Popup className={s.tip}>{tip}</Tooltip.Popup>
        </Tooltip.Positioner>
      </Tooltip.Portal>
    </Tooltip.Root>
  );
}
