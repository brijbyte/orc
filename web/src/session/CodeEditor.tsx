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
  ViewPlugin,
  type ViewUpdate,
  WidgetType,
} from "@codemirror/view";
import {
  HighlightStyle,
  LanguageDescription,
  syntaxHighlighting,
} from "@codemirror/language";
import { languages } from "@codemirror/language-data";
import { Chunk, unifiedMergeView } from "@codemirror/merge";
import {
  defaultKeymap,
  history,
  historyKeymap,
  indentWithTab,
} from "@codemirror/commands";
import { tags } from "@lezer/highlight";
import { createRoot, type Root } from "react-dom/client";
import { Button } from "../ui/Button";

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
    backgroundColor: "color-mix(in srgb, var(--green) 14%, transparent)",
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
  ".cm-change-map-add": { backgroundColor: "var(--green)" },
  ".cm-change-map-del": { backgroundColor: "var(--red)" },
  ".cm-change-map-both": {
    background: "linear-gradient(to right, var(--red) 50%, var(--green) 50%)",
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
  ".cm-diff-marker-add": { color: "var(--green)" },
  ".cm-diff-marker-del": { color: "var(--red)" },
});

type ChangeMapMark = {
  line: number;
  added?: boolean;
  deleted?: boolean;
};

function changeMap(marks: ChangeMapMark[]) {
  if (!marks.length) return [];
  return ViewPlugin.fromClass(
    class {
      readonly dom = document.createElement("div");
      readonly viewport = document.createElement("div");
      readonly resize: ResizeObserver;
      readonly onScroll = () => this.renderViewport();
      dragOffset: number | null = null;
      readonly onPointerDown = (event: PointerEvent) => {
        if (event.button !== 0) return;
        event.preventDefault();
        const viewport = this.viewport.getBoundingClientRect();
        this.dragOffset = this.viewport.contains(event.target as Node)
          ? event.clientY - viewport.top
          : viewport.height / 2;
        this.dom.dataset.dragging = "";
        this.dom.setPointerCapture(event.pointerId);
        this.scrollFromPointer(event.clientY);
      };
      readonly onPointerMove = (event: PointerEvent) => {
        if (this.dragOffset !== null) this.scrollFromPointer(event.clientY);
      };
      readonly onPointerUp = (event: PointerEvent) => {
        if (this.dragOffset === null) return;
        this.dragOffset = null;
        delete this.dom.dataset.dragging;
        if (this.dom.hasPointerCapture(event.pointerId)) {
          this.dom.releasePointerCapture(event.pointerId);
        }
      };
      readonly onKeyDown = (event: KeyboardEvent) => {
        const { clientHeight, scrollHeight } = this.view.scrollDOM;
        let top: number | undefined;
        switch (event.key) {
          case "ArrowUp":
            top = this.view.scrollDOM.scrollTop - this.view.defaultLineHeight;
            break;
          case "ArrowDown":
            top = this.view.scrollDOM.scrollTop + this.view.defaultLineHeight;
            break;
          case "PageUp":
            top = this.view.scrollDOM.scrollTop - clientHeight;
            break;
          case "PageDown":
            top = this.view.scrollDOM.scrollTop + clientHeight;
            break;
          case "Home":
            top = 0;
            break;
          case "End":
            top = scrollHeight;
            break;
        }
        if (top === undefined) return;
        event.preventDefault();
        this.view.scrollDOM.scrollTo({ top });
      };

      constructor(readonly view: EditorView) {
        this.dom.className = "cm-gutters cm-change-map";
        this.dom.setAttribute("role", "scrollbar");
        this.dom.setAttribute("aria-orientation", "vertical");
        this.dom.setAttribute("aria-label", "editor change map and scrollbar");
        this.dom.tabIndex = 0;
        this.viewport.className = "cm-change-map-viewport";
        const count = Math.max(1, view.state.doc.lines);
        for (const mark of marks) {
          const line = document.createElement("div");
          const kind =
            mark.added && mark.deleted ? "both" : mark.added ? "add" : "del";
          line.className = `cm-change-map-mark cm-change-map-${kind}`;
          line.style.top = `${((mark.line - 1) / count) * 100}%`;
          line.style.height = `${100 / count}%`;
          this.dom.appendChild(line);
        }
        this.dom.appendChild(this.viewport);
        view.scrollDOM.insertBefore(this.dom, view.contentDOM.nextSibling);
        view.scrollDOM.addEventListener("scroll", this.onScroll);
        this.dom.addEventListener("pointerdown", this.onPointerDown);
        this.dom.addEventListener("pointermove", this.onPointerMove);
        this.dom.addEventListener("pointerup", this.onPointerUp);
        this.dom.addEventListener("pointercancel", this.onPointerUp);
        this.dom.addEventListener("keydown", this.onKeyDown);
        this.resize = new ResizeObserver(this.onScroll);
        this.resize.observe(view.scrollDOM);
        this.renderViewport();
      }

      update(update: ViewUpdate) {
        if (update.geometryChanged) this.renderViewport();
      }

      scrollFromPointer(clientY: number) {
        const track = this.dom.getBoundingClientRect();
        const viewportHeight = this.viewport.getBoundingClientRect().height;
        const travel = Math.max(0, track.height - viewportHeight);
        const offset = Math.min(
          travel,
          Math.max(0, clientY - track.top - (this.dragOffset ?? 0)),
        );
        const { clientHeight, scrollHeight } = this.view.scrollDOM;
        const scrollTravel = Math.max(0, scrollHeight - clientHeight);
        this.view.scrollDOM.scrollTop = travel
          ? (offset / travel) * scrollTravel
          : 0;
      }

      renderViewport() {
        const { clientHeight, scrollHeight, scrollTop } = this.view.scrollDOM;
        const height = scrollHeight ? (clientHeight / scrollHeight) * 100 : 100;
        const top = scrollHeight ? (scrollTop / scrollHeight) * 100 : 0;
        this.viewport.style.top = `${top}%`;
        this.viewport.style.height = `${height}%`;
        this.dom.setAttribute("aria-valuemin", "0");
        this.dom.setAttribute(
          "aria-valuemax",
          String(Math.max(0, scrollHeight - clientHeight)),
        );
        this.dom.setAttribute("aria-valuenow", String(Math.round(scrollTop)));
      }

      destroy() {
        this.resize.disconnect();
        this.view.scrollDOM.removeEventListener("scroll", this.onScroll);
        this.dom.removeEventListener("pointerdown", this.onPointerDown);
        this.dom.removeEventListener("pointermove", this.onPointerMove);
        this.dom.removeEventListener("pointerup", this.onPointerUp);
        this.dom.removeEventListener("pointercancel", this.onPointerUp);
        this.dom.removeEventListener("keydown", this.onKeyDown);
        this.dom.remove();
      }
    },
  );
}

