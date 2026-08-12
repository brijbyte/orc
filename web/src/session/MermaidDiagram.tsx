import { useEffect, useId, useState } from "react";
import s from "./MermaidDiagram.module.css";

let initialized = false;

function message(error: unknown): string {
  if (!(error instanceof Error)) return "Cannot render diagram";
  return error.message.split("\n", 1)[0] || "Cannot render diagram";
}

// MermaidDiagram lazy-loads Mermaid so ordinary Markdown does not pay for its
// sizeable graph engine. Mermaid sanitizes labels and returns SVG markup.
export function MermaidDiagram({ code }: { code: string }) {
  const reactID = useId();
  const [svg, setSVG] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    let current = true;
    setSVG("");
    setError("");
    import("mermaid")
      .then(async ({ default: mermaid }) => {
        if (!initialized) {
          mermaid.initialize({
            startOnLoad: false,
            securityLevel: "strict",
            suppressErrorRendering: true,
            theme: "base",
          });
          initialized = true;
        }
        const id = `mermaid-${reactID.replace(/[^a-zA-Z0-9_-]/g, "")}`;
        const result = await mermaid.render(id, code.trim());
        if (current) setSVG(result.svg);
      })
      .catch((reason: unknown) => current && setError(message(reason)));
    return () => {
      current = false;
    };
  }, [code, reactID]);

  if (error)
    return (
      <span className={s.error} role="img" aria-label={`Mermaid: ${error}`}>
        <span>{error}</span>
        <code>{code}</code>
      </span>
    );
  if (!svg)
    return (
      <span className={s.loading} role="status">
        rendering diagram…
      </span>
    );
  return (
    <span
      className={s.diagram}
      role="img"
      aria-label="Mermaid diagram"
      dangerouslySetInnerHTML={{ __html: svg }}
    />
  );
}
