import { useState } from "react";
import { Check, Copy } from "lucide-react";
import { Button } from "../ui/Button";
import s from "./CopyButton.module.css";

// CopyButton sits at a block's top-right corner, visible on hover; a brief
// check mark confirms the copy.
export function CopyButton({ text }: { text: string }) {
  const [done, setDone] = useState(false);
  if (!text) return null;
  const Icon = done ? Check : Copy;
  return (
    <Button
      icon
      tip={done ? "copied" : "copy"}
      className={s.copy}
      onClick={() => {
        navigator.clipboard.writeText(text);
        setDone(true);
        setTimeout(() => setDone(false), 1200);
      }}
    >
      <Icon size={13} strokeWidth={1.8} aria-hidden />
    </Button>
  );
}
