import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import {
  BookOpen,
  Brain,
  Cog,
  FilePen,
  Pencil,
  Sparkles,
  SquareTerminal,
  Wrench,
} from "lucide-react";
import type { Block } from "./types";
import { Preview } from "./Preview";
import { CopyBtn } from "./CopyBtn";
import s from "./BlockView.module.css";

const toolIcons: Record<string, typeof Wrench> = {
  bash: SquareTerminal,
  process: Cog,
  read: BookOpen,
  write: FilePen,
  edit: Pencil,
  skill: Brain,
};

// wide tables scroll in their own container instead of widening the column
const mdComponents: Components = {
  table: ({ node: _, ...props }) => (
    <div className={s.tblwrap}>
      <table {...props} />
    </div>
  ),
};

function Markdown({ text }: { text: string }) {
  return (
    <ReactMarkdown remarkPlugins={[remarkGfm]} components={mdComponents}>
      {text}
    </ReactMarkdown>
  );
}

export function BlockView({ b }: { b: Block }) {
  switch (b.kind) {
    case "user":
      return (
        <div className={s.user} data-block>
          <span>&gt;</span>
          <div className={s.md}>
            <Markdown text={b.text} />
          </div>
          <CopyBtn text={b.text} />
        </div>
      );
    case "pending":
      return <div className={s.pending} data-block>
          &gt; {b.text} ⏳
        </div>;
    case "think":
      return (
        <div className={s.think} data-block>
          <div className={s.thinkHead}>
            <Sparkles size={12} strokeWidth={1.8} aria-hidden />
            <span>thinking</span>
          </div>
          <div className={s.md}>
            <Markdown text={b.text} />
          </div>
          <CopyBtn text={b.text} />
        </div>
      );
    case "notice":
      return (
        <div className={s.notice} data-block>
          {b.text}
          <CopyBtn text={b.text} />
        </div>
      );
    case "tool": {
      const Icon = toolIcons[b.name] ?? Wrench;
      const bash = b.name === "bash";
      return (
        <div className={s.tool} data-block>
          <div className={s.toolLine}>
            <Icon size={14} strokeWidth={1.8} aria-hidden />
            <span>
              {b.name} {b.desc}
            </span>
          </div>
          {(b.preview || b.html) && (
            <Preview
              text={b.preview}
              html={b.html}
              gutter={!bash}
              max={bash ? 5 : 20}
            />
          )}
          <CopyBtn text={b.copy ?? b.desc} />
        </div>
      );
    }
    case "assistant":
      return (
        <div className={`${s.assistant} ${s.md}`} data-block>
          <Markdown text={b.text} />
          <CopyBtn text={b.text} />
        </div>
      );
  }
}
