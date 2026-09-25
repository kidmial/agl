#!/usr/bin/env bash
# ============================================================================
# dist.sh — конвейер дистрибутива Amber Guard Laboratory.
# Одна команда: ./dist.sh v8.4.1
# Результат: dist/agl-install-v8.4.1/ (agl + install.sh + checksums.txt)
#            dist/agl-install-v8.4.1.tar.gz
# CONTRACT: версия обязательна (пиннинг); артефакт проверяется на ELF x86-64;
#           checksums.txt генерируется конвейером, руками не правится.
# ============================================================================
set -euo pipefail

VERSION="${1:-$(git describe --tags --abbrev=0 2>/dev/null || true)}"
if [[ -z "$VERSION" ]]; then
  echo "❌ Использование: ./dist.sh <версия>   Пример: ./dist.sh v8.4.1" >&2
  exit 1
fi

BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
BIN="agl-linux-amd64"
STAGE="dist/agl-install-$VERSION"

# === КОНТУР 1: сборка linux/amd64 с инъекцией версии и штампа ===
echo "→ Собираю $BIN ($VERSION, $BUILD_TIME)..."
GOOS=linux GOARCH=amd64 go build \
  -ldflags "-X agl/internal/buildinfo.Version=$VERSION -X agl/internal/buildinfo.BuildTime=$BUILD_TIME" \
  -o "$BIN" .

# === КОНТУР 2: sanity артефакта (ELF x86-64, как в make build-linux) ===
if ! file "$BIN" | grep -q "ELF.*x86-64"; then
  echo "❌ Артефакт не является ELF x86-64: $(file "$BIN")" >&2
  exit 1
fi
echo "✓ ELF x86-64 подтверждён."

# === КОНТУР 3: сборка папки-дистрибутива ===
if [[ ! -f install.sh ]]; then
	echo "❌ В корне репозитория нет install.sh — упаковывать нечего." >&2
	exit 1
fi
if [[ ! -f restore.sh ]]; then
	echo "❌ В корне репозитория нет restore.sh — комплект неполон." >&2
	exit 1
fi
rm -rf "$STAGE"
mkdir -p "$STAGE"
printf '%s\n' "$VERSION" > "$STAGE/VERSION"
install -m 0755 "$BIN" "$STAGE/agl"
install -m 0755 install.sh "$STAGE/install.sh"
install -m 0755 restore.sh "$STAGE/restore.sh"
( cd "$STAGE" && sha256sum agl > checksums.txt )
rm -f "$BIN"
echo "✓ Папка $STAGE собрана: agl + install.sh + restore.sh + checksums.txt + VERSION"

# === КОНТУР 4: архив и контрольная сумма архива (для релиз-ноты) ===
tar -czf "dist/agl-install-$VERSION.tar.gz" -C dist "agl-install-$VERSION"
echo "✓ Архив: dist/agl-install-$VERSION.tar.gz"
sha256sum "dist/agl-install-$VERSION.tar.gz"