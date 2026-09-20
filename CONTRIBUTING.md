## `CONTRIBUTING_en.md`

# Contributing to Amber Guard Laboratory

Thanks for your interest in the project! Before making changes, please read this guide.

## Change process

1. Fork the repository
2. Create a feature branch: `git checkout -b feature/my-feature`
3. Make your changes with tests
4. Run the mandatory gates:
   ```bash
   go build ./... && go vet ./... && go test ./...
   bash scripts/i18n_gate.sh
   gofmt -l .
   ```
5. Commit with a clear message (Conventional Commits recommended: `feat(scope): ...`, `fix(scope): ...`, `docs: ...`)
6. Open a Pull Request

## Code standards

- Go 1.21+ (Linux only)
- All user-facing strings live in `i18n/` (no hardcode); `TrEvent`/`TrEventf` are never wrapped in `fmt.Sprintf`
- Errors are handled, never ignored
- Comments: Russian for internal logic, English for public API (`// PURPOSE:`, `// CONTRACT:`, `// SIDE-EFFECTS:`); every significant block gets a `// === ... ===` header
- External commands only via `sys.RunTimeout` / `sys.RunTimeoutStreamed`; direct `exec.Command` in business code is forbidden
- Config mutations only via `config.Update()` (snapshot clone + atomic write); never mutate the published `config.C` in place
- Paths only via generators/constants from `config/paths.go` (no hardcoded paths)
- Layering: `tui/` renders only; business logic lives in `internal/service/` (bridge) and protocol packages (`internal/armor`, `internal/tarpit`, `internal/tarpitctl`, `internal/maintenance`)
- State markers are created/removed only by the daemon; clients write declarations only

## Testing

```bash
go test ./...
go build ./...
go vet ./...
bash scripts/i18n_gate.sh
gofmt -l .
```
