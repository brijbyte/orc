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
- To update or restart orc, run
  `sudo systemctl restart --no-block orc-update.service` and return. Never wait
  for that unit or run `orc service install --graceful` from an agent turn.
- For a browser, start one on a free port and connect the global
  `playwright-core` to it; kill it when done. Do not `launch()`, and do not
  install browsers.

  ```sh
  browser serve --port 9222 --stealth &   # pick a free port
  ```

  ```js
  const b = await chromium.connectOverCDP("http://127.0.0.1:9222"); // or playwright
  ```
