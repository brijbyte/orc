import { useEffect, useRef, useState } from "react";
import { Dialog } from "@base-ui/react/dialog";
import {
  CheckSquare2,
  File,
  Folder,
  FolderUp,
  Paperclip,
  Square,
  X,
} from "lucide-react";
import { api } from "../lib/api";
import type { BrowseEntry, BrowseResult, ServerAttachment } from "../lib/types";
import { Button } from "../ui/Button";
import d from "../ui/dialog.module.css";
import s from "./ServerPicker.module.css";

const childPath = (path: string, name: string) =>
  path.replace(/\/$/, "") + "/" + name;

export function ServerPicker({
  sid,
  open,
  onAttach,
  onCancel,
}: {
  sid: string;
  open: boolean;
  onAttach: (files: ServerAttachment[]) => void;
  onCancel: () => void;
}) {
  const [data, setData] = useState<BrowseResult | null>(null);
  const [input, setInput] = useState("");
  const [selected, setSelected] = useState<ServerAttachment[]>([]);
  const [err, setErr] = useState("");
  const pathRef = useRef<HTMLInputElement>(null);

  const browse = (path?: string) => {
    api
      .browse(sid, path)
      .then((next: BrowseResult) => {
        setData(next);
        setInput(next.path);
        setErr("");
      })
      .catch(() => setErr("cannot read directory"));
  };

  useEffect(() => {
    if (!open) return;
    setSelected([]);
    browse();
  }, [open, sid]);

  const attachment = (entry: BrowseEntry): ServerAttachment => ({
    kind: "server",
    name: entry.name,
    path: childPath(data!.path, entry.name),
    dir: entry.dir,
    size: entry.size,
  });
  const picked = (path: string) => selected.some((file) => file.path === path);
  const toggle = (entry: BrowseEntry) => {
    const file = attachment(entry);
    setSelected((current) =>
      current.some((item) => item.path === file.path)
        ? current.filter((item) => item.path !== file.path)
        : [...current, file],
    );
  };

  return (
    <Dialog.Root open={open} onOpenChange={(next) => !next && onCancel()}>
      <Dialog.Portal>
        <Dialog.Backdrop className={d.overlay} />
        <Dialog.Popup
          className={`${d.popup} ${s.popup}`}
          initialFocus={pathRef}
        >
          <Dialog.Title className={d.head}>attach from server</Dialog.Title>
          <form
            className={s.path}
            onSubmit={(event) => {
              event.preventDefault();
              browse(input);
            }}
          >
            <input
              ref={pathRef}
              value={input}
              onChange={(event) => setInput(event.target.value)}
              spellCheck={false}
            />
          </form>
          {err && <div className={s.err}>{err}</div>}
          <div className={s.list}>
            {data?.parent && (
              <Button className={s.parent} onClick={() => browse(data.parent)}>
                <FolderUp size={14} strokeWidth={1.8} aria-hidden />
                parent
              </Button>
            )}
            {data?.entries.map((entry) => {
              const file = attachment(entry);
              const on = picked(file.path);
              const Icon = entry.dir ? Folder : File;
              return (
                <div
                  className={s.row}
                  data-selected={on || undefined}
                  key={entry.name}
                >
                  <Button
                    icon
                    tip={on ? `unselect ${entry.name}` : `select ${entry.name}`}
                    className={s.select}
                    tone={on ? "accent" : undefined}
                    onClick={() => toggle(entry)}
                  >
                    {on ? (
                      <CheckSquare2 size={14} strokeWidth={1.8} aria-hidden />
                    ) : (
                      <Square size={14} strokeWidth={1.8} aria-hidden />
                    )}
                  </Button>
                  <Button
                    className={s.entry}
                    onClick={() =>
                      entry.dir ? browse(file.path) : toggle(entry)
                    }
                  >
                    <Icon size={14} strokeWidth={1.8} aria-hidden />
                    <span>{entry.name}</span>
                  </Button>
                </div>
              );
            })}
          </div>
          <div className={d.foot}>
            <span className={s.count}>
              {selected.length
                ? `${selected.length} selected`
                : "select files or folders"}
            </span>
            <Dialog.Close render={<Button outline />}>
              <X size={13} strokeWidth={1.8} aria-hidden />
              cancel
            </Dialog.Close>
            <Button
              outline
              tone="success"
              disabled={!selected.length}
              onClick={() => {
                onAttach(selected);
                onCancel();
              }}
            >
              <Paperclip size={13} strokeWidth={1.8} aria-hidden />
              attach
            </Button>
          </div>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
