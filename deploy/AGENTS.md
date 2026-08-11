# Working on this VM

Disposable VM, not backed up, rebuilt without warning. Anything worth
keeping must leave the box — push it, or put it in your reply to the owner.

- Commit early and push once work is worth keeping — prefer a branch over
  `main`. For a PR, put the compare link in your reply:
  `https://github.com/<owner>/<repo>/compare/<base>...<branch>?expand=1`.
- The owner cannot reach localhost here. Expose a dev server with
  `sudo tailscale serve --bg --https=8443 http://127.0.0.1:<port>` and report
  the printed `https://…ts.net:8443` URL; turn it off with
  `sudo tailscale serve --https=8443 off` when done.
- `rg` (ripgrep) is installed; prefer it for search.
