export type Ev = { id: number; type: string; data: any };

export type Block =
  | { kind: "user" | "pending" | "notice" | "think"; text: string }
  | { kind: "assistant"; text: string; open: boolean }
  | {
      kind: "tool";
      name: string;
      desc: string;
      preview: string;
      html?: string[];
      copy?: string;
    };

export type Model = { slug: string; description: string };
