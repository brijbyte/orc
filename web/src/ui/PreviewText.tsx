import { PreviewCard } from "@base-ui/react/preview-card";
import s from "./PreviewText.module.css";

// PreviewText truncates a label in place and exposes its full value on
// hover, focus, or touch without widening the surrounding toolbar.
export function PreviewText({
  text,
  className,
}: {
  text: string;
  className?: string;
}) {
  return (
    <PreviewCard.Root>
      <PreviewCard.Trigger
        render={
          <span
            className={[s.trigger, className].filter(Boolean).join(" ")}
            tabIndex={0}
          />
        }
      >
        {text}
      </PreviewCard.Trigger>
      <PreviewCard.Portal>
        <PreviewCard.Positioner className={s.positioner} sideOffset={6}>
          <PreviewCard.Popup className={s.popup}>{text}</PreviewCard.Popup>
        </PreviewCard.Positioner>
      </PreviewCard.Portal>
    </PreviewCard.Root>
  );
}
