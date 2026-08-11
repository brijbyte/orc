import { useEffect } from "react";

const faviconPath = "/favicon.svg";
let faviconFrames: Promise<string[]> | null = null;

function activityFavicons(): Promise<string[]> {
  return (faviconFrames ??= fetch(faviconPath)
    .then((response) => response.text())
    .then((svg) =>
      [-16, -8, 0, 8, 16, 8, 0, -8].map((angle) => {
        const frame = svg
          .replace(/(<svg[^>]*>)/, `$1<g transform="rotate(${angle} 32 32)">`)
          .replace("</svg>", "</g></svg>");
        return `data:image/svg+xml,${encodeURIComponent(frame)}`;
      }),
    ));
}

// Spin the favicon while any session is busy; honors reduced motion.
export function useActivityFavicon(busy: boolean) {
  useEffect(() => {
    const icon = document.querySelector<HTMLLinkElement>('link[rel="icon"]');
    if (!icon || !busy) {
      if (icon) icon.href = faviconPath;
      return;
    }
    const media = matchMedia("(prefers-reduced-motion: reduce)");
    let frames: string[] = [];
    let frame = 0;
    let timer = 0;
    let stopped = false;
    const start = () => {
      clearInterval(timer);
      if (!frames.length) return;
      if (media.matches) {
        icon.href = frames[4];
        return;
      }
      const show = () => {
        icon.href = frames[frame++ % frames.length];
      };
      show();
      timer = window.setInterval(show, 160);
    };
    media.addEventListener("change", start);
    activityFavicons().then((loaded) => {
      if (stopped) return;
      frames = loaded;
      start();
    });
    return () => {
      stopped = true;
      clearInterval(timer);
      media.removeEventListener("change", start);
      icon.href = faviconPath;
    };
  }, [busy]);
}
