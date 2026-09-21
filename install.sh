#!/usr/bin/env bash
# ============================================================================
# install.sh — установщик Amber Guard Laboratory ИЗ ЛОКАЛЬНОЙ ПАПКИ.
#
# PURPOSE:
#   «Для ленивых админов»: ноль сети, ноль аргументов, ноль токенов.
#   Папка сама есть дистрибутив: agl + checksums.txt + install.sh.
#   Админ: sudo ./install.sh — и всё.
#
# CONTRACT:
#   - Файлы берутся из собственной директории скрипта (самолокализация).
#   - checksums.txt ОБЯЗАТЕЛЕН: отсутствие/несовпадение = abort (fail closed).
#   - До установки: sha256 + проверка ELF magic и класса x86-64.
#   - Подмена через rename-swap: без ETXTBSY, демоны не останавливаются.
#   - Старый бинарник живёт как agl.old (одно поколение отката).
#   - После установки пишется маркер sha256 в states/ — фундамент
#     будущей самоцелостности бинарника (M6).
#   - Инсталлятор не создаёт/не включает/не останавливает юниты; перезапускает
#     только активные для загрузки нового кода
#
# SIDE-EFFECTS:
#   - Подменяет /opt/amberguard/agl; рестарт agld_tarpit/agld_watcher (если есть).
# ============================================================================
set -euo pipefail

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SRC_BIN="$DIR/agl"
SRC_SUM="$DIR/checksums.txt"
DST_DIR="/opt/amberguard"
DST_BIN="$DST_DIR/agl"
OLD_BIN="$DST_DIR/agl.old"
HASH_MARKER="/var/lib/amberguard/states/agl_binary.sha256"
UNITS=(agld_tarpit.service agld_watcher.service)

DIST_VER="$(cat "$DIR/VERSION" 2>/dev/null || echo 'unknown')"
# после КОНТУРА 5:
if [[ "$DIST_VER" != "unknown" && "$NEW_VER" != *"$DIST_VER"* ]]; then
  say "⚠️ Версия дистрибутива ($DIST_VER) не совпадает с версией бинарника ($NEW_VER)."
fi

say() { printf '%s\n' "$*"; }
die() { printf '❌ %s\n' "$*" >&2; exit 1; }

# === КОНТУР 0: root и минимальные зависимости (сетевых утилит НЕТ вовсе) ===
if [[ $EUID -ne 0 ]]; then die "Нужны права root: sudo ./install.sh"; fi
for dep in sha256sum systemctl install od; do
  command -v "$dep" >/dev/null 2>&1 || die "Отсутствует зависимость: $dep"
done

# === КОНТУР 1: комплектность дистрибутива ===
if [[ ! -f "$SRC_BIN" ]]; then die "Рядом со скриптом нет бинарника agl ($DIR)."; fi
if [[ ! -f "$SRC_SUM" ]]; then die "Рядом со скриптом нет checksums.txt: установка прервана (fail closed)."; fi

# === КОНТУР 2: sha256 ДО любых касаний системы ===
if ! ( cd "$DIR" && awk '$2=="agl"' checksums.txt | sha256sum -c - ); then
  die "Несовпадение sha256: файл повреждён или подменён. Установка прервана."
fi
say "✓ sha256 подтверждён."

# === КОНТУР 3: ELF magic + класс x86-64 (чужая архитектура не встанет) ===
magic="$(od -An -tx1 -N 6 "$SRC_BIN" | tr -d ' ')"
if [[ "$magic" != "7f454c460201" ]]; then
  die "agl не является ELF x86-64 (magic=$magic). Установка прервана."
fi
say "✓ ELF x86-64 подтверждён."

# === КОНТУР 4: атомарная подмена (rename-swap, обход ETXTBSY) ===
mkdir -p "$DST_DIR"
OLD_VER="(отсутствовал)"
if [[ -x "$DST_BIN" ]]; then
  OLD_VER="$("$DST_BIN" --version 2>/dev/null || echo '(битый)')"
fi
if [[ -f "$DST_BIN" ]]; then
  mv -f "$DST_BIN" "$OLD_BIN"
fi
if ! install -m 0755 "$SRC_BIN" "$DST_BIN"; then
  if [[ -f "$OLD_BIN" ]]; then mv -f "$OLD_BIN" "$DST_BIN"; fi
  die "Сбой записи нового бинарника — выполнен откат."
fi

# === КОНТУР 5: sanity нового бинарника + маркер хэша ===
NEW_VER="$("$DST_BIN" --version 2>/dev/null || true)"
if [[ -z "$NEW_VER" ]]; then
  if [[ -f "$OLD_BIN" ]]; then mv -f "$OLD_BIN" "$DST_BIN"; fi
  die "Новый бинарник не исполняется — выполнен откат."
fi
mkdir -p "$(dirname "$HASH_MARKER")"
( cd "$DST_DIR" && sha256sum agl > "$HASH_MARKER" )
say "✓ Установлен: $NEW_VER (было: $OLD_VER)"

# === КОНТУР 6: перезапуск ТОЛЬКО бегущих юнитов (ДНК: жизненным циклом
# юнитов владеет комплекс; инсталлятор лишь обновляет код в ОЗУ) ===
for u in "${UNITS[@]}"; do
  if systemctl is-active --quiet "$u"; then
    if systemctl restart "$u"; then
      say "✓ Бегущий юнит перезапущен на новый код: $u"
    else
      say "⚠️ Сбой рестарта $u — демон может держать СТАРЫЙ код!"
    fi
  else
    say "· Юнит $u не активен — не трогаем (ДНК: комплекс поднимает сам)."
  fi
done

cat <<'NOTE'
────────────────────────────────────────────────────────
 Готово. Напоминания:
 1. Закрой висящие TUI-сессии (держат старый код в ОЗУ).
 2. agld_autoupdate.timer (если включён) подхватит бинарник сам.
 3. Откат: sudo mv /opt/amberguard/agl.old /opt/amberguard/agl
    && sudo systemctl restart agld_tarpit.service agld_watcher.service
────────────────────────────────────────────────────────
NOTE
say "✓ Установка завершена: $NEW_VER"