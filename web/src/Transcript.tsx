import { useEffect, useRef } from "react";
import type { Block } from "./types";
import { BlockView } from "./BlockView";

// Transcript renders the block list and follows the bottom until the user
// scrolls up.
export function Transcript({ blocks }: { blocks: Block[] }) {
  const bottom = useRef<HTMLDivElement>(null);
  const stick = useRef(true);

  useEffect(() => {
    if (stick.current) bottom.current?.scrollIntoView();
  }, [blocks]);

  return (
    <main
      onScroll={(e) => {
        const el = e.currentTarget;
        stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
      }}
    >
      {blocks.map((b, i) => (
        <BlockView key={i} b={b} />
      ))}
      <div ref={bottom} />
    </main>
  );
}
