import { useEffect, useRef, useState } from "react";
import { Dialog } from "@base-ui/react/dialog";
import { X } from "lucide-react";
import { api } from "./api";
import d from "./dialog.module.css";
import s from "./FileDrawer.module.css";

type FileData = { path: string; content: string; html?: string[] };

function plainLines(content: string): string[] {
  if (!content) return [];
  const lines = content.split("\n");
  if (lines.at(-1) === "") lines.pop();
  return lines;
}

export function FileDrawer({
  sid,
  path,
  request,
  onClose,
}: {
  sid: string;
  path: string;
  request: number;
  onClose: () => void;
}) {
  const [data, setData] = useState<FileData | null>(null);
  const [err, setErr] = useState("");
  const close = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (!path) return;
    let current = true;
    setData(null);
    setErr("");
    api
      .file(sid, path)
      .then((file) => current && setData(file))
      .catch(() => current && setErr(`cannot read ${path}`));
    return () => {
      current = false;
    };
  }, [sid, path, request]);

  const lines = data ? (data.html ?? plainLines(data.content)) : [];
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
            <Dialog.Close ref={close} className={s.close} aria-label="close file">
              <X size={17} strokeWidth={1.8} aria-hidden />
            </Dialog.Close>
          </header>
          <div className={s.body}>
            {!data && !err && <div className={s.message}>loading…</div>}
            {err && <div className={`${s.message} ${s.error}`}>{err}</div>}
            {data && lines.length === 0 && <div className={s.message}>(empty file)</div>}
            {data && lines.length > 0 && (
              <pre className={`${s.code} chroma`}>
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
