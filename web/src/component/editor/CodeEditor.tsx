import { useEffect, useLayoutEffect, useRef } from "react";
import {
  Compartment,
  EditorState,
  StateEffect,
  StateField,
} from "@codemirror/state";
import {
  Decoration,
  type DecorationSet,
  EditorView,
  keymap,
  lineNumbers,
} from "@codemirror/view";
import { unifiedMergeView } from "@codemirror/merge";
import {
  defaultKeymap,
  history,
  historyKeymap,
  indentWithTab,
} from "@codemirror/commands";
import { changeMap, fileChangeMap } from "./changeMap";
import {
  baseExtensions,
  languageFor,
  plainFile,
  replaceDocument,
} from "./editorBase";

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
