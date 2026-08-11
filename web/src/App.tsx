import { useCallback, useEffect, useMemo, useState } from "react";
import { PanelLeft, ServerOff } from "lucide-react";
import { Group, Panel, Separator } from "react-resizable-panels";
import {
  Outlet,
  useLoaderData,
  useNavigate,
  useParams,
  useRevalidator,
} from "react-router";
import { api } from "./lib/api";
import { useActivityFavicon } from "./lib/favicon";
import { useAnyBusy, useNarrowSidebar, useOpenTabs } from "./lib/hooks";
import { revalidateSoon } from "./lib/revalidate";
import type { RootData } from "./lib/rootLoader";
import { sessionTitle } from "./lib/session";
import * as store from "./lib/store";
import type { SessionRow } from "./lib/types";
import { Sidebar } from "./sidebar/Sidebar";
import { DirPicker } from "./sidebar/DirPicker";
import { DeleteDialog } from "./sidebar/DeleteDialog";
import { TerminalPanel } from "./session/TerminalPanel";
import type { SessionOutletContext } from "./session/SessionView";
import { Button } from "./ui/Button";
import { Login } from "./auth/Login";
import s from "./App.module.css";

// App is the layout route: sidebar plus the routed session (Outlet). The
// active session is /s/:sid; every open tab streams via the store.
export default function App() {
  const {
    authenticated,
    dead,
    rows,
    cwd: serverCwd,
    home,
    models,
  } = useLoaderData<RootData>();
  const { sid = "" } = useParams();
  const navigate = useNavigate();
  const { revalidate } = useRevalidator();
  const [open, setOpen] = useOpenTabs();
  const [picking, setPicking] = useState(false);
  const [doomed, setDoomed] = useState<SessionRow | null>(null);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [terminalOpen, setTerminalOpen] = useState(false);
  const { narrow, sideOpen, setSideOpen } = useNarrowSidebar();
  const selected = rows.find((r) => sid === r.id || sid === r.rid) ?? null;
  const pageTitle = selected ? sessionTitle(selected) : "orc";
  const anyBusy = useAnyBusy(open);

  const go = useCallback(
    (path: string, replace = false) => navigate(path, { replace }),
    [navigate],
  );
  const openTerminal = useCallback(() => setTerminalOpen(true), []);
  const toggleTerminal = useCallback(
    () => setTerminalOpen((open) => !open),
    [],
  );
  const outletContext = useMemo<SessionOutletContext>(
    () => ({ models, openTerminal, toggleTerminal }),
    [models, openTerminal, toggleTerminal],
  );

  // keep the sidebar fresh
  useEffect(() => {
    const iv = setInterval(revalidate, 5000);
    return () => clearInterval(iv);
  }, [revalidate]);

  // the routed session is an open tab
  useEffect(() => {
    if (sid) setOpen((o) => (o.includes(sid) ? o : [...o, sid]));
    if (narrow) setSideOpen(false);
  }, [sid, narrow, setOpen, setSideOpen]);

  useEffect(() => {
    document.title = pageTitle;
  }, [pageTitle]);

  useActivityFavicon(anyBusy);

  // every open tab streams, mounted or not; the view only subscribes
  useEffect(() => {
    if (!authenticated) return;
    for (const s of open)
      store.ensure(s, revalidateSoon).then(() => {
        // /open can resolve s to another chain id; keep one tab per session.
        // The active sid is left for SessionRoute to redirect first.
        const c = store.snapshot(s).canonical;
        if (!c || c === s || s === sid) return;
        store.drop(s);
        setOpen((o) =>
          o.includes(c)
            ? o.filter((x) => x !== s)
            : o.map((x) => (x === s ? c : x)),
        );
      });
  }, [authenticated, open, sid, setOpen]);

  const closeTab = (row: SessionRow) => {
    store.drop(row.id);
    if (row.rid) store.drop(row.rid);
    const next = open.filter((x) => x !== row.id && x !== row.rid);
    setOpen(next);
    if (sid === row.id || sid === row.rid) {
      setTerminalOpen(false);
      go(next[0] ? `/s/${next[0]}` : "/");
    }
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

  const onWake = (row: SessionRow) => {
    api.wake(row.id).finally(revalidate);
  };

  const onNew = (cwd: string, routine: string) => {
    setPicking(false);
    api
      .create(cwd, routine)
      .then((d) => go(`/s/${d.id}`))
      .catch(() => {});
  };

  if (!authenticated) return <Login onLogin={revalidate} />;

  if (dead)
    return (
      <div className={s.dead}>
        <ServerOff size={17} strokeWidth={1.8} aria-hidden />
        cannot reach orc — is it still running?
      </div>
    );

  const sidebar = (
    <Sidebar
      rows={rows}
      serverCwd={serverCwd}
      home={home}
      models={models}
      active={sid}
      openIds={open}
      sheet={narrow}
      open={sideOpen}
      onDismiss={() => setSideOpen(false)}
      onStop={onStop}
      onDelete={confirmDelete}
      onPin={onPin}
      onWake={onWake}
      onNew={() => {
        setSideOpen(false);
        setPicking(true);
      }}
    />
  );
  const chat = <Outlet context={outletContext} />;
  const terminal = sid && terminalOpen && (
    <TerminalPanel sid={sid} open onClose={() => setTerminalOpen(false)} />
  );

  return (
    <div className={s.shell}>
      <Button
        icon
        outline
        tip={sideOpen ? "hide sessions" : "show sessions"}
        className={s.menu}
        aria-controls="session-sidebar"
        aria-expanded={sideOpen}
        onClick={() => setSideOpen((open) => !open)}
      >
        <PanelLeft size={16} strokeWidth={1.8} aria-hidden />
      </Button>
      {narrow ? (
        <>
          {sidebar}
          <Group orientation="vertical" className={s.group}>
            <Panel id="chat" minSize="10rem" className={s.pane}>
              {chat}
            </Panel>
            {terminal && (
              <>
                <Separator className={s.separator} />
                <Panel
                  id="terminal"
                  defaultSize="42%"
                  minSize="10rem"
                  className={s.pane}
                >
                  {terminal}
                </Panel>
              </>
            )}
          </Group>
        </>
      ) : (
        <Group orientation="horizontal" className={s.group}>
          <Panel
            id="sessions"
            defaultSize="18rem"
            minSize="14rem"
            maxSize="28rem"
            groupResizeBehavior="preserve-pixel-size"
            className={s.pane}
          >
            {sidebar}
          </Panel>
          <Separator className={s.separator} />
          <Panel id="chat" minSize="16rem" className={s.pane}>
            {chat}
          </Panel>
          {terminal && (
            <>
              <Separator className={s.separator} />
              <Panel
                id="terminal"
                defaultSize="36%"
                minSize="15rem"
                maxSize="60%"
                className={s.pane}
              >
                {terminal}
              </Panel>
            </>
          )}
        </Group>
      )}
      <DirPicker
        open={picking}
        start={serverCwd}
        onPick={onNew}
        onCancel={() => setPicking(false)}
      />
      <DeleteDialog
        open={deleteOpen}
        row={doomed}
        onOpenChange={setDeleteOpen}
        onClosed={() => setDoomed(null)}
        onDelete={doDelete}
      />
    </div>
  );
}