function setMark(
  marks: Map<number, ChangeMapMark>,
  line: number,
  kind: "added" | "deleted",
) {
  const mark = marks.get(line) ?? { line };
  mark[kind] = true;
  marks.set(line, mark);
}

function diffChangeMap(lines: EditorDiffLine[]) {
  const marks = new Map<number, ChangeMapMark>();
  for (let i = 0; i < lines.length; i++) {
    if (lines[i].kind === "add") setMark(marks, i + 1, "added");
    if (lines[i].kind === "del") setMark(marks, i + 1, "deleted");
  }
  return [...marks.values()];
}

function markLines(
  marks: Map<number, ChangeMapMark>,
  state: EditorState,
  from: number,
  to: number,
  kind: "added" | "deleted",
) {
  if (from >= to) return;
  const start = state.doc.lineAt(Math.min(from, state.doc.length)).number;
  const end = state.doc.lineAt(Math.min(to, state.doc.length)).number;
  for (let line = start; line <= end; line++) setMark(marks, line, kind);
}

function fileChangeMap(original: string | undefined, content: string) {
  if (original === undefined) return [];
  const before = EditorState.create({ doc: original });
  const after = EditorState.create({ doc: content });
  const marks = new Map<number, ChangeMapMark>();
  for (const chunk of Chunk.build(before.doc, after.doc, {
    scanLimit: 500,
    timeout: 20,
  })) {
    if (chunk.fromB < chunk.toB) {
      markLines(marks, after, chunk.fromB, chunk.endB, "added");
    }
    if (chunk.fromA < chunk.toA) {
      if (chunk.fromB < chunk.toB) {
        markLines(marks, after, chunk.fromB, chunk.endB, "deleted");
      } else {
        setMark(
          marks,
          after.doc.lineAt(Math.min(chunk.fromB, after.doc.length)).number,
          "deleted",
        );
      }
    }
  }
  return [...marks.values()];
}

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
  const map = useRef(new Compartment());
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
          map.current.of(changeMap(fileChangeMap(original, content))),
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
      effects: map.current.reconfigure(
        changeMap(fileChangeMap(original, content)),
      ),
    });
  }, [content, original]);

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

