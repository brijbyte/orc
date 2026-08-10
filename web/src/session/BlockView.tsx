import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import {
  BookOpen,
  Brain,
  Cog,
  FilePen,
  Pencil,
  RotateCw,
  Sparkles,
  SquareTerminal,
  Wrench,
} from "lucide-react";
import type { Block } from "../lib/types";
import { Preview } from "./Preview";
import { CopyButton } from "./CopyButton";
import { Button } from "../ui/Button";
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

export function BlockView({
  b,
  compactAfter,
  onOpenFile,
  onRetry,
}: {
  b: Block;
  compactAfter: boolean;
  onOpenFile: (path: string, ref: string) => void;
  onRetry?: () => void;
}) {
  const blockAttrs = {
    "data-block": "",
    "data-compact-after": compactAfter ? "" : undefined,
  };

  switch (b.kind) {
    case "user":
      return (
        <div className={s.user} {...blockAttrs}>
          <span>&gt;</span>
          <div className={s.md}>
            <Markdown text={b.text} />
          </div>
          <CopyButton text={b.text} />
        </div>
      );
    case "pending":
      return (
        <div className={s.pending} {...blockAttrs}>
          &gt; {b.text} ⏳
        </div>
      );
    case "think":
      return (
        <div className={s.think} {...blockAttrs}>
          <div className={s.thinkHead}>
            <Sparkles size={12} strokeWidth={1.8} aria-hidden />
            <span>thinking</span>
          </div>
          <div className={s.md}>
            <Markdown text={b.text} />
          </div>
          <CopyButton text={b.text} />
        </div>
      );
    case "notice":
      return (
        <div className={s.notice} {...blockAttrs}>
          {b.text}
          {onRetry && (
            <Button
              outline
              small
              tone="accent"
              className={s.retry}
              onClick={onRetry}
            >
              <RotateCw size={12} strokeWidth={1.8} aria-hidden />
              try again
            </Button>
          )}
          <CopyButton text={b.text} />
        </div>
      );
    case "tool": {
      const Icon = toolIcons[b.name] ?? Wrench;
      const bash = b.name === "bash";
      return (
        <div className={s.tool} {...blockAttrs}>
          <div className={s.toolLine}>
            <Icon size={14} strokeWidth={1.8} aria-hidden />
            <span>{b.name}</span>
            {b.path && b.file ? (
              <Button
                link
                tone="accent"
                className={s.filePath}
                onClick={() => onOpenFile(b.path!, b.file!)}
                title={b.path}
              >
                {b.desc}
              </Button>
            ) : (
              <span>{b.desc}</span>
            )}
          </div>
          {(b.preview || b.html) && (
            <Preview
              text={b.preview}
              html={b.html}
              gutter={!bash}
              max={bash ? 5 : 20}
            />
          )}
          <CopyButton text={b.copy ?? b.desc} />
        </div>
      );
    }
    case "assistant":
      return (
        <div className={`${s.assistant} ${s.md}`} {...blockAttrs}>
          <Markdown text={b.text} />
          <CopyButton text={b.text} />
        </div>
      );
  }
}
