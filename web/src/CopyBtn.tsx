import { useState } from "react";
import { Check, Copy } from "lucide-react";

// CopyBtn sits at a block's top-right corner, visible on hover; a brief
// check mark confirms the copy.
export function CopyBtn({ text }: { text: string }) {
  const [done, setDone] = useState(false);
  if (!text) return null;
  const Icon = done ? Check : Copy;
  return (
    <button
      type="button"
      className="copy"
      title="copy"
      aria-label="copy"
      onClick={() => {
        navigator.clipboard.writeText(text);
        setDone(true);
        setTimeout(() => setDone(false), 1200);
      }}
    >
      <Icon size={13} strokeWidth={1.8} aria-hidden />
    </button>
  );
}
