import { EditorState } from "@codemirror/state";
import { EditorView, ViewPlugin, type ViewUpdate } from "@codemirror/view";
import { Chunk } from "@codemirror/merge";
import type { EditorDiffLine } from "./DiffEditor";

type ChangeMapMark = {
  line: number;
  added?: boolean;
  deleted?: boolean;
};

// changeMap replaces the vertical scrollbar with a draggable change gutter.
export function changeMap(marks: ChangeMapMark[]) {
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

export function diffChangeMap(lines: EditorDiffLine[]) {
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

export function fileChangeMap(original: string | undefined, content: string) {
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
