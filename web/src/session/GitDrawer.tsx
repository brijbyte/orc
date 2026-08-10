import { useEffect, useMemo, useRef, useState } from "react";
import { Dialog } from "@base-ui/react/dialog";
import type { GitStatusEntry } from "@pierre/trees";
import { FileTree, useFileTree } from "@pierre/trees/react";
import {
  ArrowDown,
  ArrowUp,
  ArrowUpRight,
  Check,
  ExternalLink,
  GitBranch as BranchIcon,
  History,
  ListChecks,
  LoaderCircle,
  Paperclip,
  RefreshCw,
  Undo2,
  X,
} from "lucide-react";
import { api, APIError } from "../lib/api";
import type {
  GitActivity,
  GitChange,
  GitCompare,
  GitDiff,
  GitStatus,
} from "../lib/types";
import { Button } from "../ui/Button";
import { Select } from "../ui/Select";
import d from "../ui/dialog.module.css";
import s from "./GitDrawer.module.css";

const WORKTREE = "@worktree";

type ChangeMode = "worktree" | "staged" | "compare";
type TreeChange = {
  id: string;
  mode: ChangeMode;
  change: GitChange;
};

type DiffLine = {
  text: string;
  html?: string;
  kind: "add" | "del" | "hunk" | "meta" | "plain";
  line?: number;
  hunk?: number;
};

