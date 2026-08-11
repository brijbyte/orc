export type Ev = { id: number; type: string; data: any };

export type Block = { id: number } & (
  | { kind: "user" | "pending" | "notice" | "think"; text: string }
  | { kind: "assistant"; text: string; open: boolean }
  | {
      kind: "tool";
      name: string;
      desc: string;
      preview: string;
      copy?: string;
      path?: string;
      file?: string;
    }
);

export type Model = {
  slug: string;
  name?: string;
  description: string;
  efforts?: string[];
};

export type ServerAttachment = {
  kind: "server";
  name: string;
  path: string;
  dir: boolean;
  size: number;
};

export type ComposerAttachment = File | ServerAttachment;

export type BrowseEntry = { name: string; dir: boolean; size: number };
export type BrowseResult = {
  path: string;
  parent: string;
  entries: BrowseEntry[];
};

export type GitBranch = {
  name: string;
  ref: string;
  remote: boolean;
  current: boolean;
};

export type GitChange = {
  path: string;
  old_path?: string;
  status: string;
  index: string;
  worktree: string;
  file?: string;
};

export type GitActivity = {
  at: string;
  action: "stage" | "unstage" | "commit" | "discard" | "remove" | "undo";
  paths: string[];
  hunks?: number;
};

export type GitRecovery = {
  at: string;
  action: "discard" | "remove";
  paths: string[];
  hunks?: number;
};

export type GitStatus = {
  repo: boolean;
  root?: string;
  branch?: string;
  detached?: boolean;
  upstream?: string;
  ahead: number;
  behind: number;
  clean: boolean;
  changes: GitChange[];
  branches: GitBranch[];
  activity: GitActivity[];
  recovery?: GitRecovery;
};

export type GitCompare = { base: string; changes: GitChange[] };
export type GitDiff = {
  path: string;
  root: string;
  patch: string;
  size: number;
  hash: string;
};

export type SessionRow = {
  id: string;
  rid?: string;
  title: string;
  when: string;
  used: string;
  cwd: string;
  routine?: string;
  wake?: string;
  live: boolean;
  busy: boolean;
  pinned: boolean;
};
