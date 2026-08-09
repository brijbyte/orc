import { Select } from "@base-ui/react/select";
import { Tooltip } from "@base-ui/react/tooltip";
import s from "./ui.module.css";

// TipBtn is a button with an accessible tooltip (hover and focus).
export function TipBtn({
  tip,
  ...props
}: { tip: string } & React.ComponentProps<"button">) {
  return (
    <Tooltip.Root>
      <Tooltip.Trigger type="button" aria-label={tip} {...props} />
      <Tooltip.Portal>
        <Tooltip.Positioner className={s.pop} sideOffset={5}>
          <Tooltip.Popup className={s.tip}>{tip}</Tooltip.Popup>
        </Tooltip.Positioner>
      </Tooltip.Portal>
    </Tooltip.Root>
  );
}

// Sel is a compact select for the status bar.
export function Sel({
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
    <Select.Root value={value} onValueChange={(v) => v != null && onChange(v)}>
      <Select.Trigger className={className ? `${s.trigger} ${className}` : s.trigger}>
        <Select.Value />
      </Select.Trigger>
      <Select.Portal>
        <Select.Positioner className={s.pop} sideOffset={4}>
          <Select.Popup className={s.selPop}>
            {options.map((o) => (
              <Select.Item
                key={o.value}
                className={s.selItem}
                value={o.value}
                title={o.title}
              >
                <Select.ItemText>{o.value}</Select.ItemText>
              </Select.Item>
            ))}
          </Select.Popup>
        </Select.Positioner>
      </Select.Portal>
    </Select.Root>
  );
}
