#!/usr/bin/env bash
# ==================================================================================
# install.sh — установщик Amber Guard Laboratory ИЗ ЛОКАЛЬНОЙ ДИРЕКТОРИИ. v9.0.0
#
# PURPOSE:
#   Меню-цикл с РАЗДЕЛЕНИЕМ НАМЕРЕНИЯ И ИСПОЛНЕНИЯ:
#     1 — информация (что будет ПРИ установке и ПОСЛЕ неё);
#     2 — проверка SHA-256 (только проверка с возвратом в меню);
#     3 — управление UFW (3.1 осмотр статуса, 3.2 декларация периметра);
#     4 — проверка и установка РЕКОМЕНДУЕМЫХ обновлений (openssh-server, ufw);
#     5 — установка (явный план изменений + явное подтверждение);
#     99 — авто-режим (тихий служебный код, вне оглавления): последовательность
#          2→4→3.2→5 без внутренних подтверждений, контракт K1–K6.
#     0 — выход.
#   Никаких каскадов: проверка всегда проверяет и не означает согласие на установку.
#
# CONTRACT:
#   - Пункт 2 никогда не переходит в установку: только вердикт и возврат.
#   - Пункт 5 без UFW_READY=1 (ставится только пунктом 3.2) недоступен.
#   - Пункт 4 совещательный: офлайн или отказ не блокируют установку;
#     бут-топологию в любом случае повторно проверяет стадия 0 пункта 5.
#   - Пункт 5 без успешной проверки предлагает проверить ИЛИ осознанно пропустить;
#     проваленная проверка (SHA_STATE=fail) БЛОКИРУЕТ и установку, и пункт 3.2.
#
# SIDE-EFFECTS:
#   - Только пункты 3.2 и 5 и только после подтверждений: подмена /opt/amberguard/agl,
#     симлинк /usr/local/bin/agl, рестарт активных юнитов, возможно ufw enable.
# ==================================================================================

set -euo pipefail

# === Геометрия дистрибутива: всё из собственной директории ===
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SRC_BIN="$DIR/agl"
SRC_SUM="$DIR/checksums.txt"
DST_DIR="/opt/amberguard"
DST_BIN="$DST_DIR/agl"
OLD_BIN="$DST_DIR/agl.old"
UNITS=(agld_tarpit.service agld_watcher.service)
DIST_VER="$(cat "$DIR/VERSION" 2>/dev/null || echo unknown)"

LANG_MODE="ru"     # перезаписывается языковым гейтом
SHA_STATE="none"   # none | ok | skipped | fail — намерение проверки, не автоматика

# === Двуязычный вывод: единая точка истины ===
# %b вместо %s: сообщения могут нести цветовые теги \e[...m из палитры ниже.
# Форма ${2:-$1} не_tripwire'ит set -u: односторонние вызовы (билингвальная
# строка в одном аргументе) в EN-режиме печатают её же, а не падают.
say() { if [[ "$LANG_MODE" == "ru" ]]; then printf '%b\n' "$1"; else printf '%b\n' "${2:-$1}"; fi; }

# === КОНТУР 0: root и минимум зависимостей (до меню) ===
if [[ $EUID -ne 0 ]]; then
    printf '[ FATAL ] Нужны права root: sudo bash install.sh\nRoot privileges required: sudo bash install.sh\n' >&2
    exit 1
fi
for dep in sha256sum systemctl awk grep install tput apt; do
    command -v "$dep" >/dev/null 2>&1 || { printf '[ FATAL ] Нет зависимости: %s / Missing dependency: %s\n' "$dep" "$dep" >&2; exit 1; }
done

# === КОСМЕТИКА: цветовые теги (наследие инсталлятора v8, минимум палитры) ===
RED=$'\033[31m'      # теги ошибок и [ ОШИБКА ]
CYAN=$'\033[36m'     # стрелка-маркер исполнительных пар
GREEN=$'\033[32m'    # оглавление меню и теги ${GREEN}[ OK ]${NC}
YELLOW=$'\033[33m'   # индикация [ ОРИЕНТИР ], и [ WARNING ]
LCYAN=$'\033[96m'    # пункты меню
LWHITE=$'\033[97m'   # полосы оглавления
NC=$'\033[0m'

# ANSI-C кавычки: переменные держат СЫРОЙ байт ESC, а не текст "\e".
# После этого %s и %b печатают цвет одинаково, а обратные слэши в тексте
# админа остаются литеральными. Октал \033 совместим даже с bash 3.2 (macOS).

UFW_READY=0   # декларативный замок пункта 5: ставится ТОЛЬКО пунктом 3

# === Гигиена экрана: очистка и старт контента со второй строки ===
cls() { clear; printf '\n'; }

# === Исполнительная пара: намерение и подтверждённый факт отдельными строками ===
# КОНТРАКТ: каждая системная операция объявляется ДО и подтверждается ПОСЛЕ.
op()   { say "${CYAN}>${NC} ${YELLOW}[ ОПЕРАЦИЯ ]${NC} $1" "${CYAN}>${NC} ${YELLOW}[ OPERATION ]${NC} $2"; }
res()  { say "${CYAN}>${NC} ${GREEN}[ РЕЗУЛЬТАТ ]${NC} $1" "${CYAN}>${NC} ${GREEN}[ RESULT ]${NC} $2"; }
fail() { say "${CYAN}>${NC} ${RED}[ ОШИБКА ]${NC} $1" "${CYAN}>${NC} ${RED}[ ERROR ]${NC} $2"; }

# === Пауза после пункта меню: возврат по ENTER, никаких автокаскадов ===
# Пауза с адресуемым возвратом: по умолчанию главное меню, подменю передают своё.
pause() {
    printf '\n'
    say "${1:-${CYAN}[ENTER]${NC} Для возврата в основное меню}" "${2:-${CYAN}[ENTER]${NC} To return to the main menu}"
    read -r _ || true
}

