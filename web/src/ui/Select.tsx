import { Select as Base } from "@base-ui/react/select";
import { PreviewCard } from "@base-ui/react/preview-card";
import { Info } from "lucide-react";
import s from "./Select.module.css";

// Select is the compact select of the status bar.
export function Select({
  value,
  options,
  onChange,
  className,
}: {
  value: string;
  options: { value: string; label?: string; title?: string }[];
  onChange: (v: string) => void;
  className?: string;
}) {
  return (
    <Base.Root value={value} onValueChange={(v) => v != null && onChange(v)}>
      <Base.Trigger
        className={className ? `${s.trigger} ${className}` : s.trigger}
      >
        {options.find((option) => option.value === value)?.label ?? value}
      </Base.Trigger>
      <Base.Portal>
        <Base.Positioner className={s.pop} sideOffset={4}>
          <Base.Popup className={s.selPop}>
            {options.map((o) => (
              <Base.Item key={o.value} className={s.selItem} value={o.value}>
                <Base.ItemText>{o.label ?? o.value}</Base.ItemText>
                {o.title && <ItemInfo text={o.title} />}
              </Base.Item>
            ))}
          </Base.Popup>
        </Base.Positioner>
      </Base.Portal>
    </Base.Root>
  );
}

// ItemInfo is an info icon whose hover/focus preview card holds the
// option's full description.
function ItemInfo({ text }: { text: string }) {
  return (
    <PreviewCard.Root>
      <PreviewCard.Trigger
        render={<span className={s.info} tabIndex={0} aria-label={text} />}
      >
        <Info size={13} strokeWidth={1.8} aria-hidden />
      </PreviewCard.Trigger>
      <PreviewCard.Portal>
        <PreviewCard.Positioner className={s.pop} side="right" sideOffset={8}>
          <PreviewCard.Popup className={s.infoPop}>{text}</PreviewCard.Popup>
        </PreviewCard.Positioner>
      </PreviewCard.Portal>
    </PreviewCard.Root>
  );
}
