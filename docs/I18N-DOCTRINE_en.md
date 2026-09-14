# AGL i18n Doctrine (task 6.7)

Status: active. Scope: every user-facing string of the complex.

## 1. Container boundary
- `UITranslation` (`i18n/lang_*_ui.go`): everything a human sees (TUI screens, progress
  windows, console hints, web-rescue page, settings matrix labels).
- `LogTranslation.Events` (`i18n/lang_*_logdescr.go`): human-readable journal
  descriptions keyed by machine event codes.
- Code only (never in dictionaries): identifiers, routing tags, termui markup
  (`(fg:...)`, `[[ ... ]]`), geometry (`%-Ns` widths, `│` separators, padding),
  filesystem paths, buildinfo/version, protocol verbs (ON/OFF), event codes.

## 2. Markup & geometry rule (auditor ruling)
- Table skeletons and markup live in code; dictionaries hold translatable prose only.
- Rationale: a translator mistake must neither crash the runtime nor break layout;
  RU/EN label lengths differ by design.
- Pre-doctrine foundation values that still carry markup inside (e.g.
  `CoreSysConsoleBody`) are frozen legacy; new work must not add markup to dictionaries.

## 3. Module isolation & key naming
- One source module = own key prefix; identical text is NOT reused across modules.
- Naming formula (auditor): `View + Screen + Component + Purpose`
  (example: `ViewTrapCardHintTitle`).
- Fields are grouped by source file with `// --- <file> ---` markers.

## 4. Event codes & journal contract (A.2)
- `LogAction`/`TrEvent`/`TrEventf`: code is the machine key; description is localized;
  dynamic data goes to a separate `details` field.
- Fallback: unknown code or empty template → return the machine code itself;
  the journal line is never empty (covered by `TestTrEventFallback`).
- Never wrap `TrEvent`/`TrEventf` in `fmt.Sprintf` (double-format trap);
  enforced by `scripts/i18n_gate.sh`.
- Journal event codes are semantic, not module-scoped: the same event logged
  by different modules shares ONE code; the source is identified by the
  COMPONENT/ACTION columns (example: `config.reload_failed` from
  TARPIT_MANAGER and from the watcher). Module isolation (§3) applies to
  UI strings, not to journal codes.
- Post-init journal notes go through `sys.LogAction` + event codes; raw
  `log.Println`/`log.Printf` with natural language is forbidden after
  `i18n.InitLanguage`. Boot-time diagnostics (before init) are exempt and
  written in technical English outside dictionaries (showAlertScreen,
  root check, logrotate warning).

## 5. Error formatting
- `%w` present → `fmt.Errorf(i18n.T.Field, args)`.
- No verbs → `fmt.Errorf("%s", i18n.T.Field)` (vet-safe constant format).
- Args without `%w` → `fmt.Errorf("%s", fmt.Sprintf(i18n.T.Field, args))`.

## 6. Title grammar (TD-6 convention; retroactive unification = WON'T-FIX)
- Monitoring screens: plain title `" Name "`.
- Modal / editor / transactional windows, and titles carrying metadata
  (`│`, port, tag): bracketed `" [ ... ] "`.
- Legacy inconsistency (ARMOR/CORE bracketed vs MAIN/TARPIT plain) is frozen by
  decision; all new screens follow the rule above.

## 7. Gates & hygiene
- `scripts/i18n_gate.sh`: zero-tolerance checks (no double Sprintf around TrEvent,
  no legacy settings helpers, no Translation/Languages aliases, no `isEn`,
  no `LabelRu`/`LabelEn`) + `go build/vet/test ./...` + gofmt diagnostics.
- `i18n/i18n_test.go`: RU/EN parity, non-empty values, SysctlDocs completeness,
  TrEvent/TrEventf fallback.
- `.gitignore`: build artifacts (`/agl`, `/amberguard/`).

## 8. Commit & history policy
- Conventional commits; one logical scope per commit.
- A commit may be amended only while it is the branch top; afterwards history is
  immutable and the truthful scope mapping lives in PR / release notes.