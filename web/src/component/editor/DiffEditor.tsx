import { useEffect, useLayoutEffect, useRef } from "react";
import { Compartment, EditorState, RangeSetBuilder } from "@codemirror/state";
import {
  Decoration,
  EditorView,
  GutterMarker,
  gutter,
  WidgetType,
} from "@codemirror/view";
import { createRoot, type Root } from "react-dom/client";
import { Button } from "../../ui/Button";
import { changeMap, diffChangeMap } from "./changeMap";
import {
  baseExtensions,
  languageFor,
  plainFile,
  replaceDocument,
} from "./editorBase";

export type EditorDiffLine = {
  text: string;
  kind: "add" | "del" | "hunk" | "meta" | "plain";
  line?: number;
  hunk?: number;
};

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
