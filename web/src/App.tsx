import { useCallback, useEffect, useState } from "react";
import {
  Outlet,
  useLoaderData,
  useNavigate,
  useParams,
  useRevalidator,
} from "react-router";
import { AlertDialog } from "@base-ui/react/alert-dialog";
import { api, legacySession, tokenHash } from "./api";
import { revalidateSoon } from "./revalidate";
import * as store from "./store";
import type { Model, SessionRow } from "./types";
import { Sidebar } from "./Sidebar";
import { DirPicker } from "./DirPicker";
import s from "./App.module.css";
import d from "./dialog.module.css";

export type RootData = {
  dead: boolean;
  rows: SessionRow[];
  cwd: string;
  home: string;
  models: Model[];
};

// models are static per server run: fetch once, not on every revalidation
let modelsOnce: Promise<{ models: Model[] }> | null = null;
const loadModels = () =>
  (modelsOnce ??= api.models().catch(() => {
    modelsOnce = null; // retry on the next revalidation
    return { models: [] };
  }));

// rootLoader fetches the session list and models before the shell renders.
// A dead server is data, not an error, so the poll can keep retrying.
export async function rootLoader(): Promise<RootData> {
  const [s, m] = await Promise.all([
    api.sessions().catch(() => null),
    loadModels(),
  ]);
  return {
    dead: !s,
    rows: s?.sessions ?? [],
    cwd: s?.cwd ?? "",
    home: s?.home ?? "",
    models: m.models ?? [],
  };
}

// Open tabs survive a reload in this browser tab.
function loadOpen(): string[] {
  try {
    const t = JSON.parse(sessionStorage.getItem("orc-tabs") ?? "");
    if (Array.isArray(t.open)) return t.open;
  } catch {
    /* first visit */
  }
  return [];
}

// App is the layout route: sidebar plus the routed session (Outlet). The
// active session is /s/:sid; every open tab streams via the store.
export default function App() {
  const { dead, rows, cwd: serverCwd, home, models } = useLoaderData<RootData>();
  const { sid = "" } = useParams();
  const navigate = useNavigate();
  const { revalidate } = useRevalidator();
  const [open, setOpen] = useState<string[]>(loadOpen);
  const [picking, setPicking] = useState(false);
  const [doomed, setDoomed] = useState<SessionRow | null>(null);
  const [deleteOpen, setDeleteOpen] = useState(false);

  // navigations keep the #token fragment
  const go = useCallback(
    (path: string, replace = false) =>
      navigate(path + tokenHash(), { replace }),
    [navigate],
  );

  // migrate legacy "#token/session" links onto the /s/:sid route
  useEffect(() => {
    const legacy = legacySession();
    if (legacy) go(`/s/${legacy}`, true);
  }, [go]);

  // keep the sidebar fresh
  useEffect(() => {
    const iv = setInterval(revalidate, 5000);
    return () => clearInterval(iv);
  }, [revalidate]);

  // the routed session is an open tab
  useEffect(() => {
    if (sid) setOpen((o) => (o.includes(sid) ? o : [...o, sid]));
  }, [sid]);

  useEffect(() => {
    sessionStorage.setItem("orc-tabs", JSON.stringify({ open }));
  }, [open]);

  // every open tab streams, mounted or not; the view only subscribes
  useEffect(() => {
    for (const s of open) store.ensure(s, revalidateSoon);
  }, [open]);

  const closeTab = (row: SessionRow) => {
    store.drop(row.id);
    if (row.rid) store.drop(row.rid);
    const next = open.filter((x) => x !== row.id && x !== row.rid);
    setOpen(next);
    if (sid === row.id || sid === row.rid) go(next[0] ? `/s/${next[0]}` : "/");
  };

  const onStop = (row: SessionRow) => {
    api.stop(row.rid ?? row.id).finally(revalidate);
    closeTab(row);
  };

  const doDelete = (row: SessionRow) => {
    setDeleteOpen(false);
    api.remove(row.id).finally(revalidate);
    closeTab(row);
  };

  const confirmDelete = (row: SessionRow) => {
    setDoomed(row);
    setDeleteOpen(true);
  };

  const onPin = (row: SessionRow) => {
    api.pin(row.id, !row.pinned).finally(revalidate);
  };

  const onNew = (cwd: string) => {
    setPicking(false);
    api
      .create(cwd)
      .then((d) => go(`/s/${d.id}`))
      .catch(() => {});
  };

  if (dead)
    return (
      <div className={s.dead}>
        🧌 cannot reach orc — is it still running? (check the URL token)
      </div>
    );

  return (
    <div className={s.shell}>
      <Sidebar
        rows={rows}
        serverCwd={serverCwd}
        home={home}
        active={sid}
        openIds={open}
        onStop={onStop}
        onDelete={confirmDelete}
        onPin={onPin}
        onNew={() => setPicking(true)}
      />
      <Outlet context={models} />
      <DirPicker
        open={picking}
        start={serverCwd}
        onPick={onNew}
        onCancel={() => setPicking(false)}
      />
      <AlertDialog.Root
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        onOpenChangeComplete={(isOpen) => !isOpen && setDoomed(null)}
      >
        <AlertDialog.Portal>
          <AlertDialog.Backdrop className={d.overlay} />
          <AlertDialog.Popup className={`${d.popup} ${d.confirm}`}>
            <AlertDialog.Title className={d.head}>
              delete session?
            </AlertDialog.Title>
            <AlertDialog.Description className={d.desc}>
              “{doomed?.title || doomed?.id.slice(0, 8) || "session"}” and its
              file will be removed.
            </AlertDialog.Description>
            <div className={d.foot}>
              <AlertDialog.Close>cancel</AlertDialog.Close>
              <button
                className={d.danger}
                disabled={!doomed}
                onClick={() => doomed && doDelete(doomed)}
              >
                delete
              </button>
            </div>
          </AlertDialog.Popup>
        </AlertDialog.Portal>
      </AlertDialog.Root>
    </div>
  );
}