# === ПЕЙДЖЕР: прокрутка длинного текста стрелками, выход по ENTER ===
# PURPOSE: админ видит текст ПОЦЕЛОМ: окно высотой в терминал, позиции в футере.
# CONTRACT: чистый bash, без less/more; ENTER всегда возвращает в вызвавшее меню;
#           стрелки: ESC[A / ESC[B. Одинокий ESC игнорируется через таймаут read.
# SIDE-EFFECTS: перерисовка экрана (clear) на каждое нажатие.
pager() {
    local -a lines=("$@")
    local total=${#lines[@]}
    local height
    height="$(tput lines 2>/dev/null || echo 20)"
    height=$(( height - 2 ))                 # две строки футера с подсказкой
    if [[ "$height" -lt 5 ]]; then height=5; fi
    local offset=0 key seq i end
    while true; do
        clear
        printf '\n'
        end=$(( offset + height ))
        if [[ "$end" -gt "$total" ]]; then end=$total; fi
        for (( i = offset; i < end; i++ )); do
            printf '%s\n' "${lines[$i]}"     # сырые ESC-байты тегов печатаются цветом
        done
        # Футер: подсказка + маркер позиции, чтобы начало текста не терялось.
        if [[ "$LANG_MODE" == "ru" ]]; then
            printf '%b\n' "${CYAN}↑/↓${NC} — прокрутка, ${CYAN}[ENTER]${NC} — назад   [ строки $(( offset + 1 ))–$end из $total ]"
        else
            printf '%b\n' "${CYAN}↑/↓${NC} — scroll, ${CYAN}[ENTER]${NC} — back   [ lines $(( offset + 1 ))–$end of $total ]"
        fi
        read -rsn1 key || key=""
        if [[ -z "$key" ]]; then
            return 0                         # ENTER — возврат в вызвавшее меню
        fi
        if [[ "$key" == $'\x1b' ]]; then
            read -rsn2 -t 1 seq || seq=""    # таймаут: одинокий ESC не вешает пейджер
            if [[ "$seq" == "[A" && "$offset" -gt 0 ]]; then
                offset=$(( offset - 1 ))
            elif [[ "$seq" == "[B" && "$end" -lt "$total" ]]; then
                offset=$(( offset + 1 ))
            fi
        fi
    done
}

# === ГЕЙТ: ЯЗЫК (разблокирует все остальные сообщения) ===
cls
printf 'Выберите язык / Select language: [ru/en] (по умолчанию ru): '
read -r lang || lang=""
case "${lang,,}" in
    en|eng|english) LANG_MODE="en" ;;
    *)              LANG_MODE="ru" ;;
esac

# === ПУНКТ 1: ИНФОРМАЦИЯ (текст напарника v9.0.0, пейджер со стрелками) ===
# НЕКОВЫРНЫЕ heredoc: цветовые теги раскрываются при чтении строк.
# ЗАПРЕТ внутри текста: символ $ и backtick без экранирования.
show_info() {
    local -a L=()
    if [[ "$LANG_MODE" == "ru" ]]; then
        while IFS= read -r l; do L+=("$l"); done <<RU

======================================================================
======================= AMBER GUARD LABORATORY =======================
======================================================================

                  УСТАНОВКА ИЗ ЛОКАЛЬНОЙ ДИРЕКТОРИИ

${LCYAN}Установка${NC}
- Установка комплекса AGL выполняется в директорию /opt/amberguard/
- Если исполняемый файл agl уже существует, произойдет его атомарная замена.

${LCYAN}Откат${NC}
- Для возврата к предыдущей версии используется команда:
  sudo mv /opt/amberguard/agl.old /opt/amberguard/agl

${LCYAN}Целостность${NC}
- Для ручной верификации предусмотрен файл цифровой подписи agl.minisig.
- Проверка валидности бинарника осуществляется при наличии утилиты 'minisign'.
- При отсутствии утилиты рекомендуется установить ее через системный менеджер
  пакетов (например, sudo apt install minisign).
- Готовая команда офлайн-проверки (ключ встроен одной копией):
    printf 'untrusted comment: agl pubkey\nRWRiAreZhFgyqn6xShxvsZu5p03tdZa8yf8Z3dWVwDjMWyJKQjWBZECR\n' > /tmp/agl.pub
    minisign -V -m agl -x agl.minisig -p /tmp/agl.pub

                                ${YELLOW}ВАЖНО${NC}

${LCYAN}Брандмауэр UFW${NC}
- Комплекс AGL использует встроенные сетевые возможности системы для управления ловушками.
- Активированный статус брандмауэра (UFW) необходим для штатной работы комплекса.
- Пункт 3.1: Проверка текущего статуса UFW.
- Пункт 3.2: Автоматическое создание разрешающих правил для текущей SSH-сессии и активация UFW.

${LCYAN}Обновления${NC}
- Пункт 4 обновляет только критичные для комплекса пакеты: openssh-server и ufw.
- Полные обновления системы — модуль автообновлений комплекса или ручной apt
- Служебный код 99 (вне оглавления меню): авто-режим, все решения одним выбором.
  В авто-режиме конфликты конфигов пакетов решаются в пользу локальной версии,
  поток исполнения полностью видим; проверка целостности обязательна.

${LCYAN}После установки${NC}
- Если вы обновляете уже установленный комплекс AGL, пожалуйста, завершите текущую TUI-сессию.
  Это критически важно для обновления исполняемого кода в оперативной памяти.

======================================================================
RU
    else
        while IFS= read -r l; do L+=("$l"); done <<EN

======================================================================
======================= AMBER GUARD LABORATORY =======================
======================================================================

                    INSTALL FROM LOCAL DIRECTORY

${LCYAN}Installation${NC}
- The AGL complex is installed into /opt/amberguard/
- If the agl binary already exists, it is replaced atomically.

${LCYAN}Rollback${NC}
- To return to the previous version use:
  sudo mv /opt/amberguard/agl.old /opt/amberguard/agl

${LCYAN}Integrity${NC}
- A digital signature file agl.minisig is provided for manual verification.
- Binary validity is checked when the 'minisign' utility is present.
- If missing, install it via the system package manager
  (e.g. sudo apt install minisign).
- Ready-to-use command for offline verification (the key is embedded as a single copy):
    printf 'untrusted comment: agl pubkey\nRWRiAreZhFgyqn6xShxvsZu5p03tdZa8yf8Z3dWVwDjMWyJKQjWBZECR\n' > /tmp/agl.pub
    minisign -V -m agl -x agl.minisig -p /tmp/agl.pub

                              ${YELLOW}IMPORTANT${NC}

${LCYAN}UFW firewall${NC}
- AGL uses built-in system network capabilities to manage traps.
- An active firewall (UFW) is required for normal operation.
- Item 3.1: check current UFW status.
- Item 3.2: automatically allow the current SSH session and enable UFW.

${LCYAN}Updates${NC}
- Item 4 updates only complex-critical packages: openssh-server and ufw.
- Full system updates belong to the complex autoupdate module or manual apt
- Service code 99 (not listed in the menu): auto mode, every decision in one choice.
  In auto mode package conflicts resolve to the local version, the execution
  stream stays fully visible; integrity verification is mandatory.

${LCYAN}After installation${NC}
- If you are upgrading an existing AGL complex, please close current TUI sessions.
  This is critical for refreshing executable code in RAM.

======================================================================
EN
    fi
    pager "${L[@]}"
}

