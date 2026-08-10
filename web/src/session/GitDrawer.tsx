import { useEffect, useMemo, useRef, useState } from "react";
import { Dialog } from "@base-ui/react/dialog";
import type { GitStatusEntry } from "@pierre/trees";
import { FileTree, useFileTree } from "@pierre/trees/react";
import {
  ArrowDown,
  ArrowUp,
  ArrowUpRight,
  ExternalLink,
  GitBranch as BranchIcon,
  LoaderCircle,
  Paperclip,
  RefreshCw,
  X,
} from "lucide-react";
import { api } from "../lib/api";
import type { GitChange, GitCompare, GitDiff, GitStatus } from "../lib/types";
import { Button } from "../ui/Button";
import { Select } from "../ui/Select";
import d from "../ui/dialog.module.css";
import s from "./GitDrawer.module.css";

const WORKTREE = "@worktree";

type DiffLine = {
  text: string;
  html?: string;
  kind: "add" | "del" | "hunk" | "meta" | "plain";
  line?: number;
};

function parseDiff(diff: GitDiff): DiffLine[] {
  const raw = diff.patch.split("\n");
  if (raw.at(-1) === "") raw.pop();
  let next = 0;
  return raw.map((text, i) => {
    if (text.startsWith("@@")) {
      const match = text.match(/\+(\d+)/);
      if (match) next = Number(match[1]);
      return { text, html: diff.html?.[i], kind: "hunk" };
    }
    if (text.startsWith("+++ ") || text.startsWith("--- "))
      return { text, html: diff.html?.[i], kind: "meta" };
    if (text.startsWith("+"))
      return { text, html: diff.html?.[i], kind: "add", line: next++ };
    if (text.startsWith("-"))
      return { text, html: diff.html?.[i], kind: "del" };
    if (text.startsWith(" "))
      return { text, html: diff.html?.[i], kind: "plain", line: next++ };
    return { text, html: diff.html?.[i], kind: "meta" };
  });
}

function group(change: GitChange, comparing: boolean): string {
  if (comparing) return "Branch changes";
  if (change.status === "Conflicted") return "Conflicts";
  if (change.index === "?") return "Untracked";
  if (change.index !== ".") return "Staged";
  return "Changes";
}

function basename(path: string): string {
  return path.split("/").at(-1) || "file";
}

function treeStatus(change: GitChange): GitStatusEntry["status"] {
  switch (change.status) {
    case "Added":
      return "added";
    case "Deleted":
      return "deleted";
    case "Renamed":
      return "renamed";
    case "Untracked":
      return "untracked";
    default:
      return "modified";
  }
}

function expandedPaths(paths: string[]): string[] {
  const dirs = new Set<string>();
  for (const path of paths) {
    const parts = path.split("/");
    for (let i = 1; i < parts.length; i++)
      dirs.add(parts.slice(0, i).join("/"));
  }
  return [...dirs];
}

function ChangeTree({
  changes,
  comparing,
  selected,
  onSelect,
}: {
  changes: GitChange[];
  comparing: boolean;
  selected: GitChange | null;
  onSelect: (change: GitChange) => void;
}) {
  const data = useMemo(() => {
    const byPath = new Map<string, GitChange>();
    const paths = changes.map((change) => {
      const path = `${group(change, comparing)}/${change.path}`;
      byPath.set(path, change);
      return path;
    });
    return {
      byPath,
      paths,
      expanded: expandedPaths(paths),
      gitStatus: paths.map((path) => ({
        path,
        status: treeStatus(byPath.get(path)!),
      })),
    };
  }, [changes, comparing]);
  const dataRef = useRef(data);
  const onSelectRef = useRef(onSelect);
  dataRef.current = data;
  onSelectRef.current = onSelect;
  const { model } = useFileTree({
    density: "compact",
    gitStatus: data.gitStatus,
    icons: { set: "complete", colored: true },
    initialExpandedPaths: data.expanded,
    initialExpansion: "open",
    initialSelectedPaths: selected
      ? [...data.byPath].flatMap(([path, change]) =>
          change.path === selected.path ? [path] : [],
        )
      : [],
    paths: data.paths,
    search: true,
    stickyFolders: true,
    unsafeCSS: `[data-file-tree-search-container] { padding-top: 8px; }`,
    onSelectionChange: (paths) => {
      const change = dataRef.current.byPath.get(paths.at(-1) ?? "");
      if (change) onSelectRef.current(change);
    },
  });

  useEffect(() => {
    model.resetPaths(data.paths, { initialExpandedPaths: data.expanded });
    model.setGitStatus(data.gitStatus);
  }, [model, data]);

  useEffect(() => {
    const path = [...data.byPath].find(
      ([, change]) => change.path === selected?.path,
    )?.[0];
    for (const old of model.getSelectedPaths()) {
      if (old !== path) model.getItem(old)?.deselect();
    }
    if (path && !model.getItem(path)?.isSelected())
      model.getItem(path)?.select();
  }, [model, data, selected]);

  return (
    <FileTree className={s.tree} model={model} aria-label="changed files" />
  );
}