class HunkWidget extends WidgetType {
  private root?: Root;

  constructor(
    readonly hunk: number,
    readonly selected: boolean,
    readonly onToggle: (hunk: number) => void,
  ) {
    super();
  }

  eq(other: HunkWidget) {
    return this.hunk === other.hunk && this.selected === other.selected;
  }

  toDOM() {
    const host = document.createElement("span");
    host.className = "cm-hunk-widget";
    this.root = createRoot(host);
    this.root.render(
      <Button
        outline
        small
        tone={this.selected ? "success" : undefined}
        aria-label={`${this.selected ? "deselect" : "select"} hunk ${this.hunk + 1}`}
        aria-pressed={this.selected}
        onMouseDown={(event) => event.stopPropagation()}
        onClick={() => this.onToggle(this.hunk)}
      >
        hunk {this.hunk + 1}
      </Button>,
    );
    return host;
  }

  destroy() {
    this.root?.unmount();
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
  onToggle?: (hunk: number) => void,
) {
  const builder = new RangeSetBuilder<Decoration>();
  for (let i = 0; i < lines.length && i < state.doc.lines; i++) {
    const line = lines[i];
    const docLine = state.doc.line(i + 1);
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
        docLine.from,
        docLine.from,
        Decoration.line({ class: classes }),
      );
    }
    if (line.kind === "hunk" && line.hunk !== undefined && onToggle) {
      builder.add(
        docLine.to,
        docLine.to,
        Decoration.widget({
          widget: new HunkWidget(line.hunk, selected.has(line.hunk), onToggle),
          side: 1,
        }),
      );
    }
  }
  return builder.finish();
}

export function DiffEditor({
  path,
  lines,
  selectedHunks,
  onToggleHunk,
  onOpenLine,
  className,
}: {
  path: string;
  lines: EditorDiffLine[];
  selectedHunks: number[];
  onToggleHunk?: (hunk: number) => void;
  onOpenLine?: (line: number) => void;
  className?: string;
}) {
  const host = useRef<HTMLDivElement>(null);
  const view = useRef<EditorView>(null);
  const language = useRef(new Compartment());
  const decorations = useRef(new Compartment());
  const map = useRef(new Compartment());
  const linesRef = useRef(lines);
  const openLineRef = useRef(onOpenLine);
  const toggleHunkRef = useRef(onToggleHunk);
  const toggleHunk = useRef((hunk: number) =>
    toggleHunkRef.current?.(hunk),
  ).current;
  linesRef.current = lines;
  openLineRef.current = onOpenLine;
  toggleHunkRef.current = onToggleHunk;
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
        map.current.of(changeMap(diffChangeMap(lines))),
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
              onToggleHunk ? toggleHunk : undefined,
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
          diffDecorationSet(
            view.current.state,
            lines,
            new Set(selectedHunks),
            onToggleHunk ? toggleHunk : undefined,
          ),
        ),
      ),
    });
  }, [content, lines, selectedHunks, onToggleHunk, toggleHunk]);

  useEffect(() => {
    if (!view.current) return;
    view.current.dispatch({
      effects: map.current.reconfigure(changeMap(diffChangeMap(lines))),
    });
  }, [content, lines]);

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

  return <div ref={host} className={className} />;
}
