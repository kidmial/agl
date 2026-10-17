## `CONTRIBUTING_en.md`

# Contributing to Amber Guard Laboratory

Thank you for your interest in the project! Before making changes, please read this guide.

### Contribution Workflow
1. Fork the repository.
2. Create a feature branch: `git checkout -b feature/my-feature`
3. Make changes along with tests.
4. Run mandatory gates:
   ```bash
   go build ./... && go vet ./... && go test ./...
   bash scripts/i18n_gate.sh
   gofmt -l .
   ```
5. Commit with a clear message (Conventional Commits recommended: `feat(scope): ...`, `fix(scope): ...`, `docs: ...`).
6. Create a Pull Request.

### Code Standards
- **Go 1.21+** (Linux only).
- All user-facing strings live in `i18n/` (no hardcoding); `TrEvent`/`TrEventf` are never wrapped in `fmt.Sprintf`.
- Errors are handled, never ignored.
- **Comments:** Russian for internal logic, English for public API (`// PURPOSE:`, `// CONTRACT:`, `// SIDE-EFFECTS:`); every significant block must have a header `// === ... ===`.
- External commands exclusively via `sys.RunTimeout` / `sys.RunTimeoutStreamed`; direct `exec.Command` in business logic is strictly forbidden.
- Config mutations exclusively via `config.Update()` (snapshot clone + atomic write); the published `config.C` is never mutated directly.
- Paths exclusively via generators/constants from `config/paths.go` (no hardcoded paths).
- **Layering:** `tui/` is strictly for display; business logic resides in `internal/service/` (bridge) and protocol packages (`internal/armor`, `internal/tarpit`, `internal/tarpitctl`, `internal/maintenance`, `internal/backup`, `internal/forensics`).
- State markers are created/deleted only by the daemon; clients write only declarations.
- Trap inventory transactions exclusively via `sys.WithInventoryTx()` (flock-locked).
- **Logging:** after `i18n.InitLanguage`, exclusively via `sys.LogAction` + machine event codes; raw `log.Println` is forbidden. Diagnostics prior to i18n initialization must use technical English outside of dictionaries.
- **Anti-spam in hot paths:** edge-triggered principle (one WARNING upon entering degradation, one SUCCESS upon recovery).
- **Error formatting:** `%w` → `fmt.Errorf(i18n.T.Field, args)`; without verbs → `fmt.Errorf("%s", i18n.T.Field)`; arguments without `%w` → `fmt.Errorf("%s", fmt.Sprintf(i18n.T.Field, args))`.

### Architectural Principles
- **Declarative management:** the client writes intent into the inventory, the daemon reconciles it to fact.
- **Three-factor status:** declaration (state) → daemon confirmation (status) → fact on disk (marker).
- **Fail closed:** failure equals a safe state, never an open one.
- **Uniqueness safeguards:** flock for daemon modes; the lock file is not deleted on exit.
- **POSIX Atomic Write:** tmp → fsync → rename for all disk commits.

### Testing
```bash
go test ./...
go build ./...
go vet ./...
bash scripts/i18n_gate.sh
gofmt -l .
```

### i18n Doctrine
Details: `docs/I18N-DOCTRINE.md`

- `UITranslation`: everything the human sees (screens, hints, progress).
- `LogTranslation.Events`: journal descriptions mapped to machine codes.
- Markup and geometry live in the code; dictionaries contain only translatable text.
- One module = one key prefix; formula: `View + Screen + Component + Purpose`.
- Journal event codes are semantic and not tied to specific modules.