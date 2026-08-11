# Working on this VM

Disposable Hetzner VM, not backed up, rebuilt without warning. Anything worth
keeping must leave the box — push it, or put it in your reply to the owner.

- No git push credentials are provisioned. Commit locally; if a push is
  needed, say so in your reply instead of retrying.
- The owner cannot reach localhost here. Expose a dev server with
  `sudo tailscale serve --bg --https=8443 http://127.0.0.1:<port>` and report
  the printed `https://…ts.net:8443` URL; turn it off with
  `sudo tailscale serve --https=8443 off` when done.
- `rg` (ripgrep) is installed; prefer it for search.
- The orc service itself runs on this box (port 7777) — do not kill orc
  processes or touch `~/.config/orc`.
