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
