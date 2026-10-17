#!/usr/bin/env bash
# ============================================================================
# dist.sh — конвейер дистрибутива Amber Guard Laboratory.
# Одна команда: ./dist.sh v8.4.1
# Результат: dist/agl-install-v8.4.1/ (agl + install.sh + checksums.txt)
#            dist/agl-install-v8.4.1.tar.gz
# CONTRACT: версия обязательна (пиннинг); артефакт проверяется на ELF x86-64;
#           checksums.txt генерируется конвейером, руками не правится.
#           Бинарник и архив подписываются криптографически (minisign).
# ============================================================================
set -euo pipefail

VERSION="${1:-$(git describe --tags --abbrev=0 2>/dev/null || true)}"
if [[ -z "$VERSION" ]]; then
  echo "❌ Использование: ./dist.sh <версия>   Пример: ./dist.sh v8.4.1" >&2
  exit 1
fi

# === ПУТЬ К СЕКРЕТНОМУ КЛЮЧУ - minisign.key ===
SECRET_KEY_PATH="${MINISIGN_KEY_PATH:-${HOME}/Projects/private_ms_key/minisign.key}"

BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
BIN="agl-linux-amd64"
STAGE="dist/agl-install-$VERSION"

# === КОНТУР 1: сборка linux/amd64 с инъекцией версии и штампа ===
echo "→ Собираю $BIN ($VERSION, $BUILD_TIME)..."
GOOS=linux GOARCH=amd64 go build \
  -ldflags "-X agl/internal/buildinfo.Version=$VERSION -X agl/internal/buildinfo.BuildTime=$BUILD_TIME" \
  -o "$BIN" .

# === КОНТУР 2: sanity артефакта (ELF x86-64) ===
if ! file "$BIN" | grep -q "ELF.*x86-64"; then
  echo "❌ Артефакт не является ELF x86-64: $(file "$BIN")" >&2
  exit 1
fi
echo "✓ ELF x86-64 подтверждён."

# === КОНТУР 3: сборка директория-дистрибутива ===
if [[ ! -f install.sh ]]; then
  echo "❌ В корне репозитория нет install.sh — упаковывать нечего." >&2
  exit 1
fi
if [[ ! -f restore.sh ]]; then
  echo "❌ В корне репозитория нет restore.sh — комплект неполон." >&2
  exit 1
fi

# === КОНТУР 3.1: гарантия, что инсталлятор действительно несёт зашитый публичный ключ ===
EXPECTED_MINISIGN_PUBKEY="RWRiAreZhFgyqn6xShxvsZu5p03tdZa8yf8Z3dWVwDjMWyJKQjWBZECR"

if ! grep -Fq "$EXPECTED_MINISIGN_PUBKEY" install.sh; then
  echo "❌ install.sh не содержит ожидаемый публичный ключ." >&2
  echo "   Обновите install.sh перед сборкой релиза." >&2
  exit 1
fi

echo "✓ install.sh содержит ожидаемый публичный ключ."

rm -rf "$STAGE"
mkdir -p "$STAGE"
printf '%s\n' "$VERSION" > "$STAGE/VERSION"
install -m 0755 "$BIN" "$STAGE/agl"
install -m 0755 install.sh "$STAGE/install.sh"
install -m 0755 restore.sh "$STAGE/restore.sh"

# Генерация SHA-256 (целостность)
( cd "$STAGE" && sha256sum agl > checksums.txt )
echo "✓ Сгенерирован checksums.txt"

# === КОНТУР 3.5: Криптографическая подпись бинарника (подлинность) ===
if ! command -v minisign &> /dev/null; then
    echo " minisign не найден в PATH. Установите его (brew install minisign / apt install minisign)." >&2
    exit 1
fi
if [[ ! -f "$SECRET_KEY_PATH" ]]; then
    echo "❌ Секретный ключ не найден по пути: $SECRET_KEY_PATH" >&2
    echo "   Задайте путь через переменную окружения MINISIGN_KEY_PATH" >&2
    exit 1
fi

echo "→ Подписываю бинарник (потребуется пароль от ключа)..."
minisign -S -m "$STAGE/agl" -s "$SECRET_KEY_PATH"
echo "✓ Бинарник подписан: $STAGE/agl.minisig"

rm -f "$BIN"
echo "✓ Директория $STAGE собрана и подписана."

# === КОНТУР 4: архив и контрольная сумма архива ===
# Отключаем упаковку метаданных macOS (xattr, resource forks)
COPYFILE_DISABLE=1 tar --no-xattrs -czf "dist/agl-install-$VERSION.tar.gz" -C dist "agl-install-$VERSION"
echo "✓ Архив создан: dist/agl-install-$VERSION.tar.gz"

( cd dist && sha256sum "agl-install-$VERSION.tar.gz" > "agl-install-$VERSION.tar.gz.sha256" )
echo "✓ Контрольная сумма архива создана: dist/agl-install-$VERSION.tar.gz.sha256"

# === КОНТУР 4.5: Криптографическая подпись архива ===
echo "→ Подписываю архив (потребуется пароль от ключа)..."
minisign -S -m "dist/agl-install-$VERSION.tar.gz" -s "$SECRET_KEY_PATH"
echo "✓ Архив подписан: dist/agl-install-$VERSION.tar.gz.minisig"

echo ""
echo "✓ Релиз $VERSION успешно собран и подписан!"
echo "Артефакты для публикации:"
echo "  - $STAGE/ (директория с бинарником)"
echo "  - dist/agl-install-$VERSION.tar.gz"
echo "  - dist/agl-install-$VERSION.tar.gz.sha256"
echo "  - dist/agl-install-$VERSION.tar.gz.minisig"