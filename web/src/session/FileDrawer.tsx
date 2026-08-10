import { useEffect, useRef, useState } from "react";
import { AlertDialog } from "@base-ui/react/alert-dialog";
import { Dialog } from "@base-ui/react/dialog";
import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import { Code2, Eye, LoaderCircle, RefreshCw, Save, X } from "lucide-react";
import { api, APIError } from "../lib/api";
import { Button } from "../ui/Button";
import d from "../ui/dialog.module.css";
import { CodeEditor } from "./CodeEditor";
import s from "./FileDrawer.module.css";

type FileData = {
  path: string;
  content: string;
  original?: string;
  revision: string;
  editable: boolean;
};
type View = "code" | "preview";
type DiscardAction = "close" | "reload";

const mdComponents: Components = {
  table: ({ node: _, ...props }) => (
    <div className={s.tableWrap}>
      <table {...props} />
    </div>
  ),
};

export function FileDrawer({
  sid,
  path,
  fileRef,
  line,
  request,
  onClose,
  onSaved,
}: {
  sid: string;
  path: string;
  fileRef: string;
  line?: number;
  request: number;
  onClose: () => void;
  onSaved?: () => void;
}) {
  const [data, setData] = useState<FileData | null>(null);
  const [draft, setDraft] = useState("");
  const [err, setErr] = useState("");
  const [saveError, setSaveError] = useState("");
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);
  const [view, setView] = useState<View>("code");
  const [refresh, setRefresh] = useState(0);
  const [discard, setDiscard] = useState<DiscardAction | null>(null);
  const close = useRef<HTMLButtonElement>(null);
  const loaded = useRef("");
  const savedTimer = useRef(0);
  const dirty = !!data && draft !== data.content;

  useEffect(() => {
    if (!path) return;
    let current = true;
    const key = `${sid}:${fileRef}`;
    if (loaded.current !== key) {
      setData(null);
      setDraft("");
      setView("code");
    }
    setErr("");
    setSaveError("");
    setSaved(false);
    api
      .file(sid, fileRef)
      .then((file: FileData) => {
        if (!current) return;
        loaded.current = key;
        setData(file);
        setDraft(file.content);
      })
      .catch(() => current && setErr(`cannot read ${path}`));
    return () => {
      current = false;
    };
  }, [sid, path, fileRef, request, refresh]);

  useEffect(() => {
    if (!dirty) return;
    const preventClose = (event: BeforeUnloadEvent) => {
      event.preventDefault();
      event.returnValue = "";
    };
    window.addEventListener("beforeunload", preventClose);
    return () => window.removeEventListener("beforeunload", preventClose);
  }, [dirty]);

  useEffect(
    () => () => {
      window.clearTimeout(savedTimer.current);
    },
    [],
  );

  const requestClose = () => {
    if (dirty) setDiscard("close");
    else onClose();
  };

  const reload = () => {
    setData(null);
    setDraft("");
    setRefresh((value) => value + 1);
  };

  const confirmDiscard = () => {
    const action = discard;
    setDiscard(null);
    if (action === "close") {
      setData(null);
      setDraft("");
      onClose();
    } else if (action === "reload") reload();
  };

  const save = async () => {
    if (!data?.editable || !dirty || saving) return;
    setSaving(true);
    setSaved(false);
    setSaveError("");
    try {
      const result = (await api.saveFile(
        sid,
        fileRef,
        data.revision,
        draft,
      )) as { revision: string };
      setData({ ...data, content: draft, revision: result.revision });
      setSaved(true);
      window.clearTimeout(savedTimer.current);
      savedTimer.current = window.setTimeout(() => setSaved(false), 1600);
      onSaved?.();
    } catch (error) {
      setSaveError(
        error instanceof APIError && error.status === 409
          ? "file changed on disk"
          : "cannot save file",
      );
    } finally {
      setSaving(false);
    }
  };

  const displayPath = data?.path ?? path;
  const markdown = /\.md$/i.test(displayPath);
  const preview = markdown && view === "preview";
  const state = saveError
    ? saveError
    : saving
      ? "saving…"
      : dirty
        ? "unsaved"
        : saved
          ? "saved"
          : data && !data.editable
            ? "read only"
            : "";

  return (
    <>
      <Dialog.Root
        open={!!path}
        onOpenChange={(open) => !open && requestClose()}
      >
        <Dialog.Portal>
          <Dialog.Backdrop className={`${d.overlay} ${s.overlay}`} />
          <Dialog.Popup className={s.drawer} initialFocus={close}>
            <header className={s.head}>
              <Dialog.Title className={s.title} title={displayPath}>
                {displayPath}
              </Dialog.Title>
              {state && (
                <span
                  className={s.state}
                  data-error={!!saveError || undefined}
                  data-saved={(saved && !dirty) || undefined}
                >
                  {state}
                </span>
              )}
              {saveError === "file changed on disk" && (
                <Button
                  outline
                  small
                  onClick={() => (dirty ? setDiscard("reload") : reload())}
                >
                  <RefreshCw size={13} strokeWidth={1.8} aria-hidden />
                  reload
                </Button>
              )}
              {data?.editable && (
                <Button
                  outline
                  small
                  tone="accent"
                  disabled={!dirty || saving}
                  data-saving={saving || undefined}
                  onClick={() => void save()}
                >
                  {saving ? (
                    <LoaderCircle size={13} strokeWidth={1.8} aria-hidden />
                  ) : (
                    <Save size={13} strokeWidth={1.8} aria-hidden />
                  )}
                  save
                </Button>
              )}
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
                      {tab === "code" ? (
                        <Code2 size={13} strokeWidth={1.8} aria-hidden />
                      ) : (
                        <Eye size={13} strokeWidth={1.8} aria-hidden />
                      )}
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
              {!data && !err && (
                <div className={s.message}>
                  <LoaderCircle size={14} strokeWidth={1.8} aria-hidden />
                  loading
                </div>
              )}
              {err && <div className={`${s.message} ${s.error}`}>{err}</div>}
              {data && preview && !draft && (
                <div className={s.message} role="tabpanel">
                  (empty file)
                </div>
              )}
              {data && preview && draft && (
                <article className={s.markdown} role="tabpanel">
                  <ReactMarkdown
                    remarkPlugins={[remarkGfm]}
                    components={mdComponents}
                  >
                    {draft}
                  </ReactMarkdown>
                </article>
              )}
              {data && !preview && (
                <CodeEditor
                  path={data.path}
                  content={draft}
                  original={data.original}
                  line={line}
                  editable={data.editable}
                  onChange={setDraft}
                  onSave={() => void save()}
                  className={s.editor}
                />
              )}
            </div>
          </Dialog.Popup>
        </Dialog.Portal>
      </Dialog.Root>
      <AlertDialog.Root
        open={discard !== null}
        onOpenChange={(open) => !open && setDiscard(null)}
      >
        <AlertDialog.Portal>
          <AlertDialog.Backdrop
            className={`${d.overlay} ${s.confirmOverlay}`}
          />
          <AlertDialog.Popup className={`${d.popup} ${d.confirm} ${s.confirm}`}>
            <AlertDialog.Title className={d.head}>
              discard unsaved changes?
            </AlertDialog.Title>
            <AlertDialog.Description className={d.desc}>
              Your edits to “{path}” will be lost.
            </AlertDialog.Description>
            <div className={d.foot}>
              <AlertDialog.Close render={<Button outline />}>
                <X size={13} strokeWidth={1.8} aria-hidden />
                cancel
              </AlertDialog.Close>
              <Button outline tone="danger" onClick={confirmDiscard}>
                discard
              </Button>
            </div>
          </AlertDialog.Popup>
        </AlertDialog.Portal>
      </AlertDialog.Root>
    </>
  );
}