# === ПУНКТ 2: ПРОВЕРКА SHA-256 (только проверить и вернуться) ===
do_sha() {
    cls
    say "======================================================================" \
        "======================================================================"
    say "====== AMBER GUARD LABORATORY — КОНТРОЛЬ ЦЕЛОСТНОСТИ (SHA-256) =======" \
        "========= AMBER GUARD LABORATORY — INTEGRITY CHECK (SHA-256) ========="
    say "======================================================================" \
        "======================================================================"


    say "" ""
    say "  1 — проверить checksums.txt" "  1 — verify checksums.txt"
    say "  0 — Возврат в главное меню"                           "  0 — Back to main menu"
    read -r choice || choice=""
    say "" ""
    say "--------------------------------------------------------------------------" "--------------------------------------------------------------------------"
    sleep 0.2
    case "$choice" in
        1)
            [[ -f "$SRC_BIN" ]] || { say "${CYAN}>${NC} ${RED}[ ERROR ]${NC} В директории нет бинарника agl." "${CYAN}>${NC} ${RED}[ ERROR ]${NC} agl binary missing in the directory."; pause; return; }
            # Fail closed: нет файла сумм или строки для agl — проверка невозможна, abort.
            [[ -f "$SRC_SUM" ]] || { say "${CYAN}>${NC} ${RED}[ ERROR ]${NC} Нет checksums.txt: проверка невозможна (fail closed)." "${CYAN}>${NC} ${RED}[ ERROR ]${NC} checksums.txt missing: verification impossible (fail closed)."; pause; return; }
            local sum_line
            sum_line="$(awk '$2=="agl"' "$SRC_SUM")"
            [[ -n "$sum_line" ]] || { say "${CYAN}>${NC} ${RED}[ ERROR ]${NC} В checksums.txt нет строки для agl." "${CYAN}>${NC} ${RED}[ ERROR ]${NC} checksums.txt has no line for agl."; pause; return; }
            if ( cd "$DIR" && printf '%s\n' "$sum_line" | sha256sum -c - ); then
                sleep 0.2
                say "" ""
                say "${CYAN}>${NC} ${GREEN}[ OK ]${NC} SHA-256 подтверждён." "${CYAN}>${NC} ${GREEN}[ OK ]${NC} SHA-256 verified."
                SHA_STATE="ok"
            else
                sleep 0.2
                say "" ""
                say "${CYAN}>${NC} ${RED}[ ERROR ]${NC} Несовпадение SHA-256: файл повреждён или подменён. Установка заблокирована." \
                    "${CYAN}>${NC} ${RED}[ ERROR ]${NC} SHA-256 mismatch: file corrupted or tampered. Installation blocked."
                SHA_STATE="fail"
            fi
            ;;
        0|"") return ;;   # ENTER эквивалентен возврату: ENTER в инсталляторе никогда не наказывает
        *) say "${CYAN}>${NC} ${YELLOW}[ WARNING ]${NC} Неверный выбор." "${CYAN}>${NC} ${YELLOW}[ WARNING ]${NC} Invalid choice." ;;
    esac
    pause
}

# === ПУНКТ 3: Управление сервисом UFW (подменю держит админа внутри себя) ===
do_ufw() {
    while true; do
        cls
        say "======================================================================" \
            "======================================================================"
        say "========== AMBER GUARD LABORATORY — УПРАВЛЕНИЕ СЕРВИСОМ UFW ==========" \
            "========== AMBER GUARD LABORATORY — UFW SERVICE MANAGEMENT ==========="
        say "======================================================================" \
            "======================================================================"

        say "" ""
        say "  1 — проверить статус UFW"              "  1 — check UFW status"
        say "  2 — включить UFW и установить правила (дверь админа)" \
            "  2 — enable UFW and set rules (admin door)"
        say "  0 — в главное меню"                     "  0 — back to main menu"
        read -r c || c=""
        case "$c" in
            # 3.1 — осмотр: возврат В ЭТО же меню, маршрут осмотра непрерывен.
            1) ufw_status_show
               pause "${CYAN}[ENTER]${NC} Для возврата в меню UFW" "${CYAN}[ENTER]${NC} To return to the UFW menu" ;;
            # 3.2 — декларация: успех завершает ветку (возврат в главное меню),
            #       сбой оставляет в меню UFW для повтора попытки.
            2) if ufw_enable_door; then
                   pause   # без аргументов: «в основное меню»; админ дочитывает результат
                   return
               fi
               pause "${CYAN}[ENTER]${NC} Для возврата в меню UFW" "${CYAN}[ENTER]${NC} To return to the UFW menu" ;;
            0|"") return ;;   # ENTER = возврат в главное меню, без тихих сюрпризов
            *) ;;
        esac
    done
}

# === Подпункт 3.1: информация без действия (флаг не ставит) ===
ufw_status_show() {
    if ! command -v ufw >/dev/null 2>&1; then
        say "" ""
        say "--------------------------------------------------------------------------" "--------------------------------------------------------------------------"
        say "${CYAN}>${NC} ${RED}[ ERROR ]${NC} ufw не установлен в системе." "${CYAN}>${NC} ${RED}[ ERROR ]${NC} ufw is not installed on the system."
        return
    fi
    say "" ""
    say "--------------------------------------------------------------------------" "--------------------------------------------------------------------------"
    LC_ALL=C ufw status verbose || true
    say "--------------------------------------------------------------------------" "--------------------------------------------------------------------------"
}

# === Подпункт 3.2: декларация периметра; ЕДИНСТВЕННЫЙ ставит UFW_READY ===
# CONTRACT: return 0 — декларация принята (флаг установлен); return 1 — действие
#           не выполнено (целостность, отсутствие ufw, сбой правила или активации).
ufw_enable_door() {
    # Связка с пунктом 2: проваленная целостность блокирует изменения периметра.
    if [[ "$SHA_STATE" == "fail" ]]; then
        say "" ""
        say "--------------------------------------------------------------------------" "--------------------------------------------------------------------------"
        say "${CYAN}>${NC} ${RED}[ ERROR ]${NC} SHA-256 провален: изменения периметра заблокированы до решения по целостности." \
            "${CYAN}>${NC} ${RED}[ ERROR ]${NC} SHA-256 failed: perimeter changes blocked until integrity is resolved."
        return 1
    fi
    if ! command -v ufw >/dev/null 2>&1; then
        say "" ""
        say "--------------------------------------------------------------------------" "--------------------------------------------------------------------------"
        say "${CYAN}>${NC} ${RED}[ ERROR ]${NC} ufw не установлен в системе." "${CYAN}>${NC} ${RED}[ ERROR ]${NC} ufw is not installed on the system."
        say "Установите его (apt install ufw) и вернитесь в этот пункт." \
            "Install it (apt install ufw) and return to this item."
        say "Без активного UFW установка комплекса недоступна (fail closed)." \
            "Without active UFW the complex installation is unavailable (fail closed)."
        return 1
    fi
    if [[ "$(LC_ALL=C ufw status 2>/dev/null | awk '/^Status:/{print $2}')" == "active" ]]; then
        # Периметр уже жив: правил не касаемся, декларацию засчитываем.
        say "" ""
        say "--------------------------------------------------------------------------" "--------------------------------------------------------------------------"
        say "${CYAN}>${NC} ${GREEN}[ OK ]${NC} UFW уже активен. Проверка пройдена." \
            "${CYAN}>${NC} ${GREEN}[ OK ]${NC} UFW already active. Verification passed."
        ufw_unit_persist || return 1   # активен сейчас ≠ переживёт ребут
        UFW_READY=1
        return 0
    fi
    # Дверь админа: порт живой сессии → настроенный порт в sshd_config → фоллбек 22.
    local port
    port="$(printf '%s' "${SSH_CONNECTION:-}" | awk '{print $4}')"
    if ! [[ "$port" =~ ^[0-9]+$ ]]; then
        port="$(awk '/^[[:space:]]*Port[[:space:]]+/ {print $2; exit}' /etc/ssh/sshd_config 2>/dev/null || true)"
    fi
    [[ "$port" =~ ^[0-9]+$ ]] || port=22
    # Двухэтапность видна админу: правило и активация — раздельные пары операция/результат.
    say "" ""
    say "--------------------------------------------------------------------------" "--------------------------------------------------------------------------"
    op "Выполняется создание UFW правила: открытие порта ${LCYAN}$port${NC}/tcp" \
       "Creating UFW rule: allowing port ${LCYAN}$port${NC}/tcp"
    if ufw allow "$port/tcp" >/dev/null; then
        res "UFW правило ${LCYAN}$port${NC}/tcp создано" "UFW rule $port${NC}/tcp created"
    else
        fail "Сбой создания UFW правила ${LCYAN}$port${NC}/tcp" \
             "Failed to create UFW rule ${LCYAN}$port${NC}/tcp"
        return 1
    fi
    op "Выполняется активация UFW" "Activating UFW"
    if ufw --force enable >/dev/null; then
        res "Активация UFW выполнена успешно" "UFW activation completed successfully"
        ufw_unit_persist || return 1   # декларация периметра включает его бут-персистентность
        UFW_READY=1
        return 0
    fi
    fail "Сбой активации UFW: пункт не засчитан, пункт 5 заблокирован." \
         "UFW activation failed: item not counted, item 5 locked."
    return 1
}

# === Бут-персистентность UFW: юнит включён и не маскирован ===
# PURPOSE: ufw --force enable пишет ENABLED=yes, но на буте правила поднимает юнит;
#          маскированный/выключенный юнит = открытый периметр после ребута.
# CONTRACT: ufw.service — oneshot, is-active бессмысленен («dead» при живых правилах);
#           бут-готовность проверяется ТОЛЬКО is-enabled. Живой демон не трогается.
# RETURN: 0 — юнит поднимет правила на буте; 1 — персистентность не обеспечена.
ufw_unit_persist() {
    local st
    st="$(systemctl is-enabled ufw 2>/dev/null || true)"
    if [[ "$st" == "masked" ]]; then
        op "Юнит ufw маскирован: снимаем маску (${GREEN}systemctl unmask ufw${NC})" \
           "UFW unit is masked: removing mask (${GREEN}systemctl unmask ufw${NC})"
        systemctl unmask ufw >/dev/null 2>&1 || { fail "Не удалось снять маску с юнита ufw." "Failed to unmask ufw unit."; return 1; }
    fi
    op "Включаем юнит UFW в автозагрузку systemd" "Enabling UFW unit in systemd autostart"
    if systemctl enable ufw >/dev/null 2>&1; then
        st="$(systemctl is-enabled ufw 2>/dev/null || true)"
        if [[ "$st" == "enabled" || "$st" == "static" ]]; then
            res "Модуль UFW загрузит правила при запуске системы (is-enabled: ${GREEN}$st${NC})" "The UFW module will load the rules when the system starts. (is-enabled: ${GREEN}$st${NC})"
            return 0
        fi
        fail "Статус юнита UFW после enable: ${RED}$st${NC} — бут-персистентность не обеспечена." \
             "UFW unit status after enable: ${RED}$st${NC} — boot persistence not guaranteed."
        return 1
    fi
    fail "Не удалось включить юнит UFW в systemd." "Failed to enable UFW unit in systemd."
    return 1
}

# === Бут-владелец SSH-порта: классическая ssh.service вместо ssh.socket ===
# PURPOSE: при socket-activation Port в sshd_config игнорируется: после ребута система
#          возвращается на порт сокета или встаёт без слушателя — корень жёсткого локаута.
# CONTRACT: меняем ТОЛЬКО бут-конфигурацию (без --now и без рестартов): живой демон
#           и текущая сессия не трогаются, изменения вступают на следующем ребуте.
# SIDE-EFFECTS: systemctl disable ssh.socket; systemctl enable ssh.service.
# RETURN: 0 — ровно один бут-владелец порта (ssh.service); 1 — гарантировать не смогли.
ssh_boot_ownership() {
    # Проба в два слоя: кэш юнитов systemd способен соврать, диск — нет.
    # Маскированный сокет считается присутствующим: это штатный след кастлинга.
    local sock_present=0
    if systemctl list-unit-files ssh.socket 2>/dev/null | grep -q "ssh\.socket"; then sock_present=1; fi
    if [[ -e /lib/systemd/system/ssh.socket || -e /etc/systemd/system/ssh.socket ]]; then sock_present=1; fi
    if [[ "$sock_present" -eq 0 ]]; then
        # Сокета правда нет: схема классическая. Но включение ssh.service обеспечиваем
        # ВСЕГДА: на части дистрибутивов служба приходит выключенной, а бут без
        # владельца порта = локаут. Лечим здорового только там, где он здоров.
        if systemctl enable ssh.service >/dev/null 2>&1 && [[ "$(systemctl is-enabled ssh.service 2>/dev/null || true)" == "enabled" ]]; then
            res "Юнит ssh.socket отсутствует: схема классическая, ssh.service взята в автозагрузку." \
                "ssh.socket unit absent: classic scheme, ssh.service taken into autostart."
            return 0
        fi
        fail "ssh.socket отсутствует и ssh.service не включается: бут останется без владельца порта." \
             "ssh.socket absent and ssh.service cannot be enabled: boot would have no port owner."
        return 1
    fi
    op "SSH: Передача владения портом (ssh.socket -> ssh.service)" \
       "SSH: Transferring port ownership (ssh.socket -> ssh.service)"
    systemctl disable ssh.socket >/dev/null 2>&1 || true   # маскирован/отсутствует — не фатально
    if ! systemctl enable ssh.service >/dev/null 2>&1; then
        fail "Не удалось включить ssh.service в автозагрузку systemd." \
             "Failed to enable ssh.service in systemd autostart."
        return 1
    fi
    local st_sock st_svc
    st_sock="$(systemctl is-enabled ssh.socket 2>/dev/null || true)"
    st_svc="$(systemctl is-enabled ssh.service 2>/dev/null || true)"
    if [[ "$st_svc" == "enabled" && ( "$st_sock" == "disabled" || "$st_sock" == "masked" ) ]]; then
        res "ssh.service будет управлять портом при запуске системы (сокет: ${GREEN}$st_sock${NC}).\n                Текущая сессия сохраняется без изменений." \
            "ssh.service will manage the port at system boot (socket: ${GREEN}$st_sock${NC}).\n             Current session stability is maintained."
        return 0
    fi
    fail "Не подтвержден владелец порта SSH: socket=${RED}$st_sock${NC}, service=$st_svc." \
         "SSH port owner not confirmed: socket=${RED}$st_sock${NC}, service=$st_svc."
    return 1
}

# === ПУНКТ 4: ПРОВЕРКА И УСТАНОВКА РЕКОМЕНДУЕМЫХ ОБНОВЛЕНИЙ ===
# PURPOSE: закрыть дыру «грязной установки»: отложенные обновления openssh-server/ufw
#          способны перезаписать бут-топологию ПОСЛЕ наших нормализаций.
# CONTRACT: только точечное дообновление критичного списка; полные обновления —
#           автолечение комплекса или ручной apt. Нет сети → warn и возврат, без замков.
#           После установки пакетов нормализации пере-применяются АВТОМАТИЧЕСКИ
#           (намерение уже объявлено выбором пункта), но видимо: пары оп/рез.
# SIDE-EFFECTS: apt update (списки); apt install --only-upgrade наследуемым TTY
#           (диалоги dpkg видны админу); пере-применение ufw_unit_persist и
#           ssh_boot_ownership.
UPD_CRIT=(openssh-server ufw)

do_updates() {
    cls
    say "======================================================================" \
        "======================================================================"
    say "========= AMBER GUARD LABORATORY — РЕКОМЕНДУЕМЫЕ ОБНОВЛЕНИЯ ==========" \
        "============ AMBER GUARD LABORATORY — RECOMMENDED UPDATES ============"
    say "======================================================================" \
        "======================================================================"
    say ""
    op "Выполняется обновление списков пакетов (apt update)" \
       "Refreshing package lists (apt update)"
    if ! apt update >/dev/null 2>&1; then
        fail "Не удалось синхронизировать репозитории: отсутствует сетевое соединение или недоступны зеркала." \
             "Failed to synchronize repositories: network unavailable or mirrors are inaccessible."
        say "Продолжаем работу без обновления: бут-топологию проверит стадия 0 установки." \
            "Proceed without updates: install stage 0 will verify boot topology."
        pause
        return
    fi
    local upg crit n_all=0
    upg="$(apt list --upgradable 2>/dev/null | tail -n +2)"
    if [[ -n "$upg" ]]; then n_all="$(printf '%s\n' "$upg" | grep -c .)"; fi
    crit="$(printf '%s\n' "$upg" | grep -E '^(openssh-server|ufw)/' || true)"
    if [[ -z "$crit" ]]; then
        res "Критичных обновлений для комплекса нет (openssh-server, ufw). Всего доступно: $n_all." \
            "No critical updates for the complex (openssh-server, ufw). Total available: $n_all."
        if [[ "$n_all" -gt 0 ]]; then
            say "Полные обновления системы — модуль автообновлений комплекса или ручной apt." \
                "Full system updates — via the complex autoupdate module or manual apt."
        fi
        pause
        return
    fi
    res "Найдены критичные обновления: $(printf '%s\n' "$crit" | cut -d/ -f1 | tr '\n' ' '). Всего доступно: $n_all." \
        "Critical updates found: $(printf '%s\n' "$crit" | cut -d/ -f1 | tr '\n' ' '). Total available: $n_all."
    say "Инсталлятор обновляет ТОЛЬКО критичные пакеты, окно обслуживания минимально." \
        "The installer updates ONLY critical packages, keeping the maintenance window minimal."
    say ""
    say "Продолжить? [Enter] — установить рекомендуемые / [0] — пропустить" \
        "Continue? [Enter] — install recommended / [0] — skip"
    read -r c || c=""
    if [[ "$c" == "0" ]]; then pause; return; fi
    # Ориентир перед диалогами dpkg: локальный sshd_config принадлежит комплексу.
    say "${YELLOW}[ ОРИЕНТИР ]${NC} Если пакетная система спросит про /etc/ssh/sshd_config —" \
        "${YELLOW}[ ORIENT ]${NC} If the package system asks about /etc/ssh/sshd_config —"
    say "        выберите «keep the local version currently installed»: файл принадлежит комплексу." \
        "        choose 'keep the local version currently installed': the complex owns this file."
    op "Выполняется точечное обновление: ${UPD_CRIT[*]}" \
       "Running targeted update: ${UPD_CRIT[*]}"
    # TTY наследуется: поток apt и диалоги dpkg видны админу целиком, без захвата.
    if ! apt install --only-upgrade -y "${UPD_CRIT[@]}"; then
        fail "Сбой обновления критичных пакетов: нормализации не пере-применялись." \
             "Critical package update failed: normalizations were not re-applied."
        pause
        return 1
    fi
    res "Обновление завершено. Валидируем целостность конфигурации загрузки." \
        "Update complete. Validating boot configuration integrity."
    if ! ufw_unit_persist; then
        UFW_READY=0   # факт разошёлся с декларацией: замок установки возвращается
    fi
    ssh_boot_ownership \
        || say "${CYAN}>${NC} ${YELLOW}[ WARNING ]${NC} Валидация бут-топологии SSH не прошла. Стадия 0 установки обеспечит повторную проверку." \
           "${CYAN}>${NC} ${YELLOW}[ WARNING ]${NC} SSH boot topology validation failed. Installation Stage 0 will perform a re-check."
    pause
}

