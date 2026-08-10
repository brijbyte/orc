import { useEffect, useRef, useState } from "react";
import { Dialog } from "@base-ui/react/dialog";
import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import { X } from "lucide-react";
import { api } from "../lib/api";
import { Button } from "../ui/Button";
import d from "../ui/dialog.module.css";
import s from "./FileDrawer.module.css";

type FileData = { path: string; content: string; html?: string[] };
type View = "code" | "preview";

const mdComponents: Components = {
  table: ({ node: _, ...props }) => (
    <div className={s.tableWrap}>
      <table {...props} />
    </div>
  ),
};

function plainLines(content: string): string[] {
  if (!content) return [];
  const lines = content.split("\n");
  if (lines.at(-1) === "") lines.pop();
  return lines;
}

export function FileDrawer({
  sid,
  path,
  fileRef,
  line,
  request,
  onClose,
}: {
  sid: string;
  path: string;
  fileRef: string;
  line?: number;
  request: number;
  onClose: () => void;
}) {
  const [data, setData] = useState<FileData | null>(null);
  const [err, setErr] = useState("");
  const [view, setView] = useState<View>("code");
  const close = useRef<HTMLButtonElement>(null);
  const target = useRef<HTMLDivElement>(null);
  const loaded = useRef("");

  useEffect(() => {
    if (!path) return;
    let current = true;
    const key = `${sid}:${fileRef}`;
    if (loaded.current !== key) {
      setData(null);
      setView("code");
    }
    setErr("");
    api
      .file(sid, fileRef)
      .then((file) => {
        if (!current) return;
        loaded.current = key;
        setData(file);
      })
      .catch(() => current && setErr(`cannot read ${path}`));
    return () => {
      current = false;
    };
  }, [sid, path, fileRef, request]);

  useEffect(() => {
    if (!data || !line) return;
    requestAnimationFrame(() =>
      target.current?.scrollIntoView({ block: "center" }),
    );
  }, [data, line]);

  const lines = data ? (data.html ?? plainLines(data.content)) : [];
  const markdown = /\.md$/i.test(data?.path ?? path);
  const preview = markdown && view === "preview";
  return (
    <Dialog.Root open={!!path} onOpenChange={(open) => !open && onClose()}>
      <Dialog.Portal>
        <Dialog.Backdrop className={`${d.overlay} ${s.overlay}`} />
        <Dialog.Popup className={s.drawer} initialFocus={close}>
          <header className={s.head}>
            <Dialog.Title className={s.title} title={data?.path ?? path}>
              {path}
            </Dialog.Title>
            {markdown && (
              <div className={s.tabs} role="tablist" aria-label="file view">
                {(["code", "preview"] as View[]).map((tab) => (
                  <Button
                    small
                    role="tab"
                    aria-selected={view === tab}
                    className={s.tab}
                    data-active={view === tab || undefined}
                    onClick={() => setView(tab)}
                    key={tab}
                  >
                    {tab === "code" ? "Code" : "Preview"}
                  </Button>
                ))}
              </div>
            )}
            <Dialog.Close
              ref={close}
              render={<Button icon className={s.close} />}
              aria-label="close file"
            >
              <X size={17} strokeWidth={1.8} aria-hidden />
            </Dialog.Close>
          </header>
          <div className={s.body} data-preview={preview || undefined}>
            {!data && !err && <div className={s.message}>loading…</div>}
            {err && <div className={`${s.message} ${s.error}`}>{err}</div>}
            {data && preview && !data.content && (
              <div className={s.message} role="tabpanel">
                (empty file)
              </div>
            )}
            {data && preview && data.content && (
              <article className={s.markdown} role="tabpanel">
                <ReactMarkdown
                  remarkPlugins={[remarkGfm]}
                  components={mdComponents}
                >
                  {data.content}
                </ReactMarkdown>
              </article>
            )}
            {data && !preview && lines.length === 0 && (
              <div className={s.message}>(empty file)</div>
            )}
            {data && !preview && lines.length > 0 && (
              <pre className={`${s.code} chroma`} role="tabpanel">
                {lines.map((text, i) => (
                  <div
                    ref={i + 1 === line ? target : undefined}
                    className={s.line}
                    data-target={i + 1 === line || undefined}
                    key={i}
                  >
                    <span className={s.number}>{i + 1}</span>
                    {data.html ? (
                      <span dangerouslySetInnerHTML={{ __html: text }} />
                    ) : (
                      <span>{text}</span>
                    )}
                  </div>
                ))}
              </pre>
            )}
          </div>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
