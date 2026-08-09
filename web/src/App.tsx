import { useCallback, useEffect, useRef, useState } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import {
  BookOpen,
  Brain,
  Cog,
  FilePen,
  Pencil,
  SquareTerminal,
  Wrench,
} from "lucide-react";

const token = window.location.hash.slice(1);

type Ev = { id: number; type: string; data: any };
type Block =
  | { kind: "user" | "pending" | "notice" | "think"; text: string }
  | { kind: "assistant"; text: string; open: boolean }
  | { kind: "tool"; name: string; desc: string; diff: string };

const toolIcons: Record<string, typeof Wrench> = {
  bash: SquareTerminal,
  process: Cog,
  read: BookOpen,
  write: FilePen,
  edit: Pencil,
  skill: Brain,
};

// apply folds one event into the block list (mutates and returns it).
function apply(blocks: Block[], ev: Ev): Block[] {
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
        diff: ev.data.diff,
      });
      break;
    case "notice":
      blocks.push({ kind: "notice", text: ev.data.text });
      break;
  }
  return blocks;
}

function Diff({ diff }: { diff: string }) {
  return (
    <pre className="diff">
      {diff.split("\n").map((l, i) => (
        <div key={i} className={l.startsWith("  +") ? "add" : l.startsWith("  -") ? "del" : "ctx"}>
          {l}
        </div>
      ))}
    </pre>
  );
}

function BlockView({ b }: { b: Block }) {
  switch (b.kind) {
    case "user":
      return <div className="user">&gt; {b.text}</div>;
    case "pending":
      return <div className="pending">&gt; {b.text} ⏳</div>;
    case "think":
      return <div className="think">{b.text}</div>;
    case "notice":
      return <div className="notice">{b.text}</div>;
    case "tool": {
      const Icon = toolIcons[b.name] ?? Wrench;
      return (
        <div className="tool">
          <div className="tool-line">
            <Icon size={14} strokeWidth={1.8} aria-hidden />
            <span>
              {b.name} {b.desc}
            </span>
          </div>
          {b.diff && <Diff diff={b.diff} />}
        </div>
      );
    }
    case "assistant":
      return (
        <div className="assistant">
          <ReactMarkdown remarkPlugins={[remarkGfm]}>{b.text}</ReactMarkdown>
        </div>
      );
  }
}

export default function App() {
  const [blocks, setBlocks] = useState<Block[]>([]);
  const [busy, setBusy] = useState(false);
  const [status, setStatus] = useState("");
  const [input, setInput] = useState("");
  const [dead, setDead] = useState(false);
  const lastID = useRef(0);
  const bottom = useRef<HTMLDivElement>(null);
  const stick = useRef(true);

  const onEvent = useCallback((ev: Ev) => {
    lastID.current = ev.id;
    if (ev.type === "busy") setBusy(ev.data.busy);
    else if (ev.type === "status") setStatus(ev.data.text);
    else setBlocks((prev) => apply([...prev], ev));
  }, []);

  useEffect(() => {
    let es: EventSource | null = null;
    fetch(`/api/state?token=${token}`)
      .then((r) => {
        if (!r.ok) throw new Error(String(r.status));
        return r.json();
      })
      .then((s) => {
        const bs: Block[] = [];
        for (const ev of s.events ?? []) {
          lastID.current = ev.id;
          if (ev.type !== "busy" && ev.type !== "status") apply(bs, ev);
        }
        setBlocks(bs);
        setBusy(s.busy);
        setStatus(s.status);
        es = new EventSource(`/api/events?token=${token}&after=${lastID.current}`);
        es.onmessage = (m) => onEvent(JSON.parse(m.data));
      })
      .catch(() => setDead(true));
    return () => es?.close();
  }, [onEvent]);

  useEffect(() => {
    if (stick.current) bottom.current?.scrollIntoView();
  }, [blocks]);

  const inputRef = useRef<HTMLTextAreaElement>(null);

  // grow with content, up to the CSS max-height
  const autosize = () => {
    const el = inputRef.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = el.scrollHeight + "px";
  };

  const submit = (e?: React.FormEvent) => {
    e?.preventDefault();
    const text = input.trim();
    if (!text) return;
    setInput("");
    requestAnimationFrame(autosize);
    fetch(`/api/input`, {
      method: "POST",
      headers: { Authorization: `Bearer ${token}` },
      body: JSON.stringify({ text }),
    });
  };

  const interrupt = () =>
    fetch(`/api/interrupt`, {
      method: "POST",
      headers: { Authorization: `Bearer ${token}` },
    });

  if (dead)
    return <div className="dead">🧌 cannot reach orc — is it still running? (check the URL token)</div>;

  return (
    <div className="app">
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
      <form onSubmit={submit}>
        <div className="bar">
          <span className={busy ? "prompt busy" : "prompt"}>{busy ? "⠋" : ">"}</span>
          <textarea
            ref={inputRef}
            rows={1}
            value={input}
            onChange={(e) => {
              setInput(e.target.value);
              autosize();
            }}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey) {
                e.preventDefault();
                submit();
              } else if (e.key === "Escape" && busy) {
                interrupt();
              }
            }}
            placeholder={busy ? "queue a message… (Esc interrupts)" : "message orc"}
            autoFocus
          />
          {busy && (
            <button type="button" onClick={interrupt}>
              stop
            </button>
          )}
        </div>
      </form>
      <footer>
        <div>{status}</div>
      </footer>
    </div>
  );
}
