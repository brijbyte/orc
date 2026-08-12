import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { ArrowDown, LoaderCircle } from "lucide-react";
import { Button } from "../ui/Button";
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
  const [following, setFollowing] = useState(true);
  const anchor = useRef<{ el: HTMLElement; top: number } | null>(null);

  useLayoutEffect(() => {
    const el = main.current;
    if (!el) return;
    const saved = store.getScroll(sid);
    stick.current = saved?.stick ?? true;
    setFollowing(stick.current);
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

  const entries: Array<Block | { group: Block[] }> = [];
  for (const block of blocks) {
    const previous = entries.at(-1);
    if (
      block.kind === "tool" &&
      previous &&
      "group" in previous &&
      previous.group[0]?.kind === "tool" &&
      previous.group[0].name === block.name
    ) {
      previous.group.push(block);
    } else if (
      block.kind === "tool" &&
      previous &&
      !("group" in previous) &&
      previous.kind === "tool" &&
      previous.name === block.name
    ) {
      entries.splice(-1, 1, { group: [previous, block] });
    } else entries.push(block);
  }

  return (
    <main
      className={s.main}
      ref={main}
      onScroll={(e) => {
        const el = e.currentTarget;
        stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
        setFollowing(stick.current);
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
      {entries.map((entry, i) => {
        if ("group" in entry) {
          const first = entry.group[0];
          const latest = entry.group.at(-1)!;
          if (first.kind !== "tool") return null;
          return (
            <details className={s.toolGroup} data-block key={entry.group[0].id}>
              <summary>
                {entry.group.length} {first.name} calls · latest:{" "}
                {latest.kind === "tool" ? latest.desc : ""}
              </summary>
              <div>
                {entry.group.map((block, index) => (
                  <BlockView
                    key={block.id}
                    b={block}
                    compactAfter={index < entry.group.length - 1}
                    onOpenFile={onOpenFile}
                  />
                ))}
              </div>
            </details>
          );
        }
        return (
          <BlockView
            key={entry.id}
            b={entry}
            compactAfter={compactPair(entry, entries[i + 1] as Block)}
            onOpenFile={onOpenFile}
            onRetry={i === entries.length - 1 ? onRetry : undefined}
          />
        );
      })}
      <div ref={bottom} />
      {!following && (
        <Button
          small
          outline
          tone="accent"
          className={s.latest}
          onClick={() => {
            stick.current = true;
            setFollowing(true);
            bottom.current?.scrollIntoView();
          }}
        >
          <ArrowDown size={13} strokeWidth={1.8} aria-hidden /> latest
        </Button>
      )}
    </main>
  );
}
