import { useState } from "react";

const previewMax = 20;

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

// Preview shows an edit diff or write content, truncated to previewMax
// lines; the marker line toggles the full text. html lines arrive
// pre-highlighted from the server; their gutter is client-rendered.
export function Preview({ text, html }: { text: string; html?: string[] }) {
  const [open, setOpen] = useState(false);
  const lines = html ?? text.split("\n");
  const shown = open ? lines : lines.slice(0, previewMax);
  return (
    <pre className="preview">
      {shown.map((l, i) =>
        html ? (
          <div key={i} className="hl">
            <span className="ctx">{String(i + 1).padStart(4) + " "}</span>
            <span dangerouslySetInnerHTML={{ __html: l }} />
          </div>
        ) : (
          <div key={i} className={lineClass(l)}>
            {l}
          </div>
        ),
      )}
      {lines.length > previewMax && (
        <button
          type="button"
          className="expander"
          onClick={() => setOpen(!open)}
        >
          {open ? "collapse" : `show ${lines.length - previewMax} more lines`}
        </button>
      )}
    </pre>
  );
}
