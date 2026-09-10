#!/usr/bin/env bash
# AGL i18n gate: zero-tolerance checks of the localization contract (task 6.7 doctrine).
# Usage: bash scripts/i18n_gate.sh
set -uo pipefail
rc=0
ok()  { echo "OK:   $1"; }
bad() { echo "FAIL: $1"; rc=1; }
expect_empty() {
  local desc="$1"; shift
  local out
  if out=$(grep -rn "$@" 2>/dev/null); then bad "$desc"; echo "$out"; else ok "$desc"; fi
}
# 1. Ловушка двойного форматирования вокруг TrEvent/TrEventf
expect_empty "no double Sprintf around TrEvent/TrEventf" "fmt.Sprintf(i18n.TrEvent" tui internal main.go
# 2. Legacy-хелперы настроек удалены
expect_empty "no legacy settings helpers" "GetSettingsStatusText\|GetSettingsGroupHeader\|FormatTuiHelpText" tui internal main.go i18n/types.go
# 3. Алиасы Translation/Languages не используются
expect_empty "no alias usage (Translation/Languages)" "i18n.Translation\|i18n.Languages" tui internal main.go
# 4. Ручных переключателей isEn в tui нет
expect_empty "no isEn switches in tui" "isEn" tui
# 5. LabelRu/LabelEn в tui нет
expect_empty "no LabelRu/LabelEn in tui" "LabelRu\b\|LabelEn\b" tui
# 6. Сборка / вет / тесты по всем пакетам
if go build ./...; then ok "go build ./..."; else bad "go build ./..."; fi
if go vet ./...; then ok "go vet ./..."; else bad "go vet ./..."; fi
if go test ./...; then ok "go test ./..."; else bad "go test ./..."; fi
# 7. gofmt: диагностика без блокировки (решение по дрейфу примем отдельно)
fmt_out=$(gofmt -l . 2>/dev/null)
if [ -n "$fmt_out" ]; then echo "INFO: gofmt drift (decision needed, gate not failed):"; echo "$fmt_out"; else ok "gofmt clean"; fi
exit $rc