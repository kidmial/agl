#!/usr/bin/env bash
# ============================================================================
# install.sh — установщик Amber Guard Laboratory ИЗ ЛОКАЛЬНОЙ ДИРЕКТОРИИ.
#
# PURPOSE:
#   «Для ленивых админов»: ноль сети, ноль аргументов, ноль токенов.
#   Директория уже является полным дистрибутивом:
#   agl + agl.minisig + checksums.txt + install.sh.
#   Админ: sudo bash install.sh — и всё.
#
# CONTRACT:
#   - Файлы берутся из собственной директории скрипта (самолокализация).
#   - checksums.txt ОБЯЗАТЕЛЕН: отсутствие/несовпадение = abort.
#   - agl.minisig ОБЯЗАТЕЛЕН.
#   - Если minisign установлен: подпись проверяется встроенным ключом.
#     Невалидная подпись = установка строго прерывается с предупреждением.
#   - Если minisign не установлен: установка возможна только после явного
#     согласия администратора на установку без криптографической проверки.
#   - До установки: sha256 + проверка ELF magic и e_machine x86-64.
#   - Подмена через rename-swap: без ETXTBSY, демоны не останавливаются.
#   - Прежний бинарник принимает вид agl.old (одно поколение отката).
#   - После установки пишется маркер sha256 в states/.
#   - Инсталлятор не создаёт/не включает/не останавливает юниты; перезапускает
#     только активные для загрузки нового кода.
#
# SIDE-EFFECTS:
#   - Подменяет /opt/amberguard/agl; рестарт agld_tarpit/agld_watcher
#     (если активны).
# ============================================================================
#
# ============================================================================
# install.sh — Amber Guard Laboratory installer FROM THE LOCAL DIRECTORY.
#
# PURPOSE:
#   "For lazy admins": zero network, zero arguments, zero tokens.
#   The directory is already a complete distribution:
#   agl + agl.minisig + checksums.txt + install.sh.
#   Admin: sudo bash install.sh — that's all.
#
# CONTRACT:
#   - Files are taken from the script's own directory (self-localization).
#   - checksums.txt is REQUIRED: missing/mismatch = abort.
#   - agl.minisig is REQUIRED.
#   - If minisign is installed: the signature is verified with an embedded key.
#     Invalid signature = installation is strictly aborted with a warning.
#   - If minisign is not installed: installation is possible only after explicit
#     administrator consent to install without cryptographic verification.
#   - Before installation: sha256 + ELF magic and e_machine x86-64 check.
#   - Replacement via rename-swap: no ETXTBSY, daemons are not stopped.
#   - The previous binary becomes agl.old (one rollback generation).
#   - After installation, a sha256 marker is written to states/.
#   - The installer does not create/enable/stop units; it restarts
#     only active ones to load the new code.
#
# SIDE-EFFECTS:
#   - Replaces /opt/amberguard/agl; restarts agld_tarpit/agld_watcher
#     (if active).
# ============================================================================

set -euo pipefail

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SRC_BIN="$DIR/agl"
SRC_SUM="$DIR/checksums.txt"
SRC_SIG="$DIR/agl.minisig"
DST_DIR="/opt/amberguard"
DST_BIN="$DST_DIR/agl"
OLD_BIN="$DST_DIR/agl.old"
HASH_MARKER="/var/lib/amberguard/states/agl_binary.sha256"
UNITS=(agld_tarpit.service agld_watcher.service)
DIST_VER="$(cat "$DIR/VERSION" 2>/dev/null || echo 'unknown')"

