import { useEffect, useState, type ReactNode } from "react";
import { LanguageDescription } from "@codemirror/language";
import { languages } from "@codemirror/language-data";
import { highlightTree, tagHighlighter, tags } from "@lezer/highlight";
import s from "./MarkdownCode.module.css";

export type SyntaxRange = { from: number; to: number; className: string };

const highlighter = tagHighlighter([
  { tag: tags.comment, class: s.comment },
  { tag: [tags.keyword, tags.modifier], class: s.keyword },
  {
    tag: [tags.string, tags.special(tags.string), tags.regexp, tags.escape],
    class: s.string,
  },
  {
    tag: [tags.number, tags.bool, tags.null, tags.atom, tags.literal],
    class: s.number,
  },
  {
    tag: [tags.function(tags.variableName), tags.labelName],
    class: s.function,
  },
  {
    tag: [tags.typeName, tags.className, tags.namespace, tags.tagName],
    class: s.type,
  },
  {
    tag: [tags.propertyName, tags.attributeName],
    class: s.property,
  },
  { tag: [tags.operator, tags.punctuation], class: s.operator },
  { tag: tags.invalid, class: s.invalid },
]);

function languageDescription(language?: string, path?: string) {
  if (language)
    return LanguageDescription.matchLanguageName(languages, language, true);
  return path ? LanguageDescription.matchFilename(languages, path) : null;
}

function plainCode(path: string | undefined, size: number) {
  return (
    size > 1 << 20 ||
    /(^|\/)(package-lock\.json|pnpm-lock\.yaml|yarn\.lock|go\.sum|Cargo\.lock)$/i.test(
      path ?? "",
    )
  );
}

export function useSyntaxRanges({
  code,
  language,
  path,
}: {
  code: string;
  language?: string;
  path?: string;
}) {
  const [ranges, setRanges] = useState<SyntaxRange[]>([]);

  useEffect(() => {
    let current = true;
    setRanges([]);
    const description = plainCode(path, code.length)
      ? null
      : languageDescription(language, path);
    if (!description) return () => undefined;

    description
      .load()
      .then((support) => {
        if (!current) return;
        const next: SyntaxRange[] = [];
        highlightTree(
          support.language.parser.parse(code),
          highlighter,
          (from, to, classes) => next.push({ from, to, className: classes }),
        );
        if (current) setRanges(next);
      })
      .catch(() => undefined);

    return () => {
      current = false;
    };
  }, [code, language, path]);

  return ranges;
}

export function syntaxNodes(
  code: string,
  ranges: SyntaxRange[],
  from = 0,
  to = code.length,
): ReactNode[] {
  let offset = from;
  const content: ReactNode[] = [];
  for (const range of ranges) {
    if (range.to <= from) continue;
    if (range.from >= to) break;
    const start = Math.max(range.from, from);
    const end = Math.min(range.to, to);
    if (start > offset) content.push(code.slice(offset, start));
    content.push(
      <span className={range.className} key={`${start}:${end}`}>
        {code.slice(start, end)}
      </span>,
    );
    offset = end;
  }
  if (offset < to) content.push(code.slice(offset, to));
  return content;
}

export function MarkdownCode({
  code,
  language,
  className,
}: {
  code: string;
  language: string;
  className?: string;
}) {
  const ranges = useSyntaxRanges({ code, language });
  return <code className={className}>{syntaxNodes(code, ranges)}</code>;
}