export function GitDrawer({
  sid,
  open,
  request,
  onClose,
  onOpenFile,
  onAddContext,
}: {
  sid: string;
  open: boolean;
  request: number;
  onClose: () => void;
  onOpenFile: (path: string, ref: string, line?: number) => void;
  onAddContext: (file: File, prompt: string) => void;
}) {
  const [status, setStatus] = useState<GitStatus | null>(null);
  const [changes, setChanges] = useState<GitChange[]>([]);
  const [selected, setSelected] = useState<GitChange | null>(null);
  const [diff, setDiff] = useState<GitDiff | null>(null);
  const [base, setBase] = useState(WORKTREE);
  const [refresh, setRefresh] = useState(0);
  const [err, setErr] = useState("");
  const [diffErr, setDiffErr] = useState("");
  const close = useRef<HTMLButtonElement>(null);
  const loadedDiff = useRef("");

  useEffect(() => {
    if (!open) return;
    let current = true;
    setErr("");
    api
      .gitStatus(sid)
      .then(async (next: GitStatus) => {
        if (!current) return;
        setStatus(next);
        if (!next.repo) {
          setChanges([]);
          setSelected(null);
          return;
        }
        const list =
          base === WORKTREE
            ? next.changes
            : ((await api.gitCompare(sid, base)) as GitCompare).changes;
        if (!current) return;
        setChanges(list);
        setSelected(
          (old) =>
            list.find((change) => change.path === old?.path) ?? list[0] ?? null,
        );
      })
      .catch(() => current && setErr("cannot read Git status"));
    return () => {
      current = false;
    };
  }, [sid, open, base, refresh, request]);

  useEffect(() => {
    if (!open || !selected) {
      setDiff(null);
      loadedDiff.current = "";
      return;
    }
    let current = true;
    const key = `${sid}:${base}:${selected.path}`;
    if (loadedDiff.current !== key) setDiff(null);
    setDiffErr("");
    api
      .gitDiff(sid, selected.path, base === WORKTREE ? "" : base)
      .then((next: GitDiff) => {
        if (!current) return;
        loadedDiff.current = key;
        setDiff(next);
      })
      .catch(
        () => current && setDiffErr(`cannot read diff for ${selected.path}`),
      );
    return () => {
      current = false;
    };
  }, [sid, open, selected, base, refresh, request]);

  const lines = diff ? parseDiff(diff) : [];
  const branchOptions = [
    { value: WORKTREE, label: "Working tree" },
    ...(status?.branches ?? [])
      .filter((branch) => !branch.current)
      .map((branch) => ({
        value: branch.ref,
        label: `Compare ${branch.name}`,
      })),
  ];

  const addFile = () => {
    if (!selected?.file) return;
    api.file(sid, selected.file).then((data: { content: string }) => {
      onAddContext(
        new File([data.content], basename(selected.path), {
          type: "text/plain",
        }),
        `Review \`${selected.path}\`.`,
      );
    });
  };

  const addDiff = () => {
    if (!diff || !selected) return;
    onAddContext(
      new File([diff.patch], `${basename(selected.path)}.diff`, {
        type: "text/x-diff",
      }),
      `Review the attached Git diff for \`${selected.path}\`.`,
    );
  };

  return (
    <Dialog.Root open={open} onOpenChange={(next) => !next && onClose()}>
      <Dialog.Portal>
        <Dialog.Backdrop className={d.overlay} />
        <Dialog.Popup className={s.drawer} initialFocus={close}>
          <header className={s.head}>
            <Dialog.Title className={s.title}>
              <BranchIcon size={16} strokeWidth={1.8} aria-hidden /> Git
            </Dialog.Title>
            {status?.repo && (
              <Select value={base} options={branchOptions} onChange={setBase} />
            )}
            <Button
              icon
              tip="refresh Git status"
              onClick={() => setRefresh((value) => value + 1)}
            >
              <RefreshCw size={16} strokeWidth={1.8} aria-hidden />
            </Button>
            <Dialog.Close
              ref={close}
              render={<Button icon />}
              aria-label="close Git"
            >
              <X size={17} strokeWidth={1.8} aria-hidden />
            </Dialog.Close>
          </header>
          {!status && !err && (
            <div className={s.message}>
              <LoaderCircle
                className={s.spinner}
                size={14}
                strokeWidth={1.8}
                aria-hidden
              />
              loading
            </div>
          )}
          {err && <div className={`${s.message} ${s.error}`}>{err}</div>}
          {status && !status.repo && (
            <div className={s.message}>
              This directory is not a Git repository.
            </div>
          )}
          {status?.repo && (
            <div className={s.body}>
              <aside className={s.side}>
                <div className={s.summary}>
                  <strong>
                    {status.detached ? "detached HEAD" : status.branch}
                  </strong>
                  {status.upstream && (
                    <span>
                      <ArrowUpRight size={12} strokeWidth={1.8} aria-hidden />
                      {status.upstream}
                    </span>
                  )}
                  {(status.ahead > 0 || status.behind > 0) && (
                    <span>
                      <ArrowUp size={12} strokeWidth={1.8} aria-label="ahead" />
                      {status.ahead}
                      <ArrowDown
                        size={12}
                        strokeWidth={1.8}
                        aria-label="behind"
                      />
                      {status.behind}
                    </span>
                  )}
                  <span>{changes.length} changed</span>
                </div>
                {changes.length === 0 && (
                  <div className={s.empty}>
                    {base === WORKTREE
                      ? "Working tree is clean."
                      : "No branch changes."}
                  </div>
                )}
                {changes.length > 0 && (
                  <ChangeTree
                    changes={changes}
                    comparing={base !== WORKTREE}
                    selected={selected}
                    onSelect={setSelected}
                  />
                )}
              </aside>
              <main className={s.diff}>
                {selected && (
                  <div className={s.diffHead}>
                    <strong title={selected.path}>{selected.path}</strong>
                    {selected.file && (
                      <Button
                        outline
                        small
                        onClick={() =>
                          onOpenFile(selected.path, selected.file!)
                        }
                      >
                        <ExternalLink size={13} strokeWidth={1.8} aria-hidden />
                        open
                      </Button>
                    )}
                    {selected.file && (
                      <Button outline small onClick={addFile}>
                        <Paperclip size={13} strokeWidth={1.8} aria-hidden />{" "}
                        file
                      </Button>
                    )}
                    {diff?.patch && (
                      <Button outline small onClick={addDiff}>
                        <Paperclip size={13} strokeWidth={1.8} aria-hidden />{" "}
                        diff
                      </Button>
                    )}
                  </div>
                )}
                {selected && !diff && !diffErr && (
                  <div className={s.message}>
                    <LoaderCircle
                      className={s.spinner}
                      size={14}
                      strokeWidth={1.8}
                      aria-hidden
                    />
                    loading diff
                  </div>
                )}
                {diffErr && (
                  <div className={`${s.message} ${s.error}`}>{diffErr}</div>
                )}
                {diff && !diff.patch && (
                  <div className={s.message}>No text diff.</div>
                )}
                {diff && diff.patch && (
                  <pre className={`${s.patch} chroma`}>
                    {lines.map((line, i) => (
                      <div className={s.diffLine} data-kind={line.kind} key={i}>
                        <span className={s.lineNumber}>
                          {line.line && selected?.file ? (
                            <Button
                              link
                              tip={`open line ${line.line}`}
                              onClick={() =>
                                onOpenFile(
                                  selected.path,
                                  selected.file!,
                                  line.line,
                                )
                              }
                            >
                              {line.line}
                            </Button>
                          ) : null}
                        </span>
                        {line.html ? (
                          <span
                            dangerouslySetInnerHTML={{ __html: line.html }}
                          />
                        ) : (
                          <span>{line.text}</span>
                        )}
                      </div>
                    ))}
                  </pre>
                )}
                {!selected && changes.length > 0 && (
                  <div className={s.message}>
                    Select a file to view its diff.
                  </div>
                )}
              </main>
            </div>
          )}
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