# === Цвета и форматирование вывода ===
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\e[36m'
BOLD='\033[1m'
NC='\033[0m'

# Logo and Run Command
ART_TEXT=$(cat <<'EOF'
               #######          ################   #####           
              #########         ################   #####           
             ##### #####        #####              #####           
            #####   #####       #####              #####           
           #####     #####      #####              #####           
          #####       #####     #####  ##########  #####           
         ###################    #####  ##########  #####           
        #####################   #####        ####  #####           
       #####             #####  #################  ################
       ####               ####   ###############   ################
EOF
)

info() {
    printf "${BLUE}[INFO]${NC} %s\n" "$1"
    if [[ -n "${2:-}" ]]; then
        printf "${BLUE}       %s${NC}\n" "$2"
    fi
}

ok() {
    printf "${GREEN}[ OK ]${NC} %s\n" "$1"
    if [[ -n "${2:-}" ]]; then
        printf "${GREEN}       %s${NC}\n" "$2"
    fi
}

warn() {
    printf "${YELLOW}[WARN]${NC} %s\n" "$1"
    if [[ -n "${2:-}" ]]; then
        printf "${YELLOW}       %s${NC}\n" "$2"
    fi
}

err() {
    printf "${RED}[ERR ]${NC} %s\n" "$1" >&2
    if [[ -n "${2:-}" ]]; then
        printf "${RED}       %s${NC}\n" "$2" >&2
    fi
}

die() {
    printf "${RED}${BOLD}[FATAL]${NC} %s\n" "$1" >&2
    if [[ -n "${2:-}" ]]; then
        printf "${RED}${BOLD}        %s${NC}\n" "$2" >&2
    fi
    exit 1
}

red_die() {
    printf "${RED}${BOLD}[FATAL]${NC} %s\n" "$1" >&2
    if [[ -n "${2:-}" ]]; then
        printf "${RED}${BOLD}        %s${NC}\n" "$2" >&2
    fi
    exit 1
}

confirm_unverified() {
    local reason="$1"
    local reason_en="${2:-}"

    printf "\n"

    warn "$reason" "$reason_en"
    warn "Криптографическая проверка подписи невозможна." \
         "Cryptographic signature verification is impossible."
    info "Рекомендуется установить minisign и повторить установку." \
         "It is recommended to install minisign and repeat the installation."

    printf "\n"

    if [[ "${AGL_ALLOW_UNVERIFIED_INSTALL:-0}" == "1" ]]; then
        warn "Получено явное разрешение через AGL_ALLOW_UNVERIFIED_INSTALL=1." \
             "Explicit permission received via AGL_ALLOW_UNVERIFIED_INSTALL=1."
        warn "Установка будет продолжена без криптографической проверки подписи." \
             "Installation will continue without cryptographic signature verification."
        return 0
    fi

    if [[ ! -t 0 ]]; then
        die "Неинтерактивный режим: подтверждение невозможно. Если вы осознаёте риск, задайте AGL_ALLOW_UNVERIFIED_INSTALL=1." \
            "Non-interactive mode: confirmation is impossible. If you accept the risk, set AGL_ALLOW_UNVERIFIED_INSTALL=1."
    fi

    local answer=""

    printf "%s\n" "Вы хотите продолжить установку без криптографической проверки подписи? [НЕТ/да]"
    printf "%s\n" "Do you want to continue installation without cryptographic signature verification? [NO/yes]"

    read -r -p "> " answer || answer=""
    answer="${answer,,}"

    if [[ "$answer" == "да" || "$answer" == "yes" || "$answer" == "y" ]]; then
        warn "Администратор подтвердил установку без криптографической проверки подписи." \
             "Administrator confirmed installation without cryptographic signature verification."
        return 0
    fi

    die "Установка прервана администратором: нет явного согласия на установку без криптографической проверки." \
        "Installation aborted by administrator: no explicit consent for installation without cryptographic verification."
}

# === КОНТУР 0: root и минимальные зависимости (сетевых утилит НЕТ вовсе) ===
if [[ $EUID -ne 0 ]]; then
    die "Нужны права root: sudo bash install.sh" \
        "Root privileges required: sudo bash install.sh"
fi

for dep in sha256sum systemctl install od awk mktemp; do
    command -v "$dep" >/dev/null 2>&1 || die "Отсутствует зависимость: $dep" \
                                                   "Missing dependency: $dep"
done

# === КОНТУР 0.5: Pre-flight check UFW ===
if ! command -v ufw >/dev/null 2>&1; then
    die "КРИТИЧЕСКАЯ ОШИБКА: ufw не установлен. Комплекс Amber Guard Laboratory требует ufw. Установите его (apt install ufw) и настройте ПЕРЕД запуском инсталлятора." \
        "CRITICAL ERROR: ufw is not installed. Amber Guard Laboratory requires ufw. Install it (apt install ufw) and configure it BEFORE running the installer."
fi

# Принудительно используем английскую локаль для предсказуемого парсинга ufw
UFW_STATUS=$(LC_ALL=C ufw status verbose 2>/dev/null | grep -i "Status:" | awk '{print $2}' || echo "unknown")

if [[ "$UFW_STATUS" != "active" ]]; then
    warn "ВНИМАНИЕ: UFW не активен (Статус: $UFW_STATUS)." \
         "WARNING: UFW is not active (Status: $UFW_STATUS)."
    info "Комплекс критически зависит от работы ufw для изоляции ловушек." \
         "The complex critically depends on ufw for trap isolation."
    info "Пожалуйста, активируйте ufw и настройте правила ПЕРЕД продолжением:" \
         "Please enable ufw and configure rules BEFORE continuing:"

    printf "\n"

    info "  Активация файрволл: sudo ufw enable" \
         "  Enable firewall: sudo ufw enable"
    info "  Создание правила доступа: sudo ufw allow ssh   # или sudo ufw allow <номер_порта>/tcp" \
         "  Create access rule: sudo ufw allow ssh   # or sudo ufw allow <port_number>/tcp"

    printf "\n"

    die "Установка прервана: ufw не активен. Настройте его и запустите инсталлятор снова." \
        "Installation aborted: ufw is not active. Configure it and run the installer again."
fi

# UFW активен. Проверка блокировок.
SSH_PORT=""
if [[ -n "${SSH_CONNECTION:-}" ]]; then
    SSH_PORT=$(echo "$SSH_CONNECTION" | awk '{print $4}')
fi

# Фоллбек: читаем из sshd_config.
# ВАЖНО: добавляем || true чтобы избежать silent exit из-за set -e, если grep ничего не нашел.
if [[ -z "$SSH_PORT" || "$SSH_PORT" == "0" ]]; then
    if [[ -f /etc/ssh/sshd_config ]]; then
        SSH_PORT=$(grep -iE "^\s*Port\s+" /etc/ssh/sshd_config 2>/dev/null | awk '{print $2}' | head -n 1 || true)
    fi
fi

if [[ -z "$SSH_PORT" ]]; then
    SSH_PORT="22"
fi

info "Проверяю правила UFW для текущего SSH-подключения (порт $SSH_PORT)..." \
     "Checking UFW rules for the current SSH connection (port $SSH_PORT)..."

# || true защищает от падения скрипта, если ufw status вернет ошибку
UFW_RULES=$(LC_ALL=C ufw status 2>/dev/null || true)

if ! echo "$UFW_RULES" | grep -qE "^$SSH_PORT(/tcp)?\s+ALLOW"; then
    printf "\n"

    err "КРИТИЧЕСКОЕ ПРЕДУПРЕЖДЕНИЕ БЕЗОПАСНОСТИ" \
        "CRITICAL SECURITY WARNING"
    err "UFW активен, но порт SSH ($SSH_PORT) НЕ разрешен в правилах!" \
        "UFW is active, but SSH port ($SSH_PORT) is NOT allowed in the rules!"
    err "Если вы сейчас подключены по SSH и продолжите установку," \
        "If you are currently connected via SSH and continue installation,"
    err "после перезагрузки вы МОЖЕТЕ ПОТЕРЯТЬ ДОСТУП К СЕРВЕРУ НАВСЕГДА." \
        "after reboot you MAY LOSE ACCESS TO THE SERVER PERMANENTLY."

    printf "\n"

    warn "Немедленно откройте порт в UFW в ДРУГОМ терминале:" \
         "Immediately allow the port in UFW in ANOTHER terminal:"
    info "  sudo ufw allow $SSH_PORT/tcp"

    printf "\n"

    if [[ -t 0 ]]; then
        printf "%s\n" "Вы уверены, что хотите продолжить, понимая риск блокировки SSH? [НЕТ/да]"
        printf "%s\n" "Are you sure you want to continue, understanding the risk of SSH lockout? [NO/yes]"

        read -r -p "> " confirm_ufw || confirm_ufw=""

        if [[ "${confirm_ufw,,}" != "да" && "${confirm_ufw,,}" != "yes" ]]; then
            die "Установка прервана: риск потери доступа к серверу." \
                "Installation aborted: risk of losing access to the server."
        fi

        warn "Администратор принял риск. Установка продолжается..." \
             "Administrator accepted the risk. Installation continues..."
    else
        die "Неинтерактивный режим: установка прервана. Откройте SSH-порт в ufw." \
            "Non-interactive mode: installation aborted. Allow the SSH port in ufw."
    fi
else
    ok "UFW активен. Текущий SSH-порт ($SSH_PORT) разрешен." \
       "UFW is active. Current SSH port ($SSH_PORT) is allowed."
fi

# === КОНТУР 1: комплектность дистрибутива ===
if [[ ! -f "$SRC_BIN" ]]; then
    die "В директории установки отсутствует бинарник agl ($DIR)." \
        "The agl binary is missing in the installation directory ($DIR)."
fi

if [[ ! -f "$SRC_SUM" ]]; then
    die "В директории установки отсутствует checksums.txt: установка прервана (fail closed)." \
        "checksums.txt is missing in the installation directory: installation aborted (fail closed)."
fi

if [[ ! -f "$SRC_SIG" ]]; then
    die "В директории установки отсутствует agl.minisig: установка прервана (fail closed)." \
        "agl.minisig is missing in the installation directory: installation aborted (fail closed)."
fi

# === КОНТУР 2: криптографическая проверка подлинности бинарника ===
SIGNATURE_VERIFIED=0

if command -v minisign >/dev/null 2>&1; then
    PINNED_PUB="$(mktemp)" || die "Не удалось создать временный файл для публичного ключа." \
                                   "Failed to create a temporary file for the public key."

    trap 'rm -f "$PINNED_PUB"' EXIT

    cat > "$PINNED_PUB" <<'MINISIGN_PUB'
untrusted comment: minisign public key AA32588499B70262
RWRiAreZhFgyqn6xShxvsZu5p03tdZa8yf8Z3dWVwDjMWyJKQjWBZECR
MINISIGN_PUB

    chmod 0600 "$PINNED_PUB"

    info "Проверка криптографической подписи бинарника..." \
         "Verifying the cryptographic signature of the binary..."

    if minisign -V -m "$SRC_BIN" -x "$SRC_SIG" -p "$PINNED_PUB"; then
        ok "Подпись minisign подтверждена." \
           "minisign signature verified."
        SIGNATURE_VERIFIED=1
    else
        red_die "Подпись minisign не прошла проверку. Установка прервана." \
                "minisign signature verification failed. Installation aborted."
    fi
else
    confirm_unverified "minisign не найден." \
                       "minisign not found."
    warn "Криптографическая подпись бинарника не проверялась." \
         "The cryptographic signature of the binary was not verified."
fi

# === КОНТУР 3: sha256 ДО любых касаний системы ===
if ! ( cd "$DIR" && awk '$2=="agl"' checksums.txt | sha256sum -c - ); then
    die "Несовпадение sha256: файл повреждён или подменён. Установка прервана." \
        "sha256 mismatch: file is corrupted or tampered with. Installation aborted."
fi

ok "sha256 подтверждён." \
   "sha256 verified."

# === КОНТУР 4: ELF magic + e_machine x86-64 (чужая архитектура не встанет) ===
magic="$(od -An -tx1 -N 6 "$SRC_BIN" | tr -d ' \n')"
if [[ "$magic" != "7f454c460201" ]]; then
    die "agl не является ELF x86-64 (magic=$magic). Установка прервана." \
        "agl is not an x86-64 ELF binary (magic=$magic). Installation aborted."
fi

machine="$(od -An -tx1 -j18 -N2 "$SRC_BIN" | tr -d ' \n')"
if [[ "$machine" != "3e00" ]]; then
    die "agl не является x86-64 ELF (e_machine=$machine). Установка прервана." \
        "agl is not an x86-64 ELF binary (e_machine=$machine). Installation aborted."
fi

ok "ELF x86-64 подтверждён." \
   "ELF x86-64 confirmed."

# === КОНТУР 5: атомарная подмена (rename-swap, обход ETXTBSY) ===
mkdir -p "$DST_DIR"

OLD_VER="(отсутствовал / absent)"
if [[ -x "$DST_BIN" ]]; then
    OLD_VER="$("$DST_BIN" --version 2>/dev/null || echo '(битый / broken)')"
fi

if [[ -f "$DST_BIN" ]]; then
    mv -f "$DST_BIN" "$OLD_BIN"
fi

if ! install -m 0755 "$SRC_BIN" "$DST_BIN"; then
    if [[ -f "$OLD_BIN" ]]; then
        mv -f "$OLD_BIN" "$DST_BIN"
    fi

    die "Сбой записи нового бинарника — выполнен откат." \
        "Failed to write the new binary — rollback performed."
fi

# === КОНТУР 6: sanity нового бинарника + маркер хэша ===
NEW_VER="$("$DST_BIN" --version 2>/dev/null || true)"

if [[ -z "$NEW_VER" ]]; then
    if [[ -f "$OLD_BIN" ]]; then
        mv -f "$OLD_BIN" "$DST_BIN"
    fi

    die "Новый бинарник не исполняется — выполнен откат." \
        "The new binary does not execute — rollback performed."
fi

mkdir -p "$(dirname "$HASH_MARKER")"
( cd "$DST_DIR" && sha256sum agl > "$HASH_MARKER" )

ok "Установлен: $NEW_VER (было: $OLD_VER)" \
   "Installed: $NEW_VER (previous: $OLD_VER)"

# Перекрёстная сверка версии дистрибутива и установленного бинарника:
# VERSION из тарболла обязан содержаться в строке --version бинарника.
if [[ "$DIST_VER" != "unknown" && "$NEW_VER" != *"$DIST_VER"* ]]; then
    warn "Версия дистрибутива ($DIST_VER) не совпадает с версией бинарника ($NEW_VER)." \
         "Distribution version ($DIST_VER) does not match binary version ($NEW_VER)."
fi

# === КОНТУР 7: перезапуск ТОЛЬКО бегущих юнитов (ДНК: жизненным циклом
# юнитов владеет комплекс; инсталлятор лишь обновляет код в ОЗУ) ===
for u in "${UNITS[@]}"; do
    if systemctl is-active --quiet "$u"; then
        if systemctl restart "$u"; then
            ok "Бегущий юнит перезапущен на новый код: $u" \
               "Running unit restarted with new code: $u"
        else
            warn "Сбой рестарта $u — демон может держать СТАРЫЙ код!" \
                 "Failed to restart $u — daemon may still hold OLD code!"
        fi
    else
        info "Юнит $u не активен — комплекс запустит самостоятельно." \
             "Unit $u is not active — the complex will start it itself."
    fi
done

# === КОНТУР 8: Создание глобальной ссылки для вызова комплекса из консоли одной командой ===
info "Обеспечение глобального доступа к бинарнику AGL..." \
     "Providing global access to the AGL binary..."

# Используем -sf (force), чтобы безопасно перезаписывать линк при обновлениях
ln -sf /opt/amberguard/agl /usr/local/bin/agl

ok "Симлинк /usr/local/bin/agl создан." \
   "Symlink /usr/local/bin/agl created."

cat <<'NOTE'
────────────────────────────────────────────────────────
                         Готово! / Done!

Напоминания / Reminders:

1. Закройте имеющиеся TUI-сессии (в ОЗУ исполняется старый код).
   Close existing TUI sessions (old code is still running in RAM).

2. Если включён agld_autoupdate.timer - бинарник подхватит самостоятельно.
   If agld_autoupdate.timer is enabled, the binary will be picked up automatically.

3. Быстрый возврат к прежней версии если потребуется:
   Quick rollback to the previous version if needed:
   sudo mv /opt/amberguard/agl.old /opt/amberguard/agl
   sudo systemctl restart agld_tarpit.service agld_watcher.service
────────────────────────────────────────────────────────
NOTE

if [[ "$SIGNATURE_VERIFIED" -eq 1 ]]; then
    ok "Установка завершена: $NEW_VER" \
       "Installation completed: $NEW_VER"
    ok "Криптографическая подпись подтверждена" \
       "Cryptographic signature verified"

    printf "\n"
    printf "${CYAN}${ART_TEXT}${NC}\n"
    printf "\n"
    printf "                                  |\n"
    printf "                                  v\n"
    printf "          Команда для запуска комплекса / Start command: ${GREEN}${BOLD}agl${NC}\n"
    printf "\n"
else
    warn "Установка завершена: $NEW_VER (без криптографической проверки подписи)." \
         "Installation completed: $NEW_VER (without cryptographic signature verification)."
fi