import { useEffect, useRef, useState } from "react";
import { api, type AttachedFile } from "./api";

const spinnerFrames = ["⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"];

const readB64 = (f: File) =>
  new Promise<AttachedFile>((resolve, reject) => {
    const r = new FileReader();
    r.onload = () =>
      resolve({
        name: f.name,
        type: f.type,
        data: (r.result as string).split(",", 2)[1] ?? "",
      });
    r.onerror = () => reject(r.error);
    r.readAsDataURL(f);
  });

function sizeLabel(n: number): string {
  return n < 1024 ? `${n} B` : `${Math.round(n / 1024)} KB`;
}

// InputBar owns the textarea (Enter sends, Shift+Enter breaks the line,
// Esc interrupts), attachment chips, the busy spinner, and the stop button.
export function InputBar({
  sid,
  busy,
  files,
  setFiles,
}: {
  sid: string;
  busy: boolean;
  files: File[];
  setFiles: (f: File[]) => void;
}) {
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
    if (!text && files.length === 0) return;
    const pending = files;
    setInput("");
    setFiles([]);
    requestAnimationFrame(autosize);
    if (pending.length === 0) {
      api.send(sid, text);
      return;
    }
    Promise.all(pending.map(readB64))
      .then((atts) => api.send(sid, text, atts))
      .catch(() => setFiles(pending)); // unreadable file: restore the chips
  };

  return (
    <form onSubmit={submit}>
      {files.length > 0 && (
        <div className="chips">
          {files.map((f, i) => (
            <span key={i} className="chip">
              📎 {f.name} <em>{sizeLabel(f.size)}</em>
              <button
                type="button"
                title="remove attachment"
                onClick={() => setFiles(files.filter((_, j) => j !== i))}
              >
                ✕
              </button>
            </span>
          ))}
        </div>
      )}
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
