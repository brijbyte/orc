import { useEffect, useRef, useState } from "react";
import { AlertDialog } from "@base-ui/react/alert-dialog";
import { Dialog } from "@base-ui/react/dialog";
import { Tabs } from "@base-ui/react/tabs";
import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import { Code2, Eye, LoaderCircle, RefreshCw, Save, X } from "lucide-react";
import { api, APIError } from "../lib/api";
import { Button } from "../ui/Button";
import d from "../ui/dialog.module.css";
import { CodeEditor } from "../component/editor/CodeEditor";
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
  const conflict = saveError === "file changed on disk";
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

  const editor = data && (
    <CodeEditor
      path={data.path}
      content={draft}
      original={data.original}
      line={line}
      editable={data.editable}
      autoFocus={data.editable}
      onChange={setDraft}
      onSave={() => void save()}
    />
  );

  return (
    <>
      <Dialog.Root
        open={!!path}
        onOpenChange={(open) => !open && requestClose()}
      >
        <Dialog.Portal>
          <Dialog.Backdrop className={`${d.overlay} ${s.overlay}`} />
          <Dialog.Popup className={s.drawer} initialFocus={close}>
            <Tabs.Root
              value={view}
              onValueChange={(value) => setView(value as View)}
              className={s.tabsRoot}
            >
              <header className={s.head}>
                <Dialog.Title className={s.title} title={displayPath}>
                  {displayPath}
                </Dialog.Title>
                <div className={s.viewGroup}>
                  {markdown && (
                    <Tabs.List
                      className={s.tabs}
                      aria-label="file view"
                      activateOnFocus
                    >
                      <Tabs.Tab
                        value="code"
                        aria-label="Code"
                        render={<Button tab small />}
                      >
                        <Code2 size={13} strokeWidth={1.8} aria-hidden />
                        <span className={s.buttonLabel}>Code</span>
                      </Tabs.Tab>
                      <Tabs.Tab
                        value="preview"
                        aria-label="Preview"
                        render={<Button tab small />}
                      >
                        <Eye size={13} strokeWidth={1.8} aria-hidden />
                        <span className={s.buttonLabel}>Preview</span>
                      </Tabs.Tab>
                    </Tabs.List>
                  )}
                </div>
                <div className={s.actions}>
                  <span
                    className={s.state}
                    data-error={!!saveError || undefined}
                    data-saved={(saved && !dirty) || undefined}
                    role="status"
                    aria-live="polite"
                    title={state}
                  >
                    {state}
                  </span>
                  {data?.editable && (
                    <>
                      <span className={s.reloadSlot}>
                        {conflict && (
                          <Button
                            outline
                            small
                            aria-label="reload"
                            onClick={() =>
                              dirty ? setDiscard("reload") : reload()
                            }
                          >
                            <RefreshCw
                              size={13}
                              strokeWidth={1.8}
                              aria-hidden
                            />
                            <span className={s.actionLabel}>reload</span>
                          </Button>
                        )}
                      </span>
                      <span className={s.saveSlot}>
                        <Button
                          primary
                          small
                          tone="accent"
                          aria-label="save"
                          disabled={!dirty || saving}
                          data-saving={saving || undefined}
                          onClick={() => void save()}
                        >
                          {saving ? (
                            <LoaderCircle
                              size={13}
                              strokeWidth={1.8}
                              aria-hidden
                            />
                          ) : (
                            <Save size={13} strokeWidth={1.8} aria-hidden />
                          )}
                          <span className={s.actionLabel}>save</span>
                        </Button>
                      </span>
                    </>
                  )}
                </div>
                <Dialog.Close
                  ref={close}
                  render={<Button icon className={s.close} />}
                  aria-label="close file"
                >
                  <X size={17} strokeWidth={1.8} aria-hidden />
                </Dialog.Close>
              </header>
              <div
                className={s.body}
                data-preview={(markdown && view === "preview") || undefined}
              >
                {!data && !err && (
                  <div className={s.message}>
                    <LoaderCircle size={14} strokeWidth={1.8} aria-hidden />
                    loading
                  </div>
                )}
                {err && <div className={`${s.message} ${s.error}`}>{err}</div>}
                {data && markdown && (
                  <>
                    <Tabs.Panel value="code" className={s.editor}>
                      {editor}
                    </Tabs.Panel>
                    <Tabs.Panel value="preview" className={s.previewPanel}>
                      {!draft ? (
                        <div className={s.message}>(empty file)</div>
                      ) : (
                        <article className={s.markdown}>
                          <ReactMarkdown
                            remarkPlugins={[remarkGfm]}
                            components={mdComponents}
                          >
                            {draft}
                          </ReactMarkdown>
                        </article>
                      )}
                    </Tabs.Panel>
                  </>
                )}
                {data && !markdown && <div className={s.editor}>{editor}</div>}
              </div>
            </Tabs.Root>
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
              Discard unsaved changes?
            </AlertDialog.Title>
            <AlertDialog.Description className={d.desc}>
              Your edits to “<span title={path}>{path}</span>” will be lost.
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
