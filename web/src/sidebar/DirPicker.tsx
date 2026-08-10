import { useEffect, useRef, useState } from "react";
import { Dialog } from "@base-ui/react/dialog";
import { api } from "../lib/api";
import { Button } from "../ui/Button";
import s from "./DirPicker.module.css";
import d from "../ui/dialog.module.css";

// DirPicker browses server directories for a new session's cwd.
export function DirPicker({
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
  const [newName, setNewName] = useState<string | null>(null);
  const picked = useRef<string | null>(null);
  const pathRef = useRef<HTMLInputElement>(null);

  const mkdir = () => {
    const name = (newName ?? "").trim();
    if (!name || name.includes("/")) return;
    const target = path.replace(/\/$/, "") + "/" + name;
    api
      .mkdir(target)
      .then(() => {
        setNewName(null);
        browse(target);
      })
      .catch(() => setErr("cannot create " + name));
  };

  const browse = (p: string) => {
    api
      .dirs(p)
      .then((d) => {
        setPath(d.path);
        setInput(d.path);
        setParent(d.parent ?? "");
        setDirs(d.dirs ?? []);
        setErr("");
      })
      .catch(() => setErr("cannot read " + p));
  };

  useEffect(() => browse(start), [start]);

  useEffect(() => {
    if (!open) return;
    picked.current = null;
    setNewName(null);
  }, [open]);

  return (
    <Dialog.Root
      open={open}
      onOpenChange={(isOpen) => !isOpen && onCancel()}
      onOpenChangeComplete={(isOpen) => {
        if (isOpen) return;
        const path = picked.current;
        picked.current = null;
        browse(start);
        if (path) onPick(path);
      }}
    >
      <Dialog.Portal>
        <Dialog.Backdrop className={d.overlay} />
        <Dialog.Popup
          className={`${d.popup} ${s.popup}`}
          initialFocus={pathRef}
        >
          <Dialog.Title className={d.head}>start a session in…</Dialog.Title>
          <form
            className={s.path}
            onSubmit={(e) => {
              e.preventDefault();
              browse(input);
            }}
          >
            <input
              ref={pathRef}
              value={input}
              onChange={(e) => setInput(e.target.value)}
              spellCheck={false}
            />
          </form>
          {err && <div className={s.err}>{err}</div>}
          <div className={s.list}>
            {parent && (
              <Button className={s.dir} onClick={() => browse(parent)}>
                ..
              </Button>
            )}
            {dirs.map((d) => (
              <Button
                key={d}
                className={s.dir}
                onClick={() => browse(path.replace(/\/$/, "") + "/" + d)}
              >
                {d}/
              </Button>
            ))}
          </div>
          <div className={d.foot}>
            {newName === null ? (
              <Button outline onClick={() => setNewName("")}>
                + new folder
              </Button>
            ) : (
              <form
                className={s.mkform}
                onSubmit={(e) => {
                  e.preventDefault();
                  mkdir();
                }}
              >
                <input
                  value={newName}
                  onChange={(e) => setNewName(e.target.value)}
                  onKeyDown={(e) => {
                    // Esc closes only the inline form, not the dialog.
                    if (e.key === "Escape") {
                      e.stopPropagation();
                      setNewName(null);
                    }
                  }}
                  placeholder="folder name"
                  spellCheck={false}
                  autoFocus
                />
              </form>
            )}
            <Dialog.Close render={<Button outline />}>cancel</Dialog.Close>
            <Button
              outline
              tone="success"
              onClick={() => {
                picked.current = path;
                onCancel();
              }}
            >
              start here
            </Button>
          </div>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
