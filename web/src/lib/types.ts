export type Ev = { id: number; type: string; data: any };

export type Block = { id: number } & (
  | { kind: "user" | "pending" | "notice" | "think"; text: string }
  | { kind: "assistant"; text: string; open: boolean }
  | {
      kind: "tool";
      name: string;
      desc: string;
      preview: string;
      html?: string[];
      copy?: string;
      path?: string;
      file?: string;
    }
);

export type Model = { slug: string; description: string };

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
};

export type GitCompare = { base: string; changes: GitChange[] };
export type GitDiff = {
  path: string;
  root: string;
  patch: string;
  html?: string[];
  size: number;
};

export type SessionRow = {
  id: string;
  rid?: string;
  title: string;
  when: string;
  used: string;
  cwd: string;
  live: boolean;
  busy: boolean;
  pinned: boolean;
};