# === СЛУЖЕБНЫЙ КОД 99: АВТО-РЕЖИМ (документирован в пункте 1, вне оглавления) ===
# CONTRACT K1–K6: внутренних подтверждений нет, видимость полная; целостность
#   только verify (fail = прерывание); обновления критичные под контрактом
#   ночного автолечения (noninteractive + confdef/confold); UFW как 3.2;
#   стадия 0 и нормализации безусловны; никогда не ребутим, не release-upgrade.
# SIDE-EFFECTS: те же, что у последовательности пунктов 2→4→3.2→5.
do_lazy() {
    cls
    say "======================================================================" \
        "======================================================================"
    say "============ AMBER GUARD LABORATORY — АВТО-РЕЖИМ (код 99) ============" \
        "============ AMBER GUARD LABORATORY — AUTO MODE (code 99) ============"
    say "======================================================================" \
        "======================================================================"
    say ""
    say " Автоматический режим активирован. Процесс выполнения будет прослеживаться в консоли." \
        " Auto mode engaged. The entire execution flow will be fully traceable in the console."
    say ""
    say "--------------------------------------------------------------------------" "--------------------------------------------------------------------------"
    # K2: целостность — только проверка, никогда skip.
    if ! do_sha_verify_only; then pause; return 1; fi
    # K3: критичные обновления под контрактом ночного автолечения комплекса.
    op "Синхронизация репозиториев (apt update)" "Synchronizing package repositories (apt update)"
    if apt update >/dev/null 2>&1; then
        local upg crit
        upg="$(apt list --upgradable 2>/dev/null | tail -n +2)"
        crit="$(printf '%s\n' "$upg" | grep -E '^(openssh-server|ufw)/' || true)"
        if [[ -n "$crit" ]]; then
            op "Точечное обновление в авто-режиме: ${UPD_CRIT[*]} (конфликты — в пользу локальной версии)" \
               "Targeted update in auto mode: ${UPD_CRIT[*]} (conflicts — to the local version)"
            if DEBIAN_FRONTEND=noninteractive apt install -y \
                 -o Dpkg::Options::="--force-confdef" -o Dpkg::Options::="--force-confold" \
                 --only-upgrade "${UPD_CRIT[@]}"; then
                res "Точечное обновление завершено. Подготавливаем конфигурацию системы." \
                    "Targeted update complete. Validating system configuration."
                ufw_unit_persist || UFW_READY=0
                ssh_boot_ownership || true   # стадия 0 перепроверит на установке
            else
                fail "Сбой обновления критичных пакетов: продолжаем без него, стадию 0 никто не отменял." \
                     "Critical package update failed: continuing without it, stage 0 still verifies."
            fi
        else
            res "Критичных обновлений для комплекса нет." "No critical updates for the complex."
        fi
    else
        fail "Сеть отсутствует: обновления пропущены, стадию 0 никто не отменял." \
             "Network unavailable: updates skipped, stage 0 still verifies."
    fi
    # K4: декларация периметра ровно как в 3.2.
    if ! ufw_enable_door; then pause; return 1; fi
    # K5: установка без подтверждения: намерение зафиксировано кодом.
    # Сброс LAZY после возврата: ручной пункт 5 обязан снова спросить подтверждение.
    LAZY=1
    local rc=0
    do_install || rc=$?
    LAZY=0
    return $rc
}

