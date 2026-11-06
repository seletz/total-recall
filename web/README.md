# Web UI of `total-recall serve`

Svelte 5 + Vite + TypeScript. `npm run build` writes `../internal/web/dist`,
which the Go binary embeds (`internal/web`); Node is needed at build time
only. Background, decisions and measurements: `docs/explanation/spike-webui.md`.

| Task | What |
|---|---|
| `mise run web:install` | `npm ci` with the pinned lockfile |
| `mise run web:build` | production build into `internal/web/dist`, then `mise run build` |
| `mise run web:check` | `svelte-check` and `vitest` |
| `mise run web:dev` | Vite dev server on http://127.0.0.1:5173 with `/api` proxied to `total-recall serve` |
| `mise run serve:demo` | the binary against the demo archive with token `demo` |

Development loop: `total-recall serve --token dev` in one terminal,
`mise run web:dev` in another, open http://127.0.0.1:5173/#token=dev.

Versions are exact in `package.json` (`.npmrc`: `save-exact`) and complete in
`package-lock.json`. No runtime dependency but Svelte; nothing is loaded from
the network at run time, and the server's content security policy would
block it.
