// Theme preference: "system" follows the OS and tracks changes live; the
// resolved scheme lands on <html data-theme> for the CSS.
export type ThemePref = "system" | "light" | "dark";

const KEY = "orc-theme";
const mq = window.matchMedia("(prefers-color-scheme: dark)");

export function themePref(): ThemePref {
  const v = localStorage.getItem(KEY);
  return v === "light" || v === "dark" ? v : "system";
}

export function setThemePref(p: ThemePref) {
  localStorage.setItem(KEY, p);
  apply();
}

function apply() {
  const p = themePref();
  const dark = p === "dark" || (p === "system" && mq.matches);
  document.documentElement.dataset.theme = dark ? "dark" : "light";
}

mq.addEventListener("change", apply);
apply();
