import { useEffect, useMemo, useRef, useState } from "react";
import { Dialog } from "@base-ui/react/dialog";
import { Folder, FolderPlus, FolderUp, Play, X } from "lucide-react";
import { api } from "../lib/api";
import type { Model } from "../lib/types";
import { Button } from "../ui/Button";
import s from "./DirPicker.module.css";
import d from "../ui/dialog.module.css";

type Choice = {
  path: string;
  routine: string;
  model: string;
  effort: string;
};

// DirPicker browses server directories and configures a new session.
export function DirPicker({
  open,
  start,
  models,
  defaultModel,
  defaultEffort,
  onPick,
  onCancel,
}: {
  open: boolean;
  start: string;
  models: Model[];
  defaultModel: string;
  defaultEffort: string;
  onPick: (
    path: string,
    routine: string,
    model: string,
    effort: string,
  ) => void;
  onCancel: () => void;
}) {
  const [path, setPath] = useState(start);
  const [input, setInput] = useState(start);
  const [parent, setParent] = useState("");
  const [dirs, setDirs] = useState<string[]>([]);
  const [err, setErr] = useState("");
  const [loading, setLoading] = useState(false);
  const [newName, setNewName] = useState<string | null>(null);
  const [routine, setRoutine] = useState("");
  const [model, setModel] = useState(defaultModel);
  const [effort, setEffort] = useState(defaultEffort);
  const picked = useRef<Choice | null>(null);
  const pathRef = useRef<HTMLInputElement>(null);
  const formRef = useRef<HTMLFormElement>(null);

  const efforts = useMemo(
    () =>
      models.find((item) => item.slug === model)?.efforts ?? [
        "low",
        "medium",
        "high",
      ],
    [models, model],
  );
  const modelOptions = models.some((item) => item.slug === model)
    ? models
    : model
      ? [{ slug: model, name: model, description: "" }, ...models]
      : models;

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
    setLoading(true);
    api
      .dirs(p)
      .then((data) => {
        setPath(data.path);
        setInput(data.path);
        setParent(data.parent ?? "");
        setDirs(data.dirs ?? []);
        setErr("");
      })
      .catch(() => setErr("cannot read " + p))
      .finally(() => setLoading(false));
  };

  const startSession = () => {
    if (!model || !effort) return;
    picked.current = {
      path,
      routine: routine.trim(),
      model,
      effort,
    };
    onCancel();
  };

  const selectModel = (nextModel: string) => {
    const nextEfforts = models.find((item) => item.slug === nextModel)
      ?.efforts ?? ["low", "medium", "high"];
    setModel(nextModel);
    if (!nextEfforts.includes(effort))
      setEffort(
        nextEfforts.includes("medium") ? "medium" : (nextEfforts[0] ?? effort),
      );
  };

  useEffect(() => browse(start), [start]);

  useEffect(() => {
    if (!open) return;
    picked.current = null;
    setNewName(null);
    setModel(defaultModel);
    setEffort(defaultEffort);
  }, [open, defaultModel, defaultEffort]);

  return (
    <Dialog.Root
      open={open}
      onOpenChange={(isOpen) => !isOpen && onCancel()}
      onOpenChangeComplete={(isOpen) => {
        if (isOpen) return;
        const choice = picked.current;
        picked.current = null;
        browse(start);
        setRoutine("");
        if (choice)
          onPick(choice.path, choice.routine, choice.model, choice.effort);
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
            ref={formRef}
            className={s.form}
            onSubmit={(event) => {
              event.preventDefault();
              startSession();
            }}
            onKeyDown={(event) => {
              if (
                event.key === "Enter" &&
                (event.metaKey || event.ctrlKey) &&
                !event.nativeEvent.isComposing
              ) {
                event.preventDefault();
                formRef.current?.requestSubmit();
              }
            }}
          >
            <div className={s.path}>
              <input
                ref={pathRef}
                value={input}
                onChange={(event) => setInput(event.target.value)}
                onKeyDown={(event) => {
                  if (
                    event.key === "Enter" &&
                    !event.metaKey &&
                    !event.ctrlKey &&
                    !event.nativeEvent.isComposing
                  ) {
                    event.preventDefault();
                    browse(input);
                  }
                }}
                spellCheck={false}
              />
            </div>
            {err && <div className={s.err}>{err}</div>}
            <div className={s.list} aria-busy={loading}>
              {parent && (
                <Button className={s.dir} onClick={() => browse(parent)}>
                  <FolderUp size={14} strokeWidth={1.8} aria-hidden />
                  parent
                </Button>
              )}
              {loading && <div className={s.empty}>Loading folders…</div>}
              {!loading && !dirs.length && (
                <div className={s.empty}>No folders</div>
              )}
              {!loading &&
                dirs.map((dir) => (
                  <Button
                    key={dir}
                    className={s.dir}
                    onClick={() => browse(path.replace(/\/$/, "") + "/" + dir)}
                  >
                    <Folder size={14} strokeWidth={1.8} aria-hidden />
                    {dir}
                  </Button>
                ))}
            </div>
            <div className={s.options}>
              <label>
                model
                <select
                  value={model}
                  onChange={(event) => selectModel(event.target.value)}
                  required
                >
                  {modelOptions.map((item) => (
                    <option key={item.slug} value={item.slug}>
                      {item.name || item.slug}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                effort
                <select
                  value={effort}
                  onChange={(event) => setEffort(event.target.value)}
                  required
                >
                  {efforts.map((value) => (
                    <option key={value} value={value}>
                      {value}
                    </option>
                  ))}
                </select>
              </label>
            </div>
            <label className={s.routine}>
              routine mission <span>(optional)</span>
              <textarea
                value={routine}
                onChange={(event) => setRoutine(event.target.value)}
                placeholder="Check the time, report changes, then sleep again."
                rows={3}
              />
            </label>
            <div className={d.foot}>
              {newName === null ? (
                <Button outline onClick={() => setNewName("")}>
                  <FolderPlus size={14} strokeWidth={1.8} aria-hidden />
                  new folder
                </Button>
              ) : (
                <div className={s.mkform}>
                  <input
                    value={newName}
                    onChange={(event) => setNewName(event.target.value)}
                    onKeyDown={(event) => {
                      // Esc closes only the inline form, not the dialog.
                      if (event.key === "Escape") {
                        event.stopPropagation();
                        setNewName(null);
                      } else if (
                        event.key === "Enter" &&
                        !event.metaKey &&
                        !event.ctrlKey &&
                        !event.nativeEvent.isComposing
                      ) {
                        event.preventDefault();
                        mkdir();
                      }
                    }}
                    placeholder="folder name"
                    spellCheck={false}
                    autoFocus
                  />
                  <Button small onClick={() => setNewName(null)}>
                    cancel
                  </Button>
                </div>
              )}
              <Dialog.Close render={<Button outline />}>
                <X size={13} strokeWidth={1.8} aria-hidden />
                cancel
              </Dialog.Close>
              <Button type="submit" outline tone="accent">
                <Play size={13} strokeWidth={1.8} aria-hidden />
                start here
              </Button>
            </div>
          </form>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
