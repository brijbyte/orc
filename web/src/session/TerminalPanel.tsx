import { useEffect, useRef, useState } from "react";
import { FitAddon } from "@xterm/addon-fit";
import { Terminal, type ITheme } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";
import { RotateCw, X } from "lucide-react";
import terminalFontURL from "../assets/MesloLGSNerdFontMono-Regular.woff2?url";
import { api } from "../lib/api";
import { Button } from "../ui/Button";
import s from "./TerminalPanel.module.css";

type Connection = "connecting" | "connected" | "closed";

const terminalFontName = "Orc Terminal Mono";
const terminalFontFamily = `"${terminalFontName}"`;
let terminalFontReady: Promise<void> | null = null;

function loadTerminalFont(): Promise<void> {
  if (terminalFontReady) return terminalFontReady;
  const font = new FontFace(terminalFontName, `url(${terminalFontURL})`);
  document.fonts.add(font);
  return (terminalFontReady = font.load().then(
    () => undefined,
    () => {
      terminalFontReady = null;
    },
  ));
}

function theme(): ITheme {
  const styles = getComputedStyle(document.documentElement);
  const color = (name: string) => styles.getPropertyValue(name).trim();
  return {
    background: color("--code-bg"),
    foreground: color("--code-fg"),
    cursor: color("--cyan"),
    selectionBackground: `color-mix(in srgb, ${color("--cyan")} 30%, transparent)`,
    black: color("--bg"),
    red: color("--red"),
    green: color("--green"),
    yellow: "#e5c07b",
    blue: "#61afef",
    magenta: "#c678dd",
    cyan: color("--cyan"),
    white: color("--fg"),
    brightBlack: color("--dim"),
    brightRed: color("--red"),
    brightGreen: color("--green"),
    brightYellow: "#e5c07b",
    brightBlue: "#61afef",
    brightMagenta: "#c678dd",
    brightCyan: color("--cyan"),
    brightWhite: color("--fg"),
  };
}

export function TerminalPanel({
  sid,
  open,
  onClose,
}: {
  sid: string;
  open: boolean;
  onClose: () => void;
}) {
  const host = useRef<HTMLDivElement>(null);
  const closeRef = useRef(onClose);
  closeRef.current = onClose;
  const [connection, setConnection] = useState<Connection>("connecting");
  const [generation, setGeneration] = useState(0);

  useEffect(() => {
    if (!open || !host.current) return;
    const element = host.current;
    let disposed = false;
    let stop = () => {};
    setConnection("connecting");

    void loadTerminalFont().then(() => {
      if (disposed) return;
      const terminal = new Terminal({
        cursorBlink: true,
        fontFamily: terminalFontFamily,
        fontSize: 13,
        lineHeight: 1.2,
        scrollback: 10_000,
        theme: theme(),
      });
      const fit = new FitAddon();
      terminal.loadAddon(fit);
      terminal.open(element);

      const socket = api.terminal(sid);
      socket.binaryType = "arraybuffer";
      const resize = () => {
        try {
          fit.fit();
        } catch {
          return;
        }
        if (socket.readyState === socket.OPEN)
          socket.send(
            JSON.stringify({
              type: "resize",
              cols: terminal.cols,
              rows: terminal.rows,
            }),
          );
      };
      const observer = new ResizeObserver(resize);
      observer.observe(element);
      const themeObserver = new MutationObserver(() => {
        terminal.options.theme = theme();
      });
      themeObserver.observe(document.documentElement, {
        attributes: true,
        attributeFilter: ["data-theme"],
      });
      const input = terminal.onData((data) => {
        if (socket.readyState === socket.OPEN)
          socket.send(JSON.stringify({ type: "input", data }));
      });
      socket.onopen = () => {
        setConnection("connected");
        resize();
        terminal.focus();
      };
      socket.onmessage = (event) => {
        if (event.data instanceof ArrayBuffer)
          terminal.write(new Uint8Array(event.data));
        else if (typeof event.data === "string") terminal.write(event.data);
      };
      socket.onclose = (event) => {
        setConnection("closed");
        if (!disposed && event.code === 1000 && event.reason === "shell exited")
          closeRef.current();
      };
      socket.onerror = () => setConnection("closed");
      requestAnimationFrame(resize);

      stop = () => {
        observer.disconnect();
        themeObserver.disconnect();
        input.dispose();
        socket.close();
        terminal.dispose();
      };
    });

    return () => {
      disposed = true;
      stop();
    };
  }, [sid, open, generation]);

  if (!open) return null;
  return (
    <section className={s.panel} aria-label="terminal">
      <header className={s.head}>
        <span className={s.title}>terminal</span>
        <span className={s.state} data-status={connection}>
          {connection}
        </span>
        <Button
          icon
          small
          tip="restart terminal"
          onClick={() => setGeneration((value) => value + 1)}
        >
          <RotateCw size={13} strokeWidth={1.8} aria-hidden />
        </Button>
        <Button icon small tip="close terminal" onClick={onClose}>
          <X size={14} strokeWidth={1.8} aria-hidden />
        </Button>
      </header>
      <div className={s.terminal} ref={host} />
    </section>
  );
}
