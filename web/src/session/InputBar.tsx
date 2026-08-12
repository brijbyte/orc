import { useEffect, useRef, useState } from "react";
import { Menu } from "@base-ui/react/menu";
import {
  Check,
  ChevronRight,
  Files,
  HardDrive,
  LoaderCircle,
  Paperclip,
  Send,
  Square,
  X,
} from "lucide-react";
import { api, type AttachedFile } from "../lib/api";
import type { ComposerAttachment, ServerAttachment } from "../lib/types";
import { Button } from "../ui/Button";
import { ServerPicker } from "./ServerPicker";
import s from "./InputBar.module.css";

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
  if (n < 1024) return `${n} B`;
  if (n < 1 << 20) return `${Math.round(n / 1024)} KB`;
  return `${(n / (1 << 20)).toFixed(n < 10 << 20 ? 1 : 0)} MB`;
}

function attachmentID(file: ComposerAttachment): string {
  return file instanceof File
    ? `local:${file.name}:${file.size}:${file.lastModified}`
    : `server:${file.path}`;
}

// isEditable reports whether typing already goes somewhere: form fields,
// or an open popup (dialog, select listbox) with its own key handling.
function isEditable(t: EventTarget | null): boolean {
  const el = t as HTMLElement | null;
  if (!el) return false;
  const tag = el.tagName;
  return (
    tag === "INPUT" ||
    tag === "TEXTAREA" ||
    tag === "SELECT" ||
    el.isContentEditable ||
    !!el.closest?.('[role="dialog"],[role="alertdialog"],[role="listbox"]')
  );
}

// InputBar owns the textarea (Enter sends, Shift+Enter breaks the line,
// Esc interrupts), attachments, status, and the send/stop action.
// While mounted it grabs focus Slack-style: stray
// printable keystrokes land in the textarea.
export function InputBar({
  sid,
  busy,
  complete,
  files,
  setFiles,
  addFiles,
  attachmentError,
  draft,
}: {
  sid: string;
  busy: boolean;
  complete: boolean;
  files: ComposerAttachment[];
  setFiles: (f: ComposerAttachment[]) => void;
  addFiles: (f: FileList | null) => void;
  attachmentError?: string;
  draft?: { text: string; request: number };
}) {
  const [input, setInput] = useState("");
  const [serverPicker, setServerPicker] = useState(false);
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const pickRef = useRef<HTMLInputElement>(null);

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

  useEffect(() => {
    if (!draft?.text) return;
    setInput((current) =>
      [current.trim(), draft.text].filter(Boolean).join("\n"),
    );
    requestAnimationFrame(() => {
      autosize();
      inputRef.current?.focus();
    });
  }, [draft?.request]);

  const submit = (e?: React.FormEvent) => {
    e?.preventDefault();
    const text = input.trim();
    if (!text && files.length === 0) return;
    const pending = files;
    setInput("");
    setFiles([]);
    requestAnimationFrame(autosize);
    const local = pending.filter((file): file is File => file instanceof File);
    const paths = pending
      .filter((file): file is ServerAttachment => !(file instanceof File))
      .map((file) => file.path);
    Promise.all(local.map(readB64))
      .then((atts) => api.send(sid, text, atts, paths))
      .catch(() => setFiles(pending)); // unreadable file: restore the chips
  };

  return (
    <form className={s.composer} onSubmit={submit}>
      {files.length > 0 && (
        <div className={s.chips}>
          {files.map((f) => (
            <span
              key={attachmentID(f)}
              className={s.chip}
              title={f instanceof File ? f.name : f.path}
            >
              <Paperclip size={12} strokeWidth={1.8} aria-hidden />
              {f.name}
              {(f instanceof File || !f.dir) && <em>{sizeLabel(f.size)}</em>}
              <Button
                icon
                tone="danger"
                tip="remove attachment"
                onClick={() =>
                  setFiles(
                    files.filter(
                      (item) => attachmentID(item) !== attachmentID(f),
                    ),
                  )
                }
              >
                <X size={12} strokeWidth={1.8} aria-hidden />
              </Button>
            </span>
          ))}
        </div>
      )}
      <div className={s.attachmentError} aria-live="polite">
        {attachmentError}
      </div>
      <div className={s.bar}>
        <span
          className={
            busy
              ? `${s.prompt} ${s.busy}`
              : complete
                ? `${s.prompt} ${s.complete}`
                : s.prompt
          }
          aria-hidden
        >
          {busy ? (
            <LoaderCircle size={13} strokeWidth={1.8} />
          ) : complete ? (
            <Check size={13} strokeWidth={2} />
          ) : (
            <ChevronRight size={13} strokeWidth={2} />
          )}
        </span>
        <span className={s.live} role="status" aria-live="polite" aria-atomic>
          {busy ? "Turn in progress" : complete ? "Turn complete" : ""}
        </span>
        <Menu.Root>
          <Menu.Trigger
            render={<Button icon aria-label="attach" title="attach" />}
          >
            <Paperclip size={14} strokeWidth={1.8} aria-hidden />
          </Menu.Trigger>
          <Menu.Portal>
            <Menu.Positioner className={s.menuPositioner} sideOffset={5}>
              <Menu.Popup className={s.menu}>
                <Menu.Item
                  render={<Button className={s.menuItem} />}
                  onClick={() => pickRef.current?.click()}
                >
                  <Files size={14} strokeWidth={1.8} aria-hidden />
                  from local
                </Menu.Item>
                <Menu.Item
                  render={<Button className={s.menuItem} />}
                  onClick={() => setServerPicker(true)}
                >
                  <HardDrive size={14} strokeWidth={1.8} aria-hidden />
                  from server
                </Menu.Item>
              </Menu.Popup>
            </Menu.Positioner>
          </Menu.Portal>
        </Menu.Root>
        <div className={s.field}>
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
          <Button
            outline
            tone="accent"
            disabled={!input.trim() && files.length === 0}
            onClick={() => submit()}
          >
            <Send size={13} strokeWidth={1.8} aria-hidden />
            {busy ? "queue" : "send"}
          </Button>
          {busy && (
            <Button outline tone="danger" onClick={() => api.interrupt(sid)}>
              <Square size={11} fill="currentColor" aria-hidden /> stop
            </Button>
          )}
        </div>
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
      </div>
      <ServerPicker
        sid={sid}
        open={serverPicker}
        onCancel={() => setServerPicker(false)}
        onAttach={(selected) => setFiles([...files, ...selected])}
      />
    </form>
  );
}
