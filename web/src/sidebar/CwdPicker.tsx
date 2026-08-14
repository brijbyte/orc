import { useEffect, useRef, useState } from "react";
import { Dialog } from "@base-ui/react/dialog";
import { Folder, FolderUp, X } from "lucide-react";
import { api } from "../lib/api";
import { Button } from "../ui/Button";
import d from "../ui/dialog.module.css";
import s from "./DirPicker.module.css";

export function CwdPicker({
  open,
  start,
  onPick,
  onCancel,
}: {
  open: boolean;
  start: string;
  onPick: (path: string) => void;
  onCancel: () => void;
}) {
  const [path, setPath] = useState(start);
  const [input, setInput] = useState(start);
  const [parent, setParent] = useState("");
  const [dirs, setDirs] = useState<string[]>([]);
  const [err, setErr] = useState("");
  const [loading, setLoading] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);

  const browse = (next: string) => {
    setLoading(true);
    api
      .dirs(next)
      .then((data) => {
        setPath(data.path);
        setInput(data.path);
        setParent(data.parent ?? "");
        setDirs(data.dirs ?? []);
        setErr("");
      })
      .catch(() => setErr("cannot read " + next))
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    if (open) browse(start);
  }, [open, start]);

  return (
    <Dialog.Root open={open} onOpenChange={(next) => !next && onCancel()}>
      <Dialog.Portal>
        <Dialog.Backdrop className={d.overlay} />
        <Dialog.Popup
          className={`${d.popup} ${s.popup}`}
          initialFocus={inputRef}
        >
          <Dialog.Title className={d.head}>
            change working directory
          </Dialog.Title>
          <div className={s.form}>
            <div className={s.path}>
              <input
                ref={inputRef}
                value={input}
                onChange={(event) => setInput(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key !== "Enter" || event.nativeEvent.isComposing)
                    return;
                  event.preventDefault();
                  browse(input);
                }}
                spellCheck={false}
              />
            </div>
            {err && <div className={s.err}>{err}</div>}
            <div className={s.list} aria-busy={loading}>
              {parent && (
                <Button className={s.dir} onClick={() => browse(parent)}>
                  <FolderUp size={14} strokeWidth={1.8} aria-hidden /> parent
                </Button>
              )}
              {loading && <div className={s.empty}>Loading folders…</div>}
              {!loading && !dirs.length && (
                <div className={s.empty}>No folders</div>
              )}
              {!loading &&
                dirs.map((dir) => (
                  <Button
                    className={s.dir}
                    key={dir}
                    onClick={() => browse(path.replace(/\/$/, "") + "/" + dir)}
                  >
                    <Folder size={14} strokeWidth={1.8} aria-hidden /> {dir}
                  </Button>
                ))}
            </div>
            <div className={d.foot}>
              <Dialog.Close render={<Button outline />}>
                <X size={13} strokeWidth={1.8} aria-hidden /> cancel
              </Dialog.Close>
              <Button outline tone="accent" onClick={() => onPick(path)}>
                use this directory
              </Button>
            </div>
          </div>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
