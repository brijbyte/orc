import { useState } from "react";
import s from "./Preview.module.css";

// lineClass reads the ± marker after the line-number gutter; numbered
// lines without a marker are write content (plain code). "row" is ours —
// chroma owns short class names like "hl" inside .chroma.
const lineClass = (l: string) =>
  /^\s*\d+ \+ /.test(l)
    ? s.add
    : /^\s*\d+ - /.test(l)
      ? s.del
      : /^\s*\d+ /.test(l)
        ? s.row
        : s.ctx;

// Preview shows a tool call body truncated to max lines with an
// expand/collapse toggle. html lines arrive pre-highlighted from the
// server, gutter and ± markers included; gutter only says whether those
// prefixes are there (bash renders bare).
export function Preview({
  text,
  html,
  gutter,
  max,
}: {
  text: string;
  html?: string[];
  gutter: boolean;
  max: number;
}) {
  const [open, setOpen] = useState(false);
  const lines = html ?? text.split("\n");
  const shown = open ? lines : lines.slice(0, max);
  return (
    <pre className={`${s.preview} chroma${gutter ? "" : " " + s.nogut}`}>
      {shown.map((l, i) =>
        html ? (
          <div
            key={i}
            className={gutter ? lineClass(l) : s.row}
            dangerouslySetInnerHTML={{ __html: l }}
          />
        ) : (
          <div key={i} className={gutter ? lineClass(l) : s.row}>
            {l}
          </div>
        ),
      )}
      {lines.length > max && (
        <button
          type="button"
          className={s.expander}
          onClick={() => setOpen(!open)}
        >
          {open ? "collapse" : `show ${lines.length - max} more lines`}
        </button>
      )}
    </pre>
  );
}
