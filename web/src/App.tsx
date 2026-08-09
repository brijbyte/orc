import { useCallback, useEffect, useState } from "react";
import { AlertDialog } from "@base-ui/react/alert-dialog";
import { api, hashSession, setHashSession } from "./api";
import * as store from "./store";
import type { Model, SessionRow } from "./types";
import { SessionView } from "./SessionView";
import { Sidebar } from "./Sidebar";
import { DirPicker } from "./DirPicker";

// Open tabs survive a reload in this browser tab.
function loadTabs(): { open: string[]; active: string } {
  try {
    const t = JSON.parse(sessionStorage.getItem("orc-tabs") ?? "");
    if (Array.isArray(t.open)) return { open: t.open, active: t.active ?? "" };
  } catch {
    /* first visit */
  }
  return { open: [], active: "" };
}

// App manages the session list, the set of open (mounted) sessions, and the
// directory picker for new sessions. Each open session keeps its own SSE
// stream; the sidebar switches which one is visible.
export default function App() {
  const [rows, setRows] = useState<SessionRow[]>([]);
  const [serverCwd, setServerCwd] = useState("");
  const [home, setHome] = useState("");
  const [dead, setDead] = useState(false);
  const [models, setModels] = useState<Model[]>([]);
  const [tabs, setTabs] = useState(() => {
    const t = loadTabs();
    const h = hashSession();
    if (h && !t.open.includes(h)) t.open = [...t.open, h];
    if (h) t.active = h;
    return t;
  });
  const [picking, setPicking] = useState(false);
  const [doomed, setDoomed] = useState<SessionRow | null>(null);

  const refresh = useCallback(() => {
    api
      .sessions()
      .then((d) => {
        setRows(d.sessions ?? []);
        setServerCwd(d.cwd ?? "");
        setHome(d.home ?? "");
        setDead(false);
      })
      .catch(() => setDead(true));
  }, []);

  useEffect(() => {
    refresh();
    const iv = setInterval(refresh, 5000);
    return () => clearInterval(iv);
  }, [refresh]);

  useEffect(() => {
    api
      .models()
      .then((d) => setModels(d.models ?? []))
      .catch(() => {});
  }, []);

  useEffect(() => {
    sessionStorage.setItem("orc-tabs", JSON.stringify(tabs));
    setHashSession(tabs.active);
  }, [tabs]);

  // every open tab streams, mounted or not; the view only subscribes
  useEffect(() => {
    for (const sid of tabs.open) store.ensure(sid, refresh);
  }, [tabs.open, refresh]);

  const activate = useCallback((id: string) => {
    setTabs((t) => ({
      open: t.open.includes(id) ? t.open : [...t.open, id],
      active: id,
    }));
  }, []);

  const onOpen = (row: SessionRow) => {
    // A live runtime may already hold this session under its own handle.
    activate(row.rid ?? row.id);
  };

  const closeTab = (row: SessionRow) => {
    store.drop(row.id);
    if (row.rid) store.drop(row.rid);
    setTabs((t) => {
      const open = t.open.filter((x) => x !== row.id && x !== row.rid);
      const gone = t.active === row.id || t.active === row.rid;
      return { open, active: gone ? (open[0] ?? "") : t.active };
    });
  };

  const onStop = (row: SessionRow) => {
    api.stop(row.rid ?? row.id).finally(refresh);
    closeTab(row);
  };

  const doDelete = (row: SessionRow) => {
    setDoomed(null);
    api.remove(row.id).finally(refresh);
    closeTab(row);
  };

  const onNew = (cwd: string) => {
    setPicking(false);
    api
      .create(cwd)
      .then((d) => {
        activate(d.id);
        refresh();
      })
      .catch(() => {});
  };

  if (dead)
    return (
      <div className="dead">
        🧌 cannot reach orc — is it still running? (check the URL token)
      </div>
    );

  return (
    <div className="shell">
      <Sidebar
        rows={rows}
        serverCwd={serverCwd}
        home={home}
        active={tabs.active}
        openIds={tabs.open}
        onOpen={onOpen}
        onStop={onStop}
        onDelete={setDoomed}
        onNew={() => setPicking(true)}
      />
      {tabs.active ? (
        <SessionView key={tabs.active} sid={tabs.active} models={models} />
      ) : (
        <div className="empty">🧌 pick a session on the left, or start one</div>
      )}
      {picking && (
        <DirPicker
          start={serverCwd}
          onPick={onNew}
          onCancel={() => setPicking(false)}
        />
      )}
      {doomed && (
        <AlertDialog.Root open onOpenChange={(o) => !o && setDoomed(null)}>
          <AlertDialog.Portal>
            <AlertDialog.Backdrop className="overlay" />
            <AlertDialog.Popup className="picker confirm">
              <AlertDialog.Title className="phead">
                delete session?
              </AlertDialog.Title>
              <AlertDialog.Description className="pdesc">
                “{doomed.title || doomed.id.slice(0, 8)}” and its file will be
                removed.
              </AlertDialog.Description>
              <div className="pfoot">
                <AlertDialog.Close>cancel</AlertDialog.Close>
                <button className="pdanger" onClick={() => doDelete(doomed)}>
                  delete
                </button>
              </div>
            </AlertDialog.Popup>
          </AlertDialog.Portal>
        </AlertDialog.Root>
      )}
    </div>
  );
}
