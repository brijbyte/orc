import { Select as Base } from "@base-ui/react/select";
import s from "./Select.module.css";

// Select is the compact select of the status bar.
export function Select({
  value,
  options,
  onChange,
  className,
}: {
  value: string;
  options: { value: string; title?: string }[];
  onChange: (v: string) => void;
  className?: string;
}) {
  return (
    <Base.Root value={value} onValueChange={(v) => v != null && onChange(v)}>
      <Base.Trigger
        className={className ? `${s.trigger} ${className}` : s.trigger}
      >
        <Base.Value />
      </Base.Trigger>
      <Base.Portal>
        <Base.Positioner className={s.pop} sideOffset={4}>
          <Base.Popup className={s.selPop}>
            {options.map((o) => (
              <Base.Item
                key={o.value}
                className={s.selItem}
                value={o.value}
                title={o.title}
              >
                <Base.ItemText>{o.value}</Base.ItemText>
              </Base.Item>
            ))}
          </Base.Popup>
        </Base.Positioner>
      </Base.Portal>
    </Base.Root>
  );
}
