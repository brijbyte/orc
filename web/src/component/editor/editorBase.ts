import { EditorState } from "@codemirror/state";
import {
  drawSelection,
  EditorView,
  highlightSpecialChars,
} from "@codemirror/view";
import {
  HighlightStyle,
  LanguageDescription,
  syntaxHighlighting,
} from "@codemirror/language";
import { languages } from "@codemirror/language-data";
import { tags } from "@lezer/highlight";

const highlightStyle = HighlightStyle.define([
  { tag: tags.comment, color: "var(--cm-comment)" },
  { tag: [tags.keyword, tags.modifier], color: "var(--cm-keyword)" },
  { tag: [tags.string, tags.special(tags.string)], color: "var(--cm-string)" },
  { tag: [tags.number, tags.bool, tags.null], color: "var(--cm-number)" },
  {
    tag: [tags.function(tags.variableName), tags.labelName],
    color: "var(--cm-function)",
  },
  {
    tag: [tags.typeName, tags.className, tags.namespace],
    color: "var(--cm-type)",
  },
  { tag: [tags.propertyName, tags.attributeName], color: "var(--cm-property)" },
  { tag: [tags.operator, tags.punctuation], color: "var(--cm-operator)" },
  { tag: tags.invalid, color: "var(--red)" },
]);

const editorTheme = EditorView.theme({
  "&": {
    height: "100%",
    backgroundColor: "var(--code-bg)",
    color: "var(--code-fg)",
    fontSize: "0.8125rem",
  },
  "&.cm-focused": {
    outline: "var(--focus-ring)",
    outlineOffset: "calc(-1 * var(--focus-offset))",
  },
  ".cm-scroller": {
    overflow: "auto",
    fontFamily: "var(--font-mono)",
    lineHeight: "1.55",
  },
  ".cm-scroller:has(.cm-change-map)": { scrollbarWidth: "none" },
  ".cm-scroller:has(.cm-change-map)::-webkit-scrollbar": {
    width: "0",
    height: "0.5rem",
  },
  ".cm-content": { minWidth: "max-content", padding: "0.75rem 0" },
  ".cm-line": { minHeight: "1.55em", padding: "0 1rem 0 0.65rem" },
  ".cm-gutters": {
    backgroundColor: "var(--code-bg)",
    borderRight: "1px solid var(--line)",
    color: "var(--dim)",
  },
  ".cm-gutterElement": { padding: "0 0.7rem" },
  ".cm-lineNumbers .cm-gutterElement": { minWidth: "5ch" },
  ".cm-selectionBackground, &.cm-focused .cm-selectionBackground": {
    backgroundColor: "color-mix(in srgb, var(--cyan) 22%, transparent)",
  },
  ".cm-cursor": { borderLeftColor: "var(--fg)" },
  ".cm-target-line": {
    backgroundColor: "color-mix(in srgb, var(--cyan) 12%, transparent)",
  },
  "&.cm-merge-b .cm-changedLine, .cm-inlineChangedLine": {
    backgroundColor: "var(--diff-add-bg)",
  },
  "&.cm-merge-b .cm-changedText": { background: "var(--diff-add-bg)" },
  ".cm-deletedChunk": {
    paddingLeft: "0.65rem",
    backgroundColor: "var(--diff-del-bg)",
  },
  ".cm-deletedChunk .cm-deletedText, &.cm-merge-b .cm-deletedText": {
    background: "var(--diff-del-bg)",
  },
  ".cm-diff-add": { backgroundColor: "var(--diff-add-bg)" },
  ".cm-diff-del": { backgroundColor: "var(--diff-del-bg)" },
  ".cm-diff-hunk": {
    backgroundColor: "color-mix(in srgb, var(--cyan) 9%, transparent)",
    color: "var(--dim)",
  },
  ".cm-diff-hunk-selected": {
    backgroundColor: "color-mix(in srgb, var(--cyan) 14%, transparent)",
  },
  ".cm-diff-meta": { color: "var(--dim)" },
  ".cm-diff-number": { cursor: "pointer", textDecoration: "underline" },
  ".cm-hunk-widget": {
    display: "inline-flex",
    margin: "0.2rem 0.65rem",
    verticalAlign: "middle",
  },
  ".cm-change-map": {
    position: "sticky",
    right: "0",
    top: "0",
    width: "0.875rem",
    height: "100%",
    flexShrink: "0",
    backgroundColor: "var(--code-bg)",
    borderLeft: "1px solid var(--line)",
    borderRight: "0",
    cursor: "pointer",
    touchAction: "none",
    userSelect: "none",
  },
  ".cm-change-map-mark, .cm-change-map-viewport": {
    position: "absolute",
    left: "0",
    right: "0",
  },
  ".cm-change-map[data-dragging]": { cursor: "grabbing" },
  ".cm-change-map-mark": { zIndex: "1", minHeight: "2px" },
  ".cm-change-map-add": { backgroundColor: "var(--cyan)" },
  ".cm-change-map-del": { backgroundColor: "var(--red)" },
  ".cm-change-map-both": {
    background: "linear-gradient(to right, var(--red) 50%, var(--cyan) 50%)",
  },
  ".cm-change-map-viewport": {
    zIndex: "2",
    boxSizing: "border-box",
    backgroundColor: "color-mix(in srgb, var(--cyan) 12%, transparent)",
    border: "1px solid color-mix(in srgb, var(--cyan) 55%, transparent)",
  },
  ".cm-diff-markers .cm-gutterElement": {
    minWidth: "2.5rem",
    padding: "0 0.75rem",
    overflow: "visible",
    textAlign: "center",
    whiteSpace: "nowrap",
  },
  ".cm-diff-marker-add": { color: "var(--cyan)" },
  ".cm-diff-marker-del": { color: "var(--red)" },
});

export const baseExtensions = (label: string) => [
  EditorView.contentAttributes.of({ "aria-label": label }),
  highlightSpecialChars(),
  drawSelection(),
  syntaxHighlighting(highlightStyle),
  editorTheme,
];

export const plainFile = (path: string, size: number) =>
  size > 1 << 20 ||
  /(^|\/)(package-lock\.json|pnpm-lock\.yaml|yarn\.lock|go\.sum|Cargo\.lock)$/i.test(
    path,
  );

export async function languageFor(path: string, plain: boolean) {
  if (plain) return [];
  try {
    return (
      (await LanguageDescription.matchFilename(languages, path)?.load()) ?? []
    );
  } catch {
    return [];
  }
}

export function replaceDocument(view: EditorView, content: string) {
  if (view.state.doc.toString() === content) return;
  view.dispatch({
    changes: { from: 0, to: view.state.doc.length, insert: content },
  });
}
