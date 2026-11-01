# total-recall

Harness-neutral archive of coding-agent sessions (Claude Code first) in a local SQLite DB, Go CLI.

**Read `docs/design.md` first.** It is the spec: schema, CLI, config, hooks, redaction. Do not re-decide what it settles; if something in it is wrong or missing, say so in your report instead of silently diverging.

## Rules

- Go, static build: `CGO_ENABLED=0`. SQLite via `modernc.org/sqlite`. CLI via cobra.
- TDD: failing test first, then code. Table-driven tests. `go test ./...` and `go vet ./...` must pass.
- **Never put real transcripts, real session content, or anything from `~/.claude/projects` into the repo** (tests, fixtures, docs). Write small synthetic fixtures that mimic the format.
- Reading `~/.claude/projects` locally to learn the format is fine. Never write there.
- Code, identifiers, comments, commit messages: English. User docs under `docs/` (Zensical): German.
- Go doc comments on exported identifiers. Keep packages small, no global state except the harness registry.
- DB schema: migrations in `internal/store/schema.go` are forward-only. **Never edit an existing migration; only append a new one.** `TestMigrateFromEveryVersion` checks every historical version migrates with data intact; add sample rows there if a migration touches data.
- Commits: signed when 1Password signing works; unsigned commits are fine in this private repo (`git -c commit.gpgsign=false commit`). If SSH push fails, push over HTTPS with gh credentials.
- Never mention Claude/AI in commits, PRs, issues.
- Branch from `develop`, PR into `develop`. Conventional commit style (`feat(store): …`).

## Layout

See `docs/design.md`, section Architektur. Shared types are in `internal/model`, the adapter interface in `internal/harness`. Change them only if necessary and state why in the PR.

## Tasks

`mise run build | test | lint | docs:serve | docs:build`
