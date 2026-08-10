import { useEffect, useLayoutEffect, useRef } from "react";
import {
  Compartment,
  EditorState,
  RangeSetBuilder,
  StateEffect,
  StateField,
} from "@codemirror/state";
import {
  Decoration,
  type DecorationSet,
  drawSelection,
  EditorView,
  GutterMarker,
  gutter,
  highlightSpecialChars,
  keymap,
  lineNumbers,
} from "@codemirror/view";
import {
  HighlightStyle,
  LanguageDescription,
  syntaxHighlighting,
} from "@codemirror/language";
import { languages } from "@codemirror/language-data";
import { unifiedMergeView } from "@codemirror/merge";
import {
  defaultKeymap,
  history,
  historyKeymap,
  indentWithTab,
} from "@codemirror/commands";
import { tags } from "@lezer/highlight";

export type EditorDiffLine = {
  text: string;
  kind: "add" | "del" | "hunk" | "meta" | "plain";
  line?: number;
  hunk?: number;
};

const targetLine = StateEffect.define<number>();
const targetField = StateField.define<DecorationSet>({
  create: () => Decoration.none,
  update: (value, transaction) => {
    value = value.map(transaction.changes);
    for (const effect of transaction.effects) {
      if (!effect.is(targetLine)) continue;
      const number = effect.value;
      value =
        number > 0 && number <= transaction.state.doc.lines
          ? Decoration.set([
              Decoration.line({ class: "cm-target-line" }).range(
                transaction.state.doc.line(number).from,
              ),
            ])
          : Decoration.none;
    }
    return value;
  },
  provide: (field) => EditorView.decorations.from(field),
});

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
  "&.cm-focused": { outline: "none" },
  ".cm-scroller": {
    overflow: "auto",
    fontFamily: "var(--font-mono)",
    lineHeight: "1.55",
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
    backgroundColor: "color-mix(in srgb, var(--green) 14%, transparent)",
  },
  ".cm-diff-meta": { color: "var(--dim)" },
  ".cm-diff-number": { cursor: "pointer", textDecoration: "underline" },
  ".cm-diff-markers .cm-gutterElement": {
    minWidth: "2.5rem",
    padding: "0 0.75rem",
    overflow: "visible",
    textAlign: "center",
    whiteSpace: "nowrap",
  },
  ".cm-diff-marker-add": { color: "var(--green)" },
  ".cm-diff-marker-del": { color: "var(--red)" },
});

const baseExtensions = (label: string) => [
  EditorView.contentAttributes.of({ "aria-label": label }),
  highlightSpecialChars(),
  drawSelection(),
  syntaxHighlighting(highlightStyle),
  editorTheme,
];

const plainFile = (path: string, size: number) =>
  size > 1 << 20 ||
  /(^|\/)(package-lock\.json|pnpm-lock\.yaml|yarn\.lock|go\.sum|Cargo\.lock)$/i.test(
    path,
  );

async function languageFor(path: string, plain: boolean) {
  if (plain) return [];
  try {
    return (
      (await LanguageDescription.matchFilename(languages, path)?.load()) ?? []
    );
  } catch {
    return [];
  }
}

function replaceDocument(view: EditorView, content: string) {
  if (view.state.doc.toString() === content) return;
  view.dispatch({
    changes: { from: 0, to: view.state.doc.length, insert: content },
  });
}

function setEditorTarget(view: EditorView, line?: number) {
  const number = line && line > 0 && line <= view.state.doc.lines ? line : 0;
  const effects: StateEffect<unknown>[] = [targetLine.of(number)];
  if (number) {
    effects.push(
      EditorView.scrollIntoView(view.state.doc.line(number).from, {
        y: "center",
      }),
    );
  }
  view.dispatch({ effects });
}

