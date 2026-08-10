import { useEffect, useLayoutEffect, useRef } from "react";
import { LoaderCircle } from "lucide-react";
import * as store from "../lib/store";
import type { Block } from "../lib/types";
import { BlockView } from "./BlockView";
import s from "./Transcript.module.css";

type Props = {
  sid: string;
  blocks: Block[];
  hasMore: boolean;
  loadingOlder: boolean;
  onOpenFile: (path: string, ref: string) => void;
  onRetry?: () => void; // offered on the last block only
};

function compactPair(a: Block, b?: Block) {
  return (
    (a.kind === "think" && b?.kind === "think") ||
    (a.kind === "tool" && b?.kind === "tool" && a.name === b.name)
  );
}

// Transcript follows the bottom until the user scrolls up. Scroll state stays
// in the store so a remounted session restores its position.
export function Transcript({
  sid,
  blocks,
  hasMore,
  loadingOlder,
  onOpenFile,
  onRetry,
}: Props) {
  const main = useRef<HTMLElement>(null);
  const bottom = useRef<HTMLDivElement>(null);
  const stick = useRef(true);
  const anchor = useRef<{ el: HTMLElement; top: number } | null>(null);

  useLayoutEffect(() => {
    const el = main.current;
    if (!el) return;
    const saved = store.getScroll(sid);
    stick.current = saved?.stick ?? true;
    if (saved && !saved.stick) el.scrollTop = saved.top;
    else el.scrollTop = el.scrollHeight;
  }, [sid]);

  // Keep the first old block at the same viewport position after a prepend.
  useLayoutEffect(() => {
    const saved = anchor.current;
    const el = main.current;
    if (!saved || !el || loadingOlder) return;
    if (saved.el.isConnected) {
      el.scrollTop += saved.el.getBoundingClientRect().top - saved.top;
      store.saveScroll(sid, el.scrollTop, false);
    }
    anchor.current = null;
  }, [blocks, loadingOlder, sid]);

  useEffect(() => {
    if (stick.current) bottom.current?.scrollIntoView();
  }, [blocks]);

  const loadOlder = (el: HTMLElement) => {
    if (!hasMore || loadingOlder || anchor.current) return;
    const first = el.querySelector<HTMLElement>(":scope > [data-block]");
    if (!first) return;
    anchor.current = { el: first, top: first.getBoundingClientRect().top };
    void store.loadOlder(sid);
  };

  return (
    <main
      className={s.main}
      ref={main}
      onScroll={(e) => {
        const el = e.currentTarget;
        stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
        store.saveScroll(sid, el.scrollTop, stick.current);
        if (el.scrollTop < 120) loadOlder(el);
      }}
    >
      {loadingOlder && (
        <div className={s.loading} role="status">
          <LoaderCircle size={13} strokeWidth={1.8} aria-hidden />
          loading
        </div>
      )}
      {blocks.map((b, i) => (
        <BlockView
          key={b.id}
          b={b}
          compactAfter={compactPair(b, blocks[i + 1])}
          onOpenFile={onOpenFile}
          onRetry={i === blocks.length - 1 ? onRetry : undefined}
        />
      ))}
      <div ref={bottom} />
    </main>
  );
}
