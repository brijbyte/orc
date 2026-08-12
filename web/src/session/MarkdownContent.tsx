import type { Components } from "react-markdown";
import { MarkdownCode } from "./MarkdownCode";
import { MermaidDiagram } from "./MermaidDiagram";

// Shared overrides for every Markdown surface. Fenced languages are parsed by
// CodeMirror's lazy Lezer language packages; Mermaid fences render diagrams.
export const markdownComponents: Components = {
  a: ({ node: _, ...props }) => (
    <a {...props} target="_blank" rel="noopener noreferrer" />
  ),
  code: ({ node: _, className, children, ...props }) => {
    const language = /(?:^|\s)language-([^\s]+)/.exec(className ?? "")?.[1];
    if (!language)
      return (
        <code className={className} {...props}>
          {children}
        </code>
      );
    const code = String(children);
    if (language.toLowerCase() === "mermaid")
      return <MermaidDiagram code={code} />;
    return (
      <MarkdownCode code={code} language={language} className={className} />
    );
  },
};
