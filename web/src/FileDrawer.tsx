import { useEffect, useRef, useState } from "react";
import { Dialog } from "@base-ui/react/dialog";
import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import { X } from "lucide-react";
import { api } from "./api";
import d from "./dialog.module.css";
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
  request,
  onClose,
}: {
  sid: string;
  path: string;
  fileRef: string;
  request: number;
  onClose: () => void;
}) {
  const [data, setData] = useState<FileData | null>(null);
  const [err, setErr] = useState("");
  const [view, setView] = useState<View>("code");
  const close = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (!path) return;
    let current = true;
    setData(null);
    setErr("");
    setView("code");
    api
      .file(sid, fileRef)
      .then((file) => current && setData(file))
      .catch(() => current && setErr(`cannot read ${path}`));
    return () => {
      current = false;
    };
  }, [sid, path, fileRef, request]);

  const lines = data ? (data.html ?? plainLines(data.content)) : [];
  const markdown = /\.md$/i.test(data?.path ?? path);
  const preview = markdown && view === "preview";
  return (
    <Dialog.Root
      open={!!path}
      onOpenChange={(open) => !open && onClose()}
    >
      <Dialog.Portal>
        <Dialog.Backdrop className={d.overlay} />
        <Dialog.Popup className={s.drawer} initialFocus={close}>
          <header className={s.head}>
            <Dialog.Title className={s.title} title={data?.path ?? path}>
              {path}
            </Dialog.Title>
            {markdown && (
              <div className={s.tabs} role="tablist" aria-label="file view">
                {(["code", "preview"] as View[]).map((tab) => (
                  <button
                    type="button"
                    role="tab"
                    aria-selected={view === tab}
                    className={s.tab}
                    data-active={view === tab || undefined}
                    onClick={() => setView(tab)}
                    key={tab}
                  >
                    {tab === "code" ? "Code" : "Preview"}
                  </button>
                ))}
              </div>
            )}
            <Dialog.Close ref={close} className={s.close} aria-label="close file">
              <X size={17} strokeWidth={1.8} aria-hidden />
            </Dialog.Close>
          </header>
          <div className={s.body} data-preview={preview || undefined}>
            {!data && !err && <div className={s.message}>loading…</div>}
            {err && <div className={`${s.message} ${s.error}`}>{err}</div>}
            {data && preview && !data.content && (
              <div className={s.message} role="tabpanel">(empty file)</div>
            )}
            {data && preview && data.content && (
              <article className={s.markdown} role="tabpanel">
                <ReactMarkdown remarkPlugins={[remarkGfm]} components={mdComponents}>
                  {data.content}
                </ReactMarkdown>
              </article>
            )}
            {data && !preview && lines.length === 0 && (
              <div className={s.message}>(empty file)</div>
            )}
            {data && !preview && lines.length > 0 && (
              <pre className={`${s.code} chroma`} role="tabpanel">
                {lines.map((line, i) => (
                  <div className={s.line} key={i}>
                    <span className={s.number}>{i + 1}</span>
                    {data.html ? (
                      <span dangerouslySetInnerHTML={{ __html: line }} />
                    ) : (
                      <span>{line}</span>
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
