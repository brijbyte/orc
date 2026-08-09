import { useEffect, useRef, useState } from "react";
import { Dialog } from "@base-ui/react/dialog";
import { api } from "./api";

// DirPicker browses server directories for a new session's cwd.
export function DirPicker({
  start,
  onPick,
  onCancel,
}: {
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

  return (
    <Dialog.Root open onOpenChange={(o) => !o && onCancel()}>
      <Dialog.Portal>
        <Dialog.Backdrop className="overlay" />
        <Dialog.Popup className="picker" initialFocus={pathRef}>
          <Dialog.Title className="phead">start a session in…</Dialog.Title>
          <form
            className="ppath"
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
          {err && <div className="perr">{err}</div>}
          <div className="plist">
            {parent && (
              <button className="pdir" onClick={() => browse(parent)}>
                ..
              </button>
            )}
            {dirs.map((d) => (
              <button
                key={d}
                className="pdir"
                onClick={() => browse(path.replace(/\/$/, "") + "/" + d)}
              >
                {d}/
              </button>
            ))}
          </div>
          <div className="pfoot">
            {newName === null ? (
              <button className="pmk" onClick={() => setNewName("")}>
                + new folder
              </button>
            ) : (
              <form
                className="pmkform"
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
            <Dialog.Close>cancel</Dialog.Close>
            <button className="pgo" onClick={() => onPick(path)}>
              start here
            </button>
          </div>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
