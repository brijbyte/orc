import { useCallback, useEffect, useState, useSyncExternalStore } from "react";
import * as store from "./store";

// Open tabs survive a reload in this browser tab.
function loadOpen(): string[] {
  try {
    const t = JSON.parse(sessionStorage.getItem("orc-tabs") ?? "");
    if (Array.isArray(t.open)) return t.open;
  } catch {
    /* first visit */
  }
  return [];
}

export function useOpenTabs() {
  const [open, setOpen] = useState<string[]>(loadOpen);
  useEffect(() => {
    sessionStorage.setItem("orc-tabs", JSON.stringify({ open }));
  }, [open]);
  return [open, setOpen] as const;
}

// Narrow layout tracks the media query; the sidebar closes when it applies.
export function useNarrowSidebar() {
  const [narrow, setNarrow] = useState(
    () => matchMedia("(max-width: 48rem)").matches,
  );
  const [sideOpen, setSideOpen] = useState(
    () => !matchMedia("(max-width: 48rem)").matches,
  );
  useEffect(() => {
    const media = matchMedia("(max-width: 48rem)");
    const resize = () => {
      setNarrow(media.matches);
      setSideOpen(!media.matches);
    };
    media.addEventListener("change", resize);
    return () => media.removeEventListener("change", resize);
  }, []);
  return { narrow, sideOpen, setSideOpen };
}

// True while any open tab's session is busy.
export function useAnyBusy(open: string[]) {
  return useSyncExternalStore(
    useCallback(
      (notify) => {
        const stops = open.map((id) => store.subscribe(id, notify));
        return () => stops.forEach((stop) => stop());
      },
      [open],
    ),
    useCallback(() => open.some((id) => store.snapshot(id).busy), [open]),
  );
}
