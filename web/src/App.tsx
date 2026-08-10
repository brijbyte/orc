import { useCallback, useEffect, useState, useSyncExternalStore } from "react";
import { PanelLeft, ServerOff, Trash2, X } from "lucide-react";
import {
  Outlet,
  useLoaderData,
  useNavigate,
  useParams,
  useRevalidator,
} from "react-router";
import { AlertDialog } from "@base-ui/react/alert-dialog";
import { api, APIError } from "./lib/api";
import { revalidateSoon } from "./lib/revalidate";
import * as store from "./lib/store";
import type { Model, SessionRow } from "./lib/types";
import { Sidebar } from "./sidebar/Sidebar";
import { DirPicker } from "./sidebar/DirPicker";
import { Button } from "./ui/Button";
import { Login } from "./auth/Login";
import s from "./App.module.css";
import d from "./ui/dialog.module.css";

export type RootData = {
  authenticated: boolean;
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
// A dead server is data, so the poll can keep retrying.
export async function rootLoader(): Promise<RootData> {
  try {
    const s = await api.sessions();
    const m = await loadModels();
    return {
      authenticated: true,
      dead: false,
      rows: s.sessions ?? [],
      cwd: s.cwd ?? "",
      home: s.home ?? "",
      models: m.models ?? [],
    };
  } catch (error) {
    return {
      authenticated: !(error instanceof APIError && error.status === 401),
      dead: !(error instanceof APIError && error.status === 401),
      rows: [],
      cwd: "",
      home: "",
      models: [],
    };
  }
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

function sessionTitle(row: SessionRow | null): string {
  const title = (row?.title || row?.id.slice(0, 8) || "session")
    .replace(/\s+/g, " ")
    .trim();
  return title.length > 64 ? title.slice(0, 63) + "…" : title;
}

const faviconPath = "/favicon.svg";
let faviconFrames: Promise<string[]> | null = null;

function activityFavicons(): Promise<string[]> {
  return (faviconFrames ??= fetch(faviconPath)
    .then((response) => response.text())
    .then((svg) =>
      [-16, -8, 0, 8, 16, 8, 0, -8].map((angle) => {
        const frame = svg
          .replace(/(<svg[^>]*>)/, `$1<g transform="rotate(${angle} 32 32)">`)
          .replace("</svg>", "</g></svg>");
        return `data:image/svg+xml,${encodeURIComponent(frame)}`;
      }),
    ));
}

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
  const [open, setOpen] = useState<string[]>(loadOpen);
  const [picking, setPicking] = useState(false);
  const [doomed, setDoomed] = useState<SessionRow | null>(null);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [narrow, setNarrow] = useState(
    () => matchMedia("(max-width: 48rem)").matches,
  );
  const [sideOpen, setSideOpen] = useState(
    () => !matchMedia("(max-width: 48rem)").matches,
  );
  const selected = rows.find((r) => sid === r.id || sid === r.rid) ?? null;
  const pageTitle = selected ? sessionTitle(selected) : "orc";
  const anyBusy = useSyncExternalStore(
    useCallback(
      (notify) => {
        const stops = open.map((id) => store.subscribe(id, notify));
        return () => stops.forEach((stop) => stop());
      },
      [open],
    ),
    useCallback(() => open.some((id) => store.snapshot(id).busy), [open]),
  );

  const go = useCallback(
    (path: string, replace = false) => navigate(path, { replace }),
    [navigate],
  );

  // keep the sidebar fresh
  useEffect(() => {
    const iv = setInterval(revalidate, 5000);
    return () => clearInterval(iv);
  }, [revalidate]);

  useEffect(() => {
    const media = matchMedia("(max-width: 48rem)");
    const resize = () => {
      setNarrow(media.matches);
      setSideOpen(!media.matches);
    };
    media.addEventListener("change", resize);
    return () => media.removeEventListener("change", resize);
  }, []);

  // the routed session is an open tab
  useEffect(() => {
    if (sid) setOpen((o) => (o.includes(sid) ? o : [...o, sid]));
    if (narrow) setSideOpen(false);
  }, [sid, narrow]);

  useEffect(() => {
    sessionStorage.setItem("orc-tabs", JSON.stringify({ open }));
  }, [open]);

  useEffect(() => {
    document.title = pageTitle;
  }, [pageTitle]);

  useEffect(() => {
    const icon = document.querySelector<HTMLLinkElement>('link[rel="icon"]');
    if (!icon || !anyBusy) {
      if (icon) icon.href = faviconPath;
      return;
    }
    const media = matchMedia("(prefers-reduced-motion: reduce)");
    let frames: string[] = [];
    let frame = 0;
    let timer = 0;
    let stopped = false;
    const start = () => {
      clearInterval(timer);
      if (!frames.length) return;
      if (media.matches) {
        icon.href = frames[4];
        return;
      }
      const show = () => {
        icon.href = frames[frame++ % frames.length];
      };
      show();
      timer = window.setInterval(show, 160);
    };
    media.addEventListener("change", start);
    activityFavicons().then((loaded) => {
      if (stopped) return;
      frames = loaded;
      start();
    });
    return () => {
      stopped = true;
      clearInterval(timer);
      media.removeEventListener("change", start);
      icon.href = faviconPath;
    };
  }, [anyBusy]);

  // every open tab streams, mounted or not; the view only subscribes
  useEffect(() => {
    if (authenticated) for (const s of open) store.ensure(s, revalidateSoon);
  }, [authenticated, open]);

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

  if (!authenticated) return <Login onLogin={revalidate} />;

  if (dead)
    return (
      <div className={s.dead}>
        <ServerOff size={17} strokeWidth={1.8} aria-hidden />
        cannot reach orc — is it still running?
      </div>
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
      <Sidebar
        rows={rows}
        serverCwd={serverCwd}
        home={home}
        active={sid}
        openIds={open}
        sheet={narrow}
        open={sideOpen}
        onDismiss={() => setSideOpen(false)}
        onStop={onStop}
        onDelete={confirmDelete}
        onPin={onPin}
        onNew={() => {
          setSideOpen(false);
          setPicking(true);
        }}
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
              “{sessionTitle(doomed)}” and its file will be removed.
            </AlertDialog.Description>
            <div className={d.foot}>
              <AlertDialog.Close render={<Button outline />}>
                <X size={13} strokeWidth={1.8} aria-hidden />
                cancel
              </AlertDialog.Close>
              <Button
                outline
                tone="danger"
                disabled={!doomed}
                onClick={() => doomed && doDelete(doomed)}
              >
                <Trash2 size={13} strokeWidth={1.8} aria-hidden />
                delete
              </Button>
            </div>
          </AlertDialog.Popup>
        </AlertDialog.Portal>
      </AlertDialog.Root>
    </div>
  );
}
