// Debounced router revalidation: opening several tabs at once (page load
// restores every open session) coalesces into one sidebar refresh.
let fn = () => {};
let t = 0;

export function setRevalidator(f: () => void) {
  fn = f;
}

export function revalidateSoon() {
  clearTimeout(t);
  t = window.setTimeout(() => fn(), 150);
}
