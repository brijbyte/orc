export function modShortcut(event: KeyboardEvent, key: string): boolean {
  return (
    !event.repeat &&
    !event.altKey &&
    !event.shiftKey &&
    (event.metaKey || event.ctrlKey) &&
    event.key.toLowerCase() === key
  );
}

export function overlayOpen(): boolean {
  return [
    ...document.querySelectorAll<HTMLElement>(
      '[role="dialog"],[role="alertdialog"],[role="listbox"]',
    ),
  ].some((element) => element.checkVisibility());
}
