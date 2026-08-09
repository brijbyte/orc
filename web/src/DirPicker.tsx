import { useEffect, useState } from "react";
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
    <div className="overlay" onClick={onCancel}>
      <div className="picker" onClick={(e) => e.stopPropagation()}>
        <div className="phead">start a session in…</div>
        <form
          className="ppath"
          onSubmit={(e) => {
            e.preventDefault();
            browse(input);
          }}
        >
          <input
            value={input}
            onChange={(e) => setInput(e.target.value)}
            spellCheck={false}
            autoFocus
          />
        </form>
        {err && <div className="perr">{err}</div>}
        <div className="plist">
          {parent && (
            <div className="pdir" onClick={() => browse(parent)}>
              ..
            </div>
          )}
          {dirs.map((d) => (
            <div
              key={d}
              className="pdir"
              onClick={() => browse(path.replace(/\/$/, "") + "/" + d)}
            >
              {d}/
            </div>
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
                onKeyDown={(e) => e.key === "Escape" && setNewName(null)}
                placeholder="folder name"
                spellCheck={false}
                autoFocus
              />
            </form>
          )}
          <button onClick={onCancel}>cancel</button>
          <button className="pgo" onClick={() => onPick(path)}>
            start here
          </button>
        </div>
      </div>
    </div>
  );
}