export function CodeEditor({
  path,
  content,
  original,
  line,
  editable = false,
  onChange,
  onSave,
  className,
}: {
  path: string;
  content: string;
  original?: string;
  line?: number;
  editable?: boolean;
  onChange?: (content: string) => void;
  onSave?: () => void;
  className?: string;
}) {
  const host = useRef<HTMLDivElement>(null);
  const view = useRef<EditorView>(null);
  const language = useRef(new Compartment());
  const editing = useRef(new Compartment());
  const merge = useRef(new Compartment());
  const changeRef = useRef(onChange);
  const saveRef = useRef(onSave);
  const plain = plainFile(path, content.length);
  changeRef.current = onChange;
  saveRef.current = onSave;

  useLayoutEffect(() => {
    if (!host.current) return;
    view.current = new EditorView({
      parent: host.current,
      state: EditorState.create({
        doc: content,
        extensions: [
          ...baseExtensions(`Code for ${path}`),
          lineNumbers(),
          targetField,
          language.current.of([]),
          merge.current.of(
            original === undefined
              ? []
              : unifiedMergeView({
                  original,
                  gutter: false,
                  mergeControls: false,
                  allowInlineDiffs: true,
                }),
          ),
          editing.current.of([
            EditorState.readOnly.of(!editable),
            EditorView.editable.of(editable),
          ]),
          history(),
          keymap.of([
            {
              key: "Mod-s",
              preventDefault: true,
              run: () => {
                saveRef.current?.();
                return true;
              },
            },
            ...defaultKeymap,
            ...historyKeymap,
            indentWithTab,
          ]),
          EditorView.updateListener.of((update) => {
            if (update.docChanged)
              changeRef.current?.(update.state.doc.toString());
          }),
        ],
      }),
    });
    setEditorTarget(view.current, line);
    return () => {
      view.current?.destroy();
      view.current = null;
    };
  }, []);

  useEffect(() => {
    if (!view.current) return;
    replaceDocument(view.current, content);
    setEditorTarget(view.current, line);
  }, [content, line]);

  useEffect(() => {
    if (!view.current) return;
    view.current.dispatch({
      effects: merge.current.reconfigure(
        original === undefined
          ? []
          : unifiedMergeView({
              original,
              gutter: false,
              mergeControls: false,
              allowInlineDiffs: true,
            }),
      ),
    });
  }, [original]);

  useEffect(() => {
    if (!view.current) return;
    view.current.dispatch({
      effects: editing.current.reconfigure([
        EditorState.readOnly.of(!editable),
        EditorView.editable.of(editable),
      ]),
    });
  }, [editable]);

  useEffect(() => {
    let current = true;
    languageFor(path, plain).then((support) => {
      if (!current || !view.current) return;
      view.current.dispatch({ effects: language.current.reconfigure(support) });
    });
    return () => {
      current = false;
    };
  }, [path, plain]);

  return <div ref={host} className={className} role="tabpanel" />;
}

class TextMarker extends GutterMarker {
  constructor(
    readonly text: string,
    readonly elementClass = "",
  ) {
    super();
  }
  eq(other: TextMarker) {
    return this.text === other.text && this.elementClass === other.elementClass;
  }
  toDOM() {
    const span = document.createElement("span");
    span.textContent = this.text;
    return span;
  }
}

function diffDocument(lines: EditorDiffLine[]) {
  return lines
    .map((line) =>
      line.kind === "add" || line.kind === "del" || line.kind === "plain"
        ? line.text.slice(1)
        : line.text,
    )
    .join("\n");
}

function diffDecorationSet(
  state: EditorState,
  lines: EditorDiffLine[],
  selected: Set<number>,
) {
  const builder = new RangeSetBuilder<Decoration>();
  for (let i = 0; i < lines.length && i < state.doc.lines; i++) {
    const line = lines[i];
    const classes = [
      line.kind === "add" && "cm-diff-add",
      line.kind === "del" && "cm-diff-del",
      line.kind === "hunk" && "cm-diff-hunk",
      line.kind === "meta" && "cm-diff-meta",
      line.hunk !== undefined &&
        selected.has(line.hunk) &&
        "cm-diff-hunk-selected",
    ]
      .filter(Boolean)
      .join(" ");
    if (classes) {
      builder.add(
        state.doc.line(i + 1).from,
        state.doc.line(i + 1).from,
        Decoration.line({ class: classes }),
      );
    }
  }
  return builder.finish();
}

