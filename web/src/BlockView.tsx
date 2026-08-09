import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import {
  BookOpen,
  Brain,
  Cog,
  FilePen,
  Pencil,
  SquareTerminal,
  Wrench,
} from "lucide-react";
import type { Block } from "./types";
import { Preview } from "./Preview";

const toolIcons: Record<string, typeof Wrench> = {
  bash: SquareTerminal,
  process: Cog,
  read: BookOpen,
  write: FilePen,
  edit: Pencil,
  skill: Brain,
};

function Markdown({ text }: { text: string }) {
  return <ReactMarkdown remarkPlugins={[remarkGfm]}>{text}</ReactMarkdown>;
}

export function BlockView({ b }: { b: Block }) {
  switch (b.kind) {
    case "user":
      return (
        <div className="user">
          <span>&gt;</span>
          <div className="md">
            <Markdown text={b.text} />
          </div>
        </div>
      );
    case "pending":
      return <div className="pending">&gt; {b.text} ⏳</div>;
    case "think":
      return <div className="think">{b.text}</div>;
    case "notice":
      return <div className="notice">{b.text}</div>;
    case "tool": {
      const Icon = toolIcons[b.name] ?? Wrench;
      return (
        <div className="tool">
          <div className="tool-line">
            <Icon size={14} strokeWidth={1.8} aria-hidden />
            <span>
              {b.name} {b.desc}
            </span>
          </div>
          {(b.preview || b.html) && <Preview text={b.preview} html={b.html} />}
        </div>
      );
    }
    case "assistant":
      return (
        <div className="assistant md">
          <Markdown text={b.text} />
        </div>
      );
  }
}
