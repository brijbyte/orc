import type { SessionRow } from "./types";
import { TipBtn } from "./ui";

// prettyDir abbreviates a home-prefixed path and keeps it short from the left.
function prettyDir(cwd: string, home: string): string {
  if (home && cwd.startsWith(home)) cwd = "~" + cwd.slice(home.length);
  return cwd;
}

function Row({
  row,
  active,
  open,
  onOpen,
  onStop,
  onDelete,
}: {
  row: SessionRow;
  active: boolean;
  open: boolean;
  onOpen: () => void;
  onStop: () => void;
  onDelete: () => void;
}) {
  return (
    <div className={"srow" + (active ? " active" : "")}>
      <button
        className="sopen"
        onClick={onOpen}
        title={`${row.id.slice(0, 8)} · ${row.when}`}
      >
        <span className="sdot">
          {row.busy ? <span className="busydot">●</span> : row.live ? "●" : "○"}
        </span>
        <span className="stitle">{row.title || row.id.slice(0, 8)}</span>
      </button>
      {open && row.live && (
        <TipBtn tip="close (stop) this session" className="sstop" onClick={onStop}>
          ✕
        </TipBtn>
      )}
      <TipBtn tip="delete this session" className="sdel" onClick={onDelete}>
        🗑
      </TipBtn>
    </div>
  );
}

// Sidebar lists sessions: current directory first, then the rest grouped by
// directory, each group headed by its path.
export function Sidebar({
  rows,
  serverCwd,
  home,
  active,
  openIds,
  onOpen,
  onStop,
  onDelete,
  onNew,
}: {
  rows: SessionRow[];
  serverCwd: string;
  home: string;
  active: string;
  openIds: string[];
  onOpen: (row: SessionRow) => void;
  onStop: (row: SessionRow) => void;
  onDelete: (row: SessionRow) => void;
  onNew: () => void;
}) {
  const here = rows.filter((r) => r.cwd === serverCwd);
  const rest = rows.filter((r) => r.cwd !== serverCwd);
  const groups = new Map<string, SessionRow[]>();
  for (const r of rest) {
    const g = groups.get(r.cwd);
    if (g) g.push(r);
    else groups.set(r.cwd, [r]);
  }
  const isOpen = (r: SessionRow) =>
    openIds.includes(r.id) || (!!r.rid && openIds.includes(r.rid));
  const isActive = (r: SessionRow) => active === r.id || active === r.rid;

  const group = (label: string, list: SessionRow[]) => (
    <div className="sgroup" key={label}>
      {/* bdi isolates the LTR path from the rtl left-ellipsis trick */}
      <div className="ghead" title={label}>
        <bdi>{prettyDir(label, home)}</bdi>
      </div>
      {list.map((r) => (
        <Row
          key={r.id}
          row={r}
          active={isActive(r)}
          open={isOpen(r)}
          onOpen={() => onOpen(r)}
          onStop={() => onStop(r)}
          onDelete={() => onDelete(r)}
        />
      ))}
    </div>
  );

  return (
    <nav className="side">
      <button className="snew" onClick={onNew}>
        + new session
      </button>
      {group(serverCwd || "sessions", here)}
      {[...groups.entries()].map(([cwd, list]) => group(cwd, list))}
    </nav>
  );
}
