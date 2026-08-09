import { useEffect, useLayoutEffect, useRef } from "react";
import * as store from "./store";
import type { Block } from "./types";
import { BlockView } from "./BlockView";

// Transcript renders the block list and follows the bottom until the user
// scrolls up. The position is saved per session in the store, so a
// remounted view restores where the user left off.
export function Transcript({ sid, blocks }: { sid: string; blocks: Block[] }) {
  const main = useRef<HTMLElement>(null);
  const bottom = useRef<HTMLDivElement>(null);
  const stick = useRef(true);

  // restore the saved position on mount; default to the bottom
  useLayoutEffect(() => {
    const el = main.current;
    if (!el) return;
    const saved = store.getScroll(sid);
    stick.current = saved?.stick ?? true;
    if (saved && !saved.stick) el.scrollTop = saved.top;
    else el.scrollTop = el.scrollHeight;
  }, [sid]);

  useEffect(() => {
    if (stick.current) bottom.current?.scrollIntoView();
  }, [blocks]);

  return (
    <main
      ref={main}
      onScroll={(e) => {
        const el = e.currentTarget;
        stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
        store.saveScroll(sid, el.scrollTop, stick.current);
      }}
    >
      {blocks.map((b, i) => (
        <BlockView key={i} b={b} />
      ))}
      <div ref={bottom} />
    </main>
  );
}
