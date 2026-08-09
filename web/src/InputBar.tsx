import { useEffect, useRef, useState } from "react";
import { api } from "./api";

const spinnerFrames = ["⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"];

// InputBar owns the textarea (Enter sends, Shift+Enter breaks the line,
// Esc interrupts), the busy spinner, and the stop button.
export function InputBar({ sid, busy }: { sid: string; busy: boolean }) {
  const [input, setInput] = useState("");
  const [spin, setSpin] = useState(0);
  const inputRef = useRef<HTMLTextAreaElement>(null);

  useEffect(() => {
    if (!busy) return;
    let last = 0;
    let raf = 0;
    const tick = (ts: number) => {
      if (!last) last = ts;
      if (ts - last >= 100) {
        setSpin((s) => (s + 1) % spinnerFrames.length);
        last = ts;
      }
      raf = requestAnimationFrame(tick);
    };
    raf = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(raf);
  }, [busy]);

  // grow with content, up to the CSS max-height
  const autosize = () => {
    const el = inputRef.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = el.scrollHeight + "px";
  };

  const submit = (e?: React.FormEvent) => {
    e?.preventDefault();
    const text = input.trim();
    if (!text) return;
    setInput("");
    requestAnimationFrame(autosize);
    api.send(sid, text);
  };

  return (
    <form onSubmit={submit}>
      <div className="bar">
        <span className={busy ? "prompt busy" : "prompt"}>
          {busy ? spinnerFrames[spin] : ">"}
        </span>
        <textarea
          ref={inputRef}
          rows={1}
          value={input}
          onChange={(e) => {
            setInput(e.target.value);
            autosize();
          }}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.shiftKey) {
              e.preventDefault();
              submit();
            } else if (e.key === "Escape" && busy) {
              api.interrupt(sid);
            }
          }}
          placeholder={
            busy ? "queue a message… (Esc interrupts)" : "message orc"
          }
          autoFocus
        />
        {busy && (
          <button type="button" onClick={() => api.interrupt(sid)}>
            stop
          </button>
        )}
      </div>
    </form>
  );
}
