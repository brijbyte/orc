import { useCallback, useSyncExternalStore } from "react";
import { Link } from "react-router";
import { Pin, Trash2, X } from "lucide-react";
import * as store from "../lib/store";
import type { SessionRow } from "../lib/types";
import { Button } from "../ui/Button";
import s from "./Sidebar.module.css";

// prettyDir abbreviates a home-prefixed path and keeps it short from the left.
function prettyDir(cwd: string, home: string): string {
  if (home && cwd.startsWith(home)) cwd = "~" + cwd.slice(home.length);
  return cwd;
}

// tailDir keeps the last two path segments, which fit on a row line.
function tailDir(cwd: string): string {
  const parts = cwd.split("/").filter(Boolean);
  return parts.slice(-2).join("/");
}

// ago renders a session timestamp ("2026-08-09 18:19:50.000Z") as an age.
function ago(ts: string): string {
  const t = Date.parse(ts.replace(" ", "T"));
  if (!t) return "";
  const min = (Date.now() - t) / 60000;
  if (min < 1) return "now";
  if (min < 60) return `${Math.floor(min)}m ago`;
  if (min < 1440) return `${Math.floor(min / 60)}h ago`;
  if (min < 10080) return `${Math.floor(min / 1440)}d ago`;
  return new Date(t).toLocaleDateString();
}

function Row({
  row,
  active,
  open,
  showDir,
  onStop,
  onDelete,
  onPin,
  onOpen,
}: {
  row: SessionRow;
  active: boolean;
  open: boolean;
  showDir: boolean;
  onStop: () => void;
  onDelete: () => void;
  onPin: () => void;
  onOpen: () => void;
}) {
  const sid = row.rid ?? row.id;
  // open tabs stream: their busy state is live, not 5s-poll delayed
  const streamed = useSyncExternalStore(
    useCallback((fn: () => void) => store.subscribe(sid, fn), [sid]),
    useCallback(() => store.snapshot(sid), [sid]),
  );
  const busy = streamed.busy || row.busy;
  return (
    <div className={s.row + (active ? " " + s.active : "")}>
      <Link
        className={s.open}
        to={`/s/${sid}`}
        title={`${row.id.slice(0, 8)} · started ${row.when}`}
        onClick={onOpen}
      >
        <span className={s.dot}>
          {busy ? <span className={s.busydot}>●</span> : row.live ? "●" : "○"}
        </span>
        <span className={s.title}>{row.title || row.id.slice(0, 8)}</span>
      </Link>
      <div className={s.foot}>
        <span className={s.age} title={showDir ? row.cwd : undefined}>
          {ago(row.used)}
          {showDir ? ` · ${tailDir(row.cwd)}` : ""}
        </span>
        <span className={s.acts}>
          <Button
            icon
            tone={row.pinned ? "accent" : undefined}
            tip={row.pinned ? "unpin" : "pin to the top"}
            className={s.act + (row.pinned ? " " + s.on : "")}
            onClick={onPin}
          >
            <Pin size={12} />
          </Button>
          {open && row.live && (
            <Button
              icon
              tip="close (stop) this session"
              className={s.act}
              onClick={onStop}
            >
              <X size={12} />
            </Button>
          )}
          <Button
            icon
            tone="danger"
            tip="delete this session"
            className={s.act}
            onClick={onDelete}
          >
            <Trash2 size={12} />
          </Button>
        </span>
      </div>
    </div>
  );
}

// Sidebar lists pinned sessions first, then the rest grouped by directory.
// Rows arrive most-recently-used first, so the groups and the rows inside them
// both stay in that order.
export function Sidebar({
  rows,
  serverCwd,
  home,
  active,
  openIds,
  sheet,
  open,
  onDismiss,
  onStop,
  onDelete,
  onPin,
  onNew,
}: {
  rows: SessionRow[];
  serverCwd: string;
  home: string;
  active: string;
  openIds: string[];
  sheet: boolean;
  open: boolean;
  onDismiss: () => void;
  onStop: (row: SessionRow) => void;
  onDelete: (row: SessionRow) => void;
  onPin: (row: SessionRow) => void;
  onNew: () => void;
}) {
  const pinned = rows.filter((r) => r.pinned);
  const groups = new Map<string, SessionRow[]>();
  for (const r of rows) {
    if (r.pinned) continue;
    const g = groups.get(r.cwd);
    if (g) g.push(r);
    else groups.set(r.cwd, [r]);
  }
  if (serverCwd && !groups.size && !pinned.length) groups.set(serverCwd, []);
  const isOpen = (r: SessionRow) =>
    openIds.includes(r.id) || (!!r.rid && openIds.includes(r.rid));
  const isActive = (r: SessionRow) => active === r.id || active === r.rid;

  const group = (label: string, list: SessionRow[], showDir = false) => (
    <div className={s.group} key={label}>
      {/* bdi isolates the LTR path from the rtl left-ellipsis trick */}
      <div className={s.head} title={label}>
        <bdi>{showDir ? label : prettyDir(label, home)}</bdi>
      </div>
      {list.map((r) => (
        <Row
          key={r.id}
          row={r}
          active={isActive(r)}
          open={isOpen(r)}
          showDir={showDir}
          onStop={() => onStop(r)}
          onDelete={() => onDelete(r)}
          onPin={() => onPin(r)}
          onOpen={onDismiss}
        />
      ))}
    </div>
  );

  return (
    <>
      <div
        className={s.backdrop}
        data-open={(sheet && open) || undefined}
        onClick={onDismiss}
        aria-hidden
      />
      <nav
        id="session-sidebar"
        aria-label="sessions"
        className={`${s.side}${sheet ? ` ${s.sheet}` : ""}`}
        data-open={!sheet || open || undefined}
        inert={sheet && !open}
      >
        <Button
          icon
          tip="close sidebar"
          className={s.dismiss}
          onClick={onDismiss}
        >
          <X size={16} strokeWidth={1.8} aria-hidden />
        </Button>
        <Button outline tone="success" className={s.new} onClick={onNew}>
          + new session
        </Button>
        {!!pinned.length && group("📌 pinned", pinned, true)}
        {[...groups.entries()].map(([cwd, list]) => group(cwd, list))}
      </nav>
    </>
  );
}
