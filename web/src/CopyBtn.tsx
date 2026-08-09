import { useState } from "react";
import { Check, Copy } from "lucide-react";
import { TipBtn } from "./ui";
import s from "./CopyBtn.module.css";

// CopyBtn sits at a block's top-right corner, visible on hover; a brief
// check mark confirms the copy.
export function CopyBtn({ text }: { text: string }) {
  const [done, setDone] = useState(false);
  if (!text) return null;
  const Icon = done ? Check : Copy;
  return (
    <TipBtn
      tip={done ? "copied" : "copy"}
      className={s.copy}
      onClick={() => {
        navigator.clipboard.writeText(text);
        setDone(true);
        setTimeout(() => setDone(false), 1200);
      }}
    >
      <Icon size={13} strokeWidth={1.8} aria-hidden />
    </TipBtn>
  );
}