function parseDiff(diff: GitDiff): DiffLine[] {
  const raw = diff.patch.split("\n");
  if (raw.at(-1) === "") raw.pop();
  let next = 0;
  let hunk = -1;
  return raw.map((text, i) => {
    if (text.startsWith("@@")) {
      const match = text.match(/\+(\d+)/);
      if (match) next = Number(match[1]);
      return { text, html: diff.html?.[i], kind: "hunk", hunk: ++hunk };
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

function changeID(mode: ChangeMode, path: string): string {
  return `${mode}:${path}`;
}

function workingTreeChanges(status: GitStatus): TreeChange[] {
  return status.changes.flatMap((change) => {
    const entries: TreeChange[] = [];
    if (change.index !== "." && change.index !== "?") {
      entries.push({
        id: changeID("staged", change.path),
        mode: "staged",
        change,
      });
    }
    if (change.worktree !== "." || change.index === "?") {
      entries.push({
        id: changeID("worktree", change.path),
        mode: "worktree",
        change,
      });
    }
    return entries;
  });
}

function compareChanges(changes: GitChange[]): TreeChange[] {
  return changes.map((change) => ({
    id: changeID("compare", change.path),
    mode: "compare",
    change,
  }));
}

function group(entry: TreeChange): string {
  if (entry.mode === "staged") return "Staged changes";
  if (entry.mode === "worktree") return "Unstaged changes";
  return "Branch changes";
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
  selected,
  onSelection,
}: {
  changes: TreeChange[];
  selected: string[];
  onSelection: (changes: TreeChange[]) => void;
}) {
  const data = useMemo(() => {
    const byPath = new Map<string, TreeChange>();
    const paths = changes.map((entry) => {
      const path = `${group(entry)}/${entry.change.path}`;
      byPath.set(path, entry);
      return path;
    });
    return {
      byPath,
      paths,
      expanded: expandedPaths(paths),
      gitStatus: paths.map((path) => ({
        path,
        status: treeStatus(byPath.get(path)!.change),
      })),
    };
  }, [changes]);
  const dataRef = useRef(data);
  const onSelectionRef = useRef(onSelection);
  dataRef.current = data;
  onSelectionRef.current = onSelection;
  const initialSelectedPaths = [...data.byPath].flatMap(([path, entry]) =>
    selected.includes(entry.id) ? [path] : [],
  );
  const { model } = useFileTree({
    density: "compact",
    gitStatus: data.gitStatus,
    icons: { set: "complete", colored: true },
    initialExpandedPaths: data.expanded,
    initialExpansion: "open",
    initialSelectedPaths,
    paths: data.paths,
    search: true,
    stickyFolders: true,
    unsafeCSS: `[data-file-tree-search-container] { padding-top: 8px; }`,
    onSelectionChange: (paths) => {
      const next = paths.flatMap((path) => {
        const change = dataRef.current.byPath.get(path);
        return change ? [change] : [];
      });
      onSelectionRef.current(next);
    },
  });

  useEffect(() => {
    model.resetPaths(data.paths, { initialExpandedPaths: data.expanded });
    model.setGitStatus(data.gitStatus);
  }, [model, data]);

  useEffect(() => {
    const wanted = new Set(
      [...data.byPath].flatMap(([path, entry]) =>
        selected.includes(entry.id) ? [path] : [],
      ),
    );
    for (const old of model.getSelectedPaths()) {
      if (!wanted.has(old)) model.getItem(old)?.deselect();
    }
    for (const path of wanted) {
      if (!model.getItem(path)?.isSelected()) model.getItem(path)?.select();
    }
  }, [model, data, selected]);

  return (
    <FileTree className={s.tree} model={model} aria-label="changed files" />
  );
}

function Activity({ entries }: { entries: GitActivity[] }) {
  if (!entries.length) return null;
  return (
    <section className={s.activity} aria-label="Git activity">
      <strong>
        <History size={12} strokeWidth={1.8} aria-hidden /> activity
      </strong>
      {entries.slice(0, 5).map((entry, i) => (
        <div key={`${entry.at}:${i}`} title={entry.paths.join("\n")}>
          <span>{entry.action}</span>
          <span>
            {entry.hunks
              ? `${entry.hunks} hunk${entry.hunks === 1 ? "" : "s"}`
              : `${entry.paths.length} file${entry.paths.length === 1 ? "" : "s"}`}
          </span>
          <time dateTime={entry.at}>
            {new Date(entry.at).toLocaleTimeString([], {
              hour: "2-digit",
              minute: "2-digit",
            })}
          </time>
        </div>
      ))}
    </section>
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
  const [changes, setChanges] = useState<TreeChange[]>([]);
  const [selectedIDs, setSelectedIDs] = useState<string[]>([]);
  const [activeID, setActiveID] = useState("");
  const [diff, setDiff] = useState<GitDiff | null>(null);
  const [source, setSource] = useState(WORKTREE);
  const [selectedHunks, setSelectedHunks] = useState<number[]>([]);
  const [refresh, setRefresh] = useState(0);
  const [mutating, setMutating] = useState(false);
  const [err, setErr] = useState("");
  const [diffErr, setDiffErr] = useState("");
  const [mutationErr, setMutationErr] = useState("");
  const close = useRef<HTMLButtonElement>(null);
  const loadedDiff = useRef("");
  const selected = changes.find((entry) => entry.id === activeID) ?? null;

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
          setSelectedIDs([]);
          setActiveID("");
          return;
        }
        const list =
          source === WORKTREE
            ? workingTreeChanges(next)
            : compareChanges(
                ((await api.gitCompare(sid, source)) as GitCompare).changes,
              );
        if (!current) return;
        setChanges(list);
        setSelectedIDs((old) => {
          const kept = old.filter((id) =>
            list.some((entry) => entry.id === id),
          );
          return kept.length ? kept : list[0] ? [list[0].id] : [];
        });
        setActiveID((old) =>
          list.some((entry) => entry.id === old) ? old : (list[0]?.id ?? ""),
        );
      })
      .catch(() => current && setErr("cannot read Git status"));
    return () => {
      current = false;
    };
  }, [sid, open, source, refresh, request]);

  useEffect(() => {
    if (!open || !selected) {
      setDiff(null);
      loadedDiff.current = "";
      return;
    }
    let current = true;
    const key = `${sid}:${source}:${selected.id}`;
    if (loadedDiff.current !== key) setDiff(null);
    setDiffErr("");
    api
      .gitDiff(
        sid,
        selected.change.path,
        selected.mode === "compare" ? source : "",
        selected.mode === "staged" ? "staged" : "worktree",
      )
      .then((next: GitDiff) => {
        if (!current) return;
        loadedDiff.current = key;
        setDiff(next);
      })
      .catch(
        () =>
          current && setDiffErr(`cannot read diff for ${selected.change.path}`),
      );
    return () => {
      current = false;
    };
  }, [sid, open, selected, source, refresh, request]);

  useEffect(() => setSelectedHunks([]), [diff?.hash, source, activeID]);

  const lines = diff ? parseDiff(diff) : [];
  const hunkCount = lines.filter((line) => line.kind === "hunk").length;
  const canMutate = source === WORKTREE;
  const action = selected?.mode === "staged" ? "unstage" : "stage";
  const stagedSelected = changes.filter(
    (entry) => entry.mode === "staged" && selectedIDs.includes(entry.id),
  );
  const unstagedSelected = changes.filter(
    (entry) => entry.mode === "worktree" && selectedIDs.includes(entry.id),
  );
  const branchOptions = [
    { value: WORKTREE, label: "Working tree" },
    ...(status?.branches ?? [])
      .filter((branch) => !branch.current)
      .map((branch) => ({
        value: branch.ref,
        label: `Compare ${branch.name}`,
      })),
  ];

  const mutate = async (
    stage: boolean,
    entries: TreeChange[],
    hunks?: number[],
  ) => {
    if (!canMutate || !entries.length) return;
    const mutation = stage ? "stage" : "unstage";
    setMutating(true);
    setMutationErr("");
    try {
      const next = (await (stage ? api.gitStage : api.gitUnstage)(
        sid,
        entries.map((entry) => entry.change.path),
        hunks,
        hunks?.length ? diff?.hash : undefined,
      )) as GitStatus;
      setStatus(next);
      const list = workingTreeChanges(next);
      setChanges(list);
      const kept = selectedIDs.filter((id) =>
        list.some((entry) => entry.id === id),
      );
      const nextSelection = kept.length ? kept : list[0] ? [list[0].id] : [];
      setSelectedIDs(nextSelection);
      setActiveID((old) =>
        list.some((entry) => entry.id === old)
          ? old
          : (nextSelection.at(-1) ?? ""),
      );
      setSelectedHunks([]);
      setRefresh((value) => value + 1);
    } catch (error) {
      setMutationErr(
        error instanceof APIError && error.status === 409
          ? "The diff changed. Review it and try again."
          : `cannot ${mutation} the selected changes`,
      );
      setRefresh((value) => value + 1);
    } finally {
      setMutating(false);
    }
  };

  const addFile = () => {
    if (!selected?.change.file) return;
    api.file(sid, selected.change.file).then((data: { content: string }) => {
      onAddContext(
        new File([data.content], basename(selected.change.path), {
          type: "text/plain",
        }),
        `Review \`${selected.change.path}\`.`,
      );
    });
  };

  const addDiff = () => {
    if (!diff || !selected) return;
    onAddContext(
      new File([diff.patch], `${basename(selected.change.path)}.diff`, {
        type: "text/x-diff",
      }),
      `Review the attached Git diff for \`${selected.change.path}\`.`,
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
              <Select
                value={source}
                options={branchOptions}
                onChange={setSource}
              />
            )}
            <Button
              icon
              tip="refresh Git status"
              disabled={mutating}
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
                  {source === WORKTREE ? (
                    <>
                      <span>
                        {
                          changes.filter((entry) => entry.mode === "staged")
                            .length
                        }{" "}
                        staged
                      </span>
                      <span>
                        {
                          changes.filter((entry) => entry.mode === "worktree")
                            .length
                        }{" "}
                        unstaged
                      </span>
                    </>
                  ) : (
                    <span>{changes.length} changed</span>
                  )}
                </div>
                {canMutate && changes.length > 0 && (
                  <div className={s.selectionBar}>
                    <span>{selectedIDs.length} selected</span>
                    <Button
                      link
                      small
                      onClick={() => {
                        setSelectedIDs(changes.map((entry) => entry.id));
                        setActiveID(changes.at(-1)?.id ?? "");
                      }}
                    >
                      select all
                    </Button>
                    <Button
                      link
                      small
                      disabled={!selectedIDs.length}
                      onClick={() => setSelectedIDs([])}
                    >
                      clear
                    </Button>
                    <Button
                      outline
                      small
                      tone="success"
                      disabled={mutating || !unstagedSelected.length}
                      onClick={() => void mutate(true, unstagedSelected)}
                    >
                      <Check size={13} strokeWidth={1.8} aria-hidden />
                      stage files
                    </Button>
                    <Button
                      outline
                      small
                      disabled={mutating || !stagedSelected.length}
                      onClick={() => void mutate(false, stagedSelected)}
                    >
                      <Undo2 size={13} strokeWidth={1.8} aria-hidden />
                      unstage files
                    </Button>
                  </div>
                )}
                {changes.length === 0 && (
                  <div className={s.empty}>
                    {source === WORKTREE
                      ? "Working tree is clean."
                      : "No branch changes."}
                  </div>
                )}
                {changes.length > 0 && (
                  <ChangeTree
                    changes={changes}
                    selected={selectedIDs}
                    onSelection={(next) => {
                      const ids = next.map((entry) => entry.id);
                      setSelectedIDs(ids);
                      if (ids.length) setActiveID(ids.at(-1)!);
                    }}
                  />
                )}
                <Activity entries={status.activity ?? []} />
              </aside>
              <main className={s.diff}>
                {selected && (
                  <div className={s.diffHead}>
                    <strong title={selected.change.path}>
                      {selected.change.path}
                    </strong>
                    {selected.change.file && (
                      <Button
                        outline
                        small
                        onClick={() =>
                          onOpenFile(
                            selected.change.path,
                            selected.change.file!,
                          )
                        }
                      >
                        <ExternalLink size={13} strokeWidth={1.8} aria-hidden />
                        open
                      </Button>
                    )}
                    {selected.change.file && (
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
                    {canMutate &&
                      selected.mode !== "compare" &&
                      selectedHunks.length > 0 && (
                        <Button
                          outline
                          small
                          tone={
                            selected.mode === "worktree" ? "success" : undefined
                          }
                          disabled={mutating}
                          onClick={() =>
                            void mutate(
                              selected.mode === "worktree",
                              [selected],
                              selectedHunks,
                            )
                          }
                        >
                          <ListChecks size={13} strokeWidth={1.8} aria-hidden />
                          {action} {selectedHunks.length} hunk
                          {selectedHunks.length === 1 ? "" : "s"}
                        </Button>
                      )}
                  </div>
                )}
                {mutationErr && (
                  <div className={`${s.message} ${s.error}`}>{mutationErr}</div>
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
                      <div
                        className={s.diffLine}
                        data-kind={line.kind}
                        data-selected={
                          line.hunk !== undefined &&
                          selectedHunks.includes(line.hunk)
                            ? true
                            : undefined
                        }
                        key={i}
                      >
                        <span className={s.lineNumber}>
                          {line.line && selected?.change.file ? (
                            <Button
                              link
                              tip={`open line ${line.line}`}
                              onClick={() =>
                                onOpenFile(
                                  selected.change.path,
                                  selected.change.file!,
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
                        {canMutate && line.hunk !== undefined && (
                          <Button
                            outline
                            small
                            className={s.hunkButton}
                            tone={
                              selectedHunks.includes(line.hunk)
                                ? "success"
                                : undefined
                            }
                            onClick={() =>
                              setSelectedHunks((old) =>
                                old.includes(line.hunk!)
                                  ? old.filter((hunk) => hunk !== line.hunk)
                                  : [...old, line.hunk!].sort((a, b) => a - b),
                              )
                            }
                          >
                            {selectedHunks.includes(line.hunk)
                              ? "selected"
                              : "select hunk"}
                          </Button>
                        )}
                      </div>
                    ))}
                  </pre>
                )}
                {canMutate && diff?.patch && hunkCount === 0 && (
                  <div className={s.message}>
                    This change can only be {action}d as a file.
                  </div>
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
