import { useMemo, useState } from "react";
import { ChevronDown, ChevronUp } from "lucide-react";
import { Button } from "../ui/Button";
import { syntaxNodes, useSyntaxRanges } from "./MarkdownCode";
import s from "./Preview.module.css";

type PreviewLine = {
  code: string;
  prefix: string;
  kind: "add" | "del" | "row" | "ctx";
  from: number;
  to: number;
};

function previewLines(text: string, gutter: boolean): PreviewLine[] {
  let offset = 0;
  return text.split("\n").map((text) => {
    let code = text;
    let prefix = "";
    let kind: PreviewLine["kind"] = gutter ? "ctx" : "row";
    if (gutter) {
      const diff = text.match(/^(\s*\d+ [+-] )(.*)$/);
      const numbered = text.match(/^(\s*\d+ )(.*)$/);
      if (diff) {
        [, prefix, code] = diff;
        kind = prefix.includes(" + ") ? "add" : "del";
      } else if (numbered) {
        [, prefix, code] = numbered;
        kind = "row";
      }
    }
    const line = { code, prefix, kind, from: offset, to: offset + code.length };
    offset = line.to + 1;
    return line;
  });
}

export function Preview({
  text,
  path,
  gutter,
  max,
}: {
  text: string;
  path?: string;
  gutter: boolean;
  max: number;
}) {
  const [open, setOpen] = useState(false);
  const lines = useMemo(() => previewLines(text, gutter), [text, gutter]);
  const code = useMemo(
    () => lines.map((line) => line.code).join("\n"),
    [lines],
  );
  const ranges = useSyntaxRanges({ code, path });
  const shown = open ? lines : lines.slice(0, max);

  return (
    <pre className={`${s.preview}${gutter ? "" : " " + s.nogut}`}>
      {shown.map((line, i) => (
        <div key={i} className={s[line.kind]}>
          {line.prefix && <span className={s.prefix}>{line.prefix}</span>}
          <code>{syntaxNodes(code, ranges, line.from, line.to)}</code>
        </div>
      ))}
      {lines.length > max && (
        <Button
          outline
          small
          className={s.expander}
          onClick={() => setOpen(!open)}
        >
          {open ? (
            <ChevronUp size={13} strokeWidth={1.8} aria-hidden />
          ) : (
            <ChevronDown size={13} strokeWidth={1.8} aria-hidden />
          )}
          {open ? "collapse" : `show ${lines.length - max} more lines`}
        </Button>
      )}
    </pre>
  );
}