export function DiffEditor({
  path,
  lines,
  selectedHunks,
  focusHunk,
  onOpenLine,
  className,
}: {
  path: string;
  lines: EditorDiffLine[];
  selectedHunks: number[];
  focusHunk?: number;
  onOpenLine?: (line: number) => void;
  className?: string;
}) {
  const host = useRef<HTMLDivElement>(null);
  const view = useRef<EditorView>(null);
  const language = useRef(new Compartment());
  const decorations = useRef(new Compartment());
  const linesRef = useRef(lines);
  const openLineRef = useRef(onOpenLine);
  linesRef.current = lines;
  openLineRef.current = onOpenLine;
  const content = diffDocument(lines);
  const plain = plainFile(path, content.length);

  useLayoutEffect(() => {
    if (!host.current) return;
    const numberGutter = gutter({
      class: "cm-diff-numbers",
      lineMarker: (editor, block) => {
        const item =
          linesRef.current[editor.state.doc.lineAt(block.from).number - 1];
        return item?.line
          ? new TextMarker(String(item.line), "cm-diff-number")
          : null;
      },
      domEventHandlers: {
        click: (editor, block) => {
          const item =
            linesRef.current[editor.state.doc.lineAt(block.from).number - 1];
          if (!item?.line || !openLineRef.current) return false;
          openLineRef.current(item.line);
          return true;
        },
      },
    });
    const markerGutter = gutter({
      class: "cm-diff-markers",
      lineMarker: (editor, block) => {
        const item =
          linesRef.current[editor.state.doc.lineAt(block.from).number - 1];
        if (item?.kind === "add")
          return new TextMarker("+", "cm-diff-marker-add");
        if (item?.kind === "del")
          return new TextMarker("−", "cm-diff-marker-del");
        return null;
      },
    });
    const state = EditorState.create({
      doc: content,
      extensions: [
        ...baseExtensions(`Diff for ${path}`),
        EditorState.readOnly.of(true),
        EditorView.editable.of(false),
        numberGutter,
        markerGutter,
        language.current.of([]),
        decorations.current.of(
          EditorView.decorations.of(
            diffDecorationSet(
              EditorState.create({ doc: content }),
              lines,
              new Set(selectedHunks),
            ),
          ),
        ),
      ],
    });
    view.current = new EditorView({ parent: host.current, state });
    return () => {
      view.current?.destroy();
      view.current = null;
    };
  }, []);

  useEffect(() => {
    if (!view.current) return;
    replaceDocument(view.current, content);
    view.current.dispatch({
      effects: decorations.current.reconfigure(
        EditorView.decorations.of(
          diffDecorationSet(view.current.state, lines, new Set(selectedHunks)),
        ),
      ),
    });
  }, [content, lines, selectedHunks]);

  useEffect(() => {
    let current = true;
    languageFor(path, plain).then((support) => {
      if (!current || !view.current) return;
      view.current.dispatch({ effects: language.current.reconfigure(support) });
    });
    return () => {
      current = false;
    };
  }, [path, plain]);

  useEffect(() => {
    if (!view.current || focusHunk === undefined) return;
    const index = lines.findIndex((line) => line.hunk === focusHunk);
    if (index < 0 || index >= view.current.state.doc.lines) return;
    view.current.dispatch({
      effects: EditorView.scrollIntoView(
        view.current.state.doc.line(index + 1).from,
        { y: "center" },
      ),
    });
  }, [focusHunk, lines]);

  return <div ref={host} className={className} />;
}
