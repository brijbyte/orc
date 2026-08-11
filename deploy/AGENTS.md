# Working on this VM

Disposable Hetzner VM, not backed up, rebuilt without warning. Anything worth
keeping must leave the box — push it, or put it in your reply to the owner.

- Git is configured: commits are authored as the bot identity already set
  in gitconfig (leave `user.*` alone), and `git push` over SSH works.
  Commit early and push once work is worth keeping — prefer a branch over
  `main`. There is no `gh`; for a PR, put the compare link in your reply:
  `https://github.com/<owner>/<repo>/compare/<base>...<branch>?expand=1`.
- The owner cannot reach localhost here. Expose a dev server with
  `sudo tailscale serve --bg --https=8443 http://127.0.0.1:<port>` and report
  the printed `https://…ts.net:8443` URL; turn it off with
  `sudo tailscale serve --https=8443 off` when done.
- `rg` (ripgrep) is installed; prefer it for search.
- The service you are running under lives on this box — do not kill orc
  processes or bind port 7777.
