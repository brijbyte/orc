import ReactMarkdown, { type Components } from "react-markdown";
import { Collapsible } from "@base-ui/react/collapsible";
import remarkGfm from "remark-gfm";
import {
  BookOpen,
  Bot,
  Brain,
  CheckCircle2,
  ChevronRight,
  CircleX,
  Clock3,
  Cog,
  FilePen,
  Inbox,
  LockKeyhole,
  Minimize2,
  Paperclip,
  TextCursor,
  RefreshCw,
  RotateCcw,
  RotateCw,
  Sparkles,
  SquareTerminal,
  TriangleAlert,
  UserRound,
  Wrench,
} from "lucide-react";
import type { Block } from "../lib/types";
import { Preview } from "./Preview";
import { CopyButton } from "./CopyButton";
import { markdownComponents } from "./MarkdownContent";
import { Button } from "../ui/Button";
import s from "./BlockView.module.css";

const toolIcons: Record<string, typeof Wrench> = {
  bash: SquareTerminal,
  process: Cog,
  read: BookOpen,
  write: FilePen,
  edit: TextCursor,
  skill: Brain,
};

export const toolIcon = (name: string) => toolIcons[name] ?? Wrench;

const noticeIcons = [
  ["❌", CircleX],
  ["⚠️", TriangleAlert],
  ["📭", Inbox],
  ["🗜️", Minimize2],
  ["🔐", LockKeyhole],
  ["✅", CheckCircle2],
  ["🔄", RefreshCw],
  ["↩️", RotateCcw],
  ["🧌", Bot],
  ["📎", Paperclip],
] as const;

function Notice({ text }: { text: string }) {
  const match = noticeIcons.find(([mark]) => text.startsWith(mark));
  if (!match) return text;
  const [mark, Icon] = match;
  return (
    <>
      <Icon className={s.noticeIcon} size={14} strokeWidth={1.8} aria-hidden />
      {text.slice(mark.length).trimStart()}
    </>
  );
}

// wide tables scroll in their own container instead of widening the column
const mdComponents: Components = {
  ...markdownComponents,
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

function EchoContent({
  text,
  markdown = false,
}: {
  text: string;
  markdown?: boolean;
}) {
  const lines = text.split("\n");
  const first = lines.findIndex((line) => line.startsWith("📎 "));
  const hasAttachments =
    first >= 0 && lines.slice(first).every((line) => line.startsWith("📎 "));
  const body = hasAttachments ? lines.slice(0, first).join("\n") : text;
  const attachments = hasAttachments ? lines.slice(first) : [];
  return (
    <>
      {markdown ? <Markdown text={body} /> : body}
      {attachments.map((line) => (
        <span className={s.attachment} key={line}>
          <Paperclip size={12} strokeWidth={1.8} aria-hidden />
          {line.slice(3)}
        </span>
      ))}
    </>
  );
}

export function BlockView({
  b,
  compactAfter,
  onOpenFile,
  onRetry,
  grouped = false,
}: {
  b: Block;
  compactAfter: boolean;
  onOpenFile: (path: string, ref: string) => void;
  onRetry?: () => void;
  grouped?: boolean;
}) {
  const blockAttrs = {
    "data-block": "",
    "data-compact-after": compactAfter ? "" : undefined,
  };

  switch (b.kind) {
    case "user":
      return (
        <div className={s.user} {...blockAttrs}>
          <UserRound size={14} strokeWidth={1.8} aria-hidden />
          <div className={s.md}>
            <EchoContent text={b.text} markdown />
          </div>
          <CopyButton text={b.text} />
        </div>
      );
    case "pending":
      return (
        <div className={s.pending} {...blockAttrs}>
          <UserRound size={14} strokeWidth={1.8} aria-hidden />
          <span className={s.pendingText}>
            <EchoContent text={b.text} />
          </span>
          <Clock3 size={13} strokeWidth={1.8} aria-label="queued" />
        </div>
      );
    case "think":
      return (
        <Collapsible.Root className={s.think} {...blockAttrs}>
          <Collapsible.Trigger className={s.thinkHead}>
            <ChevronRight
              className={s.thinkChevron}
              size={13}
              strokeWidth={1.8}
              aria-hidden
            />
            <Sparkles size={12} strokeWidth={1.8} aria-hidden />
            <span>reasoning</span>
          </Collapsible.Trigger>
          <Collapsible.Panel className={s.thinkPanel}>
            <div className={`${s.thinkPanelContent} ${s.md}`}>
              <Markdown text={b.text} />
            </div>
          </Collapsible.Panel>
          <CopyButton text={b.text} />
        </Collapsible.Root>
      );
    case "notice":
      return (
        <div
          className={s.notice}
          data-severity={
            b.text.startsWith("❌")
              ? "error"
              : b.text.startsWith("⚠️")
                ? "warning"
                : undefined
          }
          {...blockAttrs}
        >
          <Notice text={b.text} />
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
      const Icon = toolIcon(b.name);
      const bash = b.name === "bash";
      const preview = b.preview || (grouped && bash ? b.desc : "");
      return (
        <div className={s.tool} {...blockAttrs}>
          {grouped ? (
            !bash &&
            (b.path && b.file ? (
              <Button
                link
                tone="accent"
                className={`${s.filePath} ${s.groupedDesc}`}
                onClick={() => onOpenFile(b.path!, b.file!)}
                title={b.path}
              >
                {b.desc}
              </Button>
            ) : (
              b.desc && <div className={s.groupedDesc}>{b.desc}</div>
            ))
          ) : (
            <div className={s.toolLine}>
              <Icon size={14} strokeWidth={1.8} aria-hidden />
              <span className={s.toolName}>{b.name}</span>
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
          )}
          {preview && (
            <Preview
              text={preview}
              path={bash ? "command.sh" : b.path}
              gutter={!bash}
              max={bash ? 5 : b.name === "edit" ? 10 : 20}
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
