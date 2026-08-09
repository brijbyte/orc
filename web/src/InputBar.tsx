import { useEffect, useRef, useState } from "react";
import { Paperclip } from "lucide-react";
import { api, type AttachedFile } from "./api";
import { TipBtn } from "./ui";

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

// isEditable reports whether typing already goes somewhere: form fields,
// or an open popup (dialog, select listbox) with its own key handling.
function isEditable(t: EventTarget | null): boolean {
  const el = t as HTMLElement | null;
  if (!el) return false;
  const tag = el.tagName;
  return (
    tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" ||
    el.isContentEditable ||
    !!el.closest?.('[role="dialog"],[role="alertdialog"],[role="listbox"]')
  );
}

// InputBar owns the textarea (Enter sends, Shift+Enter breaks the line,
// Esc interrupts), attachment chips, the attach button, the busy spinner,
// and the stop button. While mounted it grabs focus Slack-style: stray
// printable keystrokes land in the textarea.
export function InputBar({
  sid,
  busy,
  files,
  setFiles,
  addFiles,
}: {
  sid: string;
  busy: boolean;
  files: File[];
  setFiles: (f: File[]) => void;
  addFiles: (f: FileList | null) => void;
}) {
  const [input, setInput] = useState("");
  const [spin, setSpin] = useState(0);
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const pickRef = useRef<HTMLInputElement>(null);

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

  // focus on mount and after files attach
  useEffect(() => {
    inputRef.current?.focus();
  }, [files.length]);

  // Slack-style: a printable key typed anywhere focuses the textarea; the
  // browser then delivers the character there, so nothing typed is lost.
  useEffect(() => {
    const grab = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey || e.key.length !== 1) return;
      if (isEditable(e.target)) return;
      // keep Space working as click on a focused button
      if (e.key === " " && (e.target as HTMLElement)?.tagName === "BUTTON")
        return;
      inputRef.current?.focus();
    };
    window.addEventListener("keydown", grab);
    return () => window.removeEventListener("keydown", grab);
  }, []);

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
    <form className="composer" onSubmit={submit}>
      {files.length > 0 && (
        <div className="chips">
          {files.map((f, i) => (
            <span key={i} className="chip">
              📎 {f.name} <em>{sizeLabel(f.size)}</em>
              <TipBtn
                tip="remove attachment"
                onClick={() => setFiles(files.filter((_, j) => j !== i))}
              >
                ✕
              </TipBtn>
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
        <input
          ref={pickRef}
          type="file"
          multiple
          hidden
          onChange={(e) => {
            addFiles(e.target.files);
            e.target.value = "";
          }}
        />
        <TipBtn
          tip="attach files"
          className="attach"
          onClick={() => pickRef.current?.click()}
        >
          <Paperclip size={14} strokeWidth={1.8} aria-hidden />
        </TipBtn>
        {busy && (
          <button type="button" onClick={() => api.interrupt(sid)}>
            stop
          </button>
        )}
      </div>
    </form>
  );
}