# === ПУНКТ 5: УСТАНОВКА (атомарная подмена + sanity + рестарт активных) ===
do_install() {
    cls
    say "======================================================================" \
        "======================================================================"
    say "================= AMBER GUARD LABORATORY — УСТАНОВКА =================" \
        "================ AMBER GUARD LABORATORY — INSTALLATION ==============="
    say "======================================================================" \
        "======================================================================"


    # Декларативный замок: без осознанного пункта 3 установка недоступна.
    if [[ "$UFW_READY" -ne 1 ]]; then
        say "" ""
        say "${CYAN}>${NC} ${RED}[ ERROR ]${NC} Установка заблокирована.\n\n" \
            "${CYAN}>${NC} ${RED}[ ERROR ]${NC} Installation blocked.\n\n"
        say "Требуется инициализация периметра (UFW). Выполните пункт 3." \
            "Perimeter initialization (UFW) required. Run item 3."
        pause
        return 1
    fi

    # Fail closed: проваленная проверка целостности блокирует установку.
    if [[ "$SHA_STATE" == "fail" ]]; then
        say "" ""
        say "${CYAN}>${NC} ${RED}[ ERROR ]${NC} Проверка SHA-256 не пройдена: установка заблокирована." \
            "${CYAN}>${NC} ${RED}[ ERROR ]${NC} SHA-256 check failed earlier: installation blocked."
        pause
        return 1
    fi
    # Проверки не было: предлагаем намерение проверить ИЛИ осознанно пропустить.
    if [[ "$SHA_STATE" == "none" ]]; then
        say "" ""
        say "Целостность не проверена. Выбор:" "Integrity not verified. Choose:"
        say "  1 — проверить сейчас и продолжить" "  1 — verify now and continue"
        say "  2 — пропустить проверку (на ваш риск)" "  2 — skip verification (at your risk)"
        say "" ""
        say "  0 — отмена" "  0 — cancel"
        read -r c || c=""
        say "" ""
        case "$c" in
            1)
                if ! do_sha_verify_only; then pause; return 1; fi
                ;;
            2)
                SHA_STATE="skipped"
                say "${CYAN}>${NC} ${YELLOW}[ WARNING ]${NC} Проверка целостности не произведена: Установка без проверки целостности." \
                    "${CYAN}>${NC} ${YELLOW}[ WARNING ]${NC} Integrity check not performed: Installation without integrity check."
                ;;
            *) pause; return 1 ;;
        esac
    fi

    # --- ПЛАН РАЗВЕРТЫВАНИЯ ---
    say "" ""
    say "ПЛАН РАЗВЕРТЫВАНИЯ:" "DEPLOYMENT STAGES:"
    say "" ""
    say "  - Обновления: критичные пакеты (openssh-server, ufw) обновлены пунктом 4;" \
        "  - Updates: critical packages (openssh-server, ufw) were updated in item 4;"
    say "    бут-топология ниже проверяется стадией 0 независимо от их состояния" \
        "    boot topology is re-verified by stage 0 below regardless of their state"
    say "" ""
    say "  - Замена: Атомарное развертывание бинарного файла в директории $DST_BIN" \
        "  - Replacement: Atomic deployment of the binary to $DST_BIN"
    say "    (механизм отката: agl.old)" \
        "    (rollback mechanism: agl.old)"
    say "  - Активация: Перезапуск ТОЛЬКО активных демонов (${UNITS[*]})" \
        "  - Activation: Restart ONLY active daemons (${UNITS[*]})"
    say "    для инициализации нового кода" \
        "    to initialize the new code"
    say "  - SSH: со следующего ребута порт держит ssh.service (socket-activation отключён);" \
        "  - SSH: from next reboot ssh.service owns the port (socket-activation disabled);"
    say "    активные демоны и текущая сессия не затрагиваются" \
        "    active daemons and current session are not touched"
    say "  - Инициализация: Первый запуск комплекса создаст конфигурационный файл config.json" \
        "  - Initialization: The first run of the complex will generate the config.json."
    say "    предварительно уведомив об этом уведомлением в TUI" \
        "    configuration file and provide a notification within the TUI."

    say "" ""
    if [[ "${LAZY:-0}" -eq 1 ]]; then
        # Авто-режим: намерение зафиксировано выбором кода 99, план показан полностью.
        say "${CYAN}>${NC} ${GREEN}[ INFO ]${NC} Автоматический режим установки: подтверждение зафиксировано выбором кода 99." \
            "${CYAN}>${NC} ${GREEN}[ INFO ]${NC} Automatic installation mode: confirmation recorded by choosing code 99."
    else
        say "Продолжить? [Enter] — да / [0] — отмена" "Continue? [Enter] — yes / [0] — cancel"
        read -r c || c=""
        [[ "$c" == "0" ]] && { pause; return 1; }
    fi

    # --- СТАДИЯ 0: бут-подготовка (профилактика локаута после ребута) ---
    # Сбой стадии прерывает установку ДО касания бинарника: система остаётся нетронутой.
    if ! ssh_boot_ownership; then
        pause
        return 1
    fi

    # --- ИСПОЛНЕНИЕ: атомарная подмена ---
    [[ -f "$SRC_BIN" ]] || { say "${CYAN}>${NC} ${RED}[ ERROR ]${NC} В директории нет бинарника agl." "${CYAN}>${NC} ${RED}[ ERROR ]${NC} agl binary missing."; pause; return 1; }
    mkdir -p "$DST_DIR"
    local TMP_BIN="$DST_DIR/agl.new.$$"
    trap 'rm -f "$TMP_BIN" 2>/dev/null; stty sane 2>/dev/null' EXIT   # темп не переживает скрипт; терминал санитарен

    local OLD_VER="(не было / absent)"
    if [[ -x "$DST_BIN" ]]; then
        OLD_VER="$("$DST_BIN" --version 2>/dev/null || echo '(битый / broken)')"
    fi

    # Копия в темп рядом с целью: та же ФС => последующий rename атомарен.
    install -m 0755 "$SRC_BIN" "$TMP_BIN" || { say "${CYAN}>${NC} ${RED}[ ERROR ]${NC} Сбой копии во временный файл." "${CYAN}>${NC} ${RED}[ ERROR ]${NC} Failed to copy to temp file."; pause; return 1; }

    # Sanity НА ТЕМПЕ до касания цели: чужая архитектура или битость не доедут до цели.
    local NEW_VER
    NEW_VER="$("$TMP_BIN" --version 2>/dev/null || true)"
    if [[ -z "$NEW_VER" ]]; then
        rm -f "$TMP_BIN"
        say "${CYAN}>${NC} ${RED}[ ERROR ]${NC} Новый бинарник не исполняется (--version пуст). Система не тронута." \
            "${CYAN}>${NC} ${RED}[ ERROR ]${NC} New binary does not execute (--version empty). System untouched."
        pause
        return 1
    fi

    # Атомарный свап: старое уходит в agl.old (одно поколение отката), темп становится целью.
    if [[ -f "$DST_BIN" ]]; then
        mv -f "$DST_BIN" "$OLD_BIN" || { say "${CYAN}>${NC} ${RED}[ ERROR ]${NC} Сбой отодвигания старого бинарника." "${CYAN}>${NC} ${RED}[ ERROR ]${NC} Failed to move old binary aside."; pause; return 1; }
    fi
    if ! mv -f "$TMP_BIN" "$DST_BIN"; then
        if [[ -f "$OLD_BIN" ]]; then mv -f "$OLD_BIN" "$DST_BIN"; fi
        say "${CYAN}>${NC} ${RED}[ ERROR ]${NC} Сбой установки нового бинарника — выполнен откат." "${CYAN}>${NC} ${RED}[ ERROR ]${NC} Failed to place new binary — rollback performed."
        pause
        return 1
    fi
    sleep 0.3
    say "${CYAN}>${NC} ${GREEN}[ OK ]${NC} Установлен: $NEW_VER" "${CYAN}>${NC} ${GREEN}[ OK ]${NC} Installed: $NEW_VER"
    say "       (было: $OLD_VER)" "       (was: $OLD_VER)"

    # Симлинк глобальной команды agl: сбой не фатален (warn вместо смерти постфактум).
    ln -sf "$DST_BIN" /usr/local/bin/agl 2>/dev/null \
        || say "${CYAN}>${NC} ${YELLOW}[ WARNING ]${NC} Не создан симлинк /usr/local/bin/agl." "${CYAN}>${NC} ${YELLOW}[ WARNING ]${NC} Symlink /usr/local/bin/agl not created."

    # Рестарт ТОЛЬКО активных юнитов: инсталлятор обновляет код в ОЗУ, жизненный цикл — у комплекса.
    local u
    for u in "${UNITS[@]}"; do
        if systemctl is-active --quiet "$u"; then
            if systemctl restart "$u"; then
                sleep 0.2
                say "${CYAN}>${NC} ${GREEN}[ OK ]${NC} Юнит перезапущен на новый код: $u" "${CYAN}>${NC} ${GREEN}[ OK ]${NC} Unit restarted with new code: $u"
            else
                say "${CYAN}>${NC} ${YELLOW}[ WARNING ]${NC} Сбой рестарта $u: демон может держать СТАРЫЙ код!" "${CYAN}>${NC} ${YELLOW}[ WARNING ]${NC} Restart of $u failed: daemon may hold OLD code!"
            fi
        else
            say "" ""
            say "Юнит $u не активен — комплекс поднимет его сам." "Unit $u inactive — the complex will start it itself."
        fi
    done

    say "--------------------------------------------------------------------------" "--------------------------------------------------------------------------"
    say "" ""
    say "${LCYAN}               #######          ################   #####           
              #########         ################   #####           
             ##### #####        #####              #####           
            #####   #####       #####              #####           
           #####     #####      #####              #####           
          #####       #####     #####  ##########  #####           
         ###################    #####  ##########  #####           
        #####################   #####        ####  #####           
       #####             #####  #################  ################
       ####               ####   ###############   ################${NC}"
    say "" ""
    say "--------------------------------------------------------------------------" "--------------------------------------------------------------------------"
    sleep 0.5
    say " Установка AGL успешно завершена." "AGL Installation complete."
    say "" ""
    say " Напоминания:" " Reminders:"
    say "  1. Завершите работу старых TUI-сессий: в их оперативной памяти сохранен старый код." "  1. Close old TUI sessions: they retain deprecated code in memory."
    say "  2. Откат: ${GREEN}sudo mv $OLD_BIN $DST_BIN && sudo systemctl restart ${UNITS[*]}${NC}" \
        "  2. Rollback: ${GREEN}sudo mv $OLD_BIN $DST_BIN && sudo systemctl restart ${UNITS[*]}${NC}"
    say "  3. Настройка пароля: Необходимо установить пароль в меню SETTING для корректной работы модуля Watcher." "  3. Password setup: You must set a password in the SETTING menu for the Watcher module to function correctly."
    say "  4. Веб-адреса комплекса доступны по команде в терминале: ${GREEN}agl --info${NC}" "  4. Complex web addresses available via command: ${GREEN}agl --info${NC}"
    say "  5. Запуск комплекса: ${GREEN}agl${NC}" "  5. Start command: ${GREEN}agl${NC}"
    say "" ""
    say "" ""
    exit 0
}

