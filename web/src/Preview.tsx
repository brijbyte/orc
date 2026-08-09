import { useState } from "react";

// lineClass reads the ± marker after the line-number gutter; numbered
// lines without a marker are write content (plain code).
const lineClass = (l: string) =>
  /^\s*\d+ \+ /.test(l)
    ? "add"
    : /^\s*\d+ - /.test(l)
      ? "del"
      : /^\s*\d+ /.test(l)
        ? "hl"
        : "ctx";

// Preview shows a tool call body truncated to max lines with an
// expand/collapse toggle. html lines arrive pre-highlighted from the
// server; gutter adds client-rendered line numbers (edit/write — bash
// renders bare).
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
    <pre className={gutter ? "preview" : "preview nogut"}>
      {shown.map((l, i) =>
        html ? (
          <div key={i} className="hl">
            {gutter && (
              <span className="ctx">{String(i + 1).padStart(4) + " "}</span>
            )}
            <span dangerouslySetInnerHTML={{ __html: l }} />
          </div>
        ) : (
          <div key={i} className={gutter ? lineClass(l) : "hl"}>
            {l}
          </div>
        ),
      )}
      {lines.length > max && (
        <button
          type="button"
          className="expander"
          onClick={() => setOpen(!open)}
        >
          {open ? "collapse" : `show ${lines.length - max} more lines`}
        </button>
      )}
    </pre>
  );
}
