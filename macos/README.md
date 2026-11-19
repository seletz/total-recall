# macOS app (spike #317)

A native SwiftUI rebuild of the TUI. It reads the archive only through the
CLI (`total-recall … --json`, one subprocess per call); it never opens the
SQLite file.

| Target | What |
|---|---|
| `TotalRecallCore` | CLI client, decoded outputs, view models, line diff, project globs; no SwiftUI, tested headless |
| `TotalRecallUI` | Window, views, shot renderer |
| `TotalRecall` | The executable |

Tasks (macOS only, not part of the Linux CI):

```sh
mise run macos:test       # swift test against fixtures from the demo archive
mise run macos:build      # debug + release, ad-hoc signed TotalRecall.app
mise run macos:run:demo   # the app against the synthetic demo archive
mise run macos:shots      # screenshots of every shot of internal/screenshot/frames.go
mise run macos:fixtures   # re-record the test fixtures
```

`macos/scripts/demo-home.sh` lays out a synthetic home around `build/demo.db`
(config with the screenshot projects, hook block, MCP entry, one backup).
The screenshots are in [screenshots/](screenshots/README.md).