# === Внутренняя верификация суммы без паузы (для подвыбора внутри пункта 5) ===
do_sha_verify_only() {
    [[ -f "$SRC_BIN" ]] || { say "${CYAN}>${NC} ${RED}[ ERROR ]${NC} В директории нет бинарника agl." "${CYAN}>${NC} ${RED}[ ERROR ]${NC} agl binary missing."; return 1; }
    [[ -f "$SRC_SUM" ]] || { say "${CYAN}>${NC} ${RED}[ ERROR ]${NC} Нет checksums.txt: проверка невозможна (fail closed)." "${CYAN}>${NC} ${RED}[ ERROR ]${NC} checksums.txt missing: verification impossible (fail closed)."; return 1; }
    local sum_line
    sum_line="$(awk '$2=="agl"' "$SRC_SUM")"
    [[ -n "$sum_line" ]] || { say "${CYAN}>${NC} ${RED}[ ERROR ]${NC} В checksums.txt нет строки для agl." "${CYAN}>${NC} ${RED}[ ERROR ]${NC} checksums.txt has no line for agl."; return 1; }
    if ( cd "$DIR" && printf '%s\n' "$sum_line" | sha256sum -c - ); then
        say "" ""
        say "--------------------------------------------------------------------------" "--------------------------------------------------------------------------"
        say "${CYAN}>${NC} ${GREEN}[ OK ]${NC} SHA-256 подтверждён." "${CYAN}>${NC} ${GREEN}[ OK ]${NC} SHA-256 verified."
        SHA_STATE="ok"
        return 0
    fi
    say "${CYAN}>${NC} ${RED}[ ERROR ]${NC} Несовпадение SHA-256: файл повреждён или подменён. Установка заблокирована." \
        "${CYAN}>${NC} ${RED}[ ERROR ]${NC} SHA-256 mismatch: file corrupted or tampered. Installation blocked."
    SHA_STATE="fail"
    return 1
}

# === ОГЛАВЛЕНИЕ МЕНЮ: полосы LWHITE, заголовок GREEN, пункты LCYAN ===
show_menu() {
    cls
    say "======================================================================" \
        "======================================================================"
    say "========== AMBER GUARD LABORATORY — МЕНЮ УСТАНОВКИ ($DIST_VER) ==========" \
        "=========== AMBER GUARD LABORATORY — INSTALL MENU ($DIST_VER) ==========="
    say "======================================================================" \
        "======================================================================"
    printf '\n'
    say "  ${LCYAN}1 — Информация (краткая инструкция)${NC}" \
        "  ${LCYAN}1 — Information (brief instructions)${NC}"
    say "  ${LCYAN}2 — Проверить checksums.txt${NC}" \
        "  ${LCYAN}2 — Verify checksums.txt${NC}"
    say "  ${LCYAN}3 — Управление сервисом UFW${NC}" \
        "  ${LCYAN}3 — UFW service management${NC}"
    say "  ${LCYAN}4 — Проверить и установить рекомендуемые обновления${NC}" \
        "  ${LCYAN}4 — Check and install recommended updates${NC}"
    if [[ "$UFW_READY" -eq 1 ]]; then
        say "  ${LCYAN}5 — Произвести установку комплекса AGL${NC}" \
            "  ${LCYAN}5 — Install the AGL complex${NC}"
    else
        say "  ${YELLOW}5 — Произвести установку комплекса AGL (заблокировано: выполните пункт 3)${NC}" \
            "  ${YELLOW}5 — Install the AGL complex (locked: run item 3 first)${NC}"
    fi
    say "  ${LCYAN}0 — Выход${NC}" "  ${LCYAN}0 — Exit${NC}"
}

# === МЕНЮ-ЦИКЛ: намерение отдельно, исполнение отдельно, каскадов нет ===
while true; do
    show_menu
    read -r choice || choice=""
    case "$choice" in
        1) show_info ;;
        2) do_sha ;;
        3) do_ufw ;;
        4) do_updates ;;
        5) do_install || true ;;   # сбой установки = возврат в меню, не смерть скрипта
        99) do_lazy || true ;;     # служебный код авто-режима, вне оглавления
        0) exit 0 ;;
        *) ;;                      # неверный ввод = тихая перерисовка меню
    esac
done