import type { Block, Ev } from "./types";

// apply folds one event into the block list (mutates and returns it).
export function apply(blocks: Block[], ev: Ev): Block[] {
  const last = blocks[blocks.length - 1];
  switch (ev.type) {
    case "user": {
      const i = blocks.findIndex(
        (b) => b.kind === "pending" && b.text === ev.data.text,
      );
      if (i >= 0) blocks.splice(i, 1);
      blocks.push({ kind: "user", text: ev.data.text });
      break;
    }
    case "pending":
      blocks.push({ kind: "pending", text: ev.data.text });
      break;
    case "delta":
      if (last?.kind === "assistant" && last.open) last.text += ev.data.text;
      else blocks.push({ kind: "assistant", text: ev.data.text, open: true });
      break;
    case "think":
      if (last?.kind === "think") last.text += ev.data.text;
      else blocks.push({ kind: "think", text: ev.data.text });
      break;
    case "turn_end":
      if (last?.kind === "assistant") last.open = false;
      break;
    case "tool":
      blocks.push({
        kind: "tool",
        name: ev.data.name,
        desc: ev.data.desc,
        preview: ev.data.preview,
        html: ev.data.html,
      });
      break;
    case "notice":
      blocks.push({ kind: "notice", text: ev.data.text });
      break;
  }
  return blocks;
}
