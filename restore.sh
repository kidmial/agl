#!/bin/bash
# =====================================================================
# 02_restore.sh — автономное восстановление из снапшота AGL (shell-эра v5.0.1)
# ГОЛЫЙ СЕРВЕР: без Go, без dialog, без AGL-бинарника. Точка входа: /backup/
# ИЗМЕНЕНИЯ v5.0.1:
#   - КОНТУР 6.5: зачистка самоссылающихся симлинков (лечение бага v8.4.2).
# =====================================================================
set -euo pipefail

BACKUP_ROOT="/backup"
NET_BACKUP="/root/.agl_netplan_backup"
say() { printf '%s\n' "$*"; }
die() { printf '❌ %s\n' "$*" >&2; exit 1; }

[ "$EUID" -eq 0 ] || die "Нужны права root: sudo ./02_restore.sh"
[ -d "$BACKUP_ROOT" ] || die "Каталог $BACKUP_ROOT отсутствует."

# === КОНТУР 0.5: блокировка APT ===
say "→ Освобождаю лок APT (гашу unattended-upgrades и apt-daily)..."
systemctl stop unattended-upgrades.service apt-daily.service apt-daily-upgrade.service 2>/dev/null || true
LOCK_WAIT=0
while pgrep -x apt-get >/dev/null 2>&1 || pgrep -x dpkg >/dev/null 2>&1 || pgrep -f unattended-upgr >/dev/null 2>&1; do
    LOCK_WAIT=$((LOCK_WAIT + 5))
    if [ "$LOCK_WAIT" -ge 600 ]; then
        die "Лок dpkg не освободился за 600 сек: прервано (fail closed)."
    fi
    say "· Ожидание освобождения лока dpkg... (${LOCK_WAIT} сек)"
    sleep 5
done
say "✓ Лок APT свободен."

# === КОНТУР 1: выбор точки восстановления ===
mapfile -t SESSIONS < <(find "$BACKUP_ROOT" -mindepth 1 -maxdepth 1 -type d | sort)
[ "${#SESSIONS[@]}" -gt 0 ] || die "$BACKUP_ROOT пуст: восстанавливать нечего."
say "===================================================="
say "          ДОСТУПНЫЕ ТОЧКИ ВОССТАНОВЛЕНИЯ"
say "===================================================="
i=0
for s in "${SESSIONS[@]}"; do i=$((i+1)); say "  $i) ${s#"$BACKUP_ROOT"/}"; done
say "===================================================="
printf 'Номер точки: '
read -r CHOICE
if ! [[ "$CHOICE" =~ ^[0-9]+$ ]] || [ "$CHOICE" -lt 1 ] || [ "$CHOICE" -gt "${#SESSIONS[@]}" ]; then
    die "Некорректный выбор."
fi
SESSION_DIR="${SESSIONS[$((CHOICE-1))]}"
TS="$(basename "$SESSION_DIR")"
SNAPSHOT="$SESSION_DIR/${TS}_server_snapshot.tar.gz"
PKGLIST="$SESSION_DIR/${TS}_package_list.txt"
[ -f "$SNAPSHOT" ] || die "В сессии $TS нет *_server_snapshot.tar.gz."

# === КОНТУР 2: целостность архива ===
gzip -t "$SNAPSHOT" || die "Архив повреждён: восстановление прервано."
say "✓ Целостность архива подтверждена."

# === КОНТУР 3: PRE-FLIGHT PHP ===
NEW_PHP_VERSION=$(apt-cache search php-fpm | grep -E '^php[0-9]\.[0-9]-fpm' | awk '{print $1}' | sed 's/-fpm//' | sed 's/php//' | sort -V | tail -n 1)
[ -n "$NEW_PHP_VERSION" ] || die "Пакеты PHP-FPM не найдены в репозиториях."
say "→ Целевая версия PHP: $NEW_PHP_VERSION"

# === КОНТУР 4: базовое окружение ===
DEBIAN_FRONTEND=noninteractive apt-get install -y -q -o DPkg::Lock::Timeout=600 \
    -o Dpkg::Options::="--force-confdef" -o Dpkg::Options::="--force-confold" \
    openssh-server ufw fail2ban apache2 nginx

# === КОНТУР 5: защита сети ===
mkdir -p "$NET_BACKUP"
if [ -d /etc/netplan ]; then
    cp -a /etc/netplan/. "$NET_BACKUP"/ 2>/dev/null || true
    say "✓ Текущий netplan сохранён в $NET_BACKUP"
fi

# === КОНТУР 6: распаковка ===
say "→ Распаковка снапшота $TS в / ..."
tar -xzf "$SNAPSHOT" -C / \
    --exclude='var/log/*' \
    --exclude='*.log'
say "✓ Конфигурации распакованы."

# === КОНТУР 6.5 (НОВОЕ v5.0.1): ЗАЧИСТКА САМОССЫЛАЮЩИХСЯ СИМЛИНКОВ ===
say "→ Зачистка битых симлинков веб-стека..."
for d in /etc/apache2/mods-enabled /etc/apache2/sites-enabled /etc/nginx/sites-enabled; do
    [ -d "$d" ] || continue
    find "$d" -maxdepth 1 -type l | while read -r lnk; do
        target="$(readlink "$lnk" 2>/dev/null)"
        base="$(basename "$lnk")"
        # Удаляем: битые симлинки (target не существует) + самоссылающиеся
        if [ ! -e "$lnk" ] || [ "$target" = "$base" ] || [ "$target" = "./$base" ]; then
            rm -f "$lnk"
        fi
    done
done
say "✓ Битые симлинки зачищены."

# === КОНТУР 7: анти-локаут SSH ===
. /etc/os-release
VMAJ="${VERSION_ID:-0}"
case "$VMAJ" in ''|*[!0-9.]*) VMAJ=0 ;; esac
VMAJ="${VMAJ%%.*}"
if [ "${ID:-}" = "ubuntu" ] && [ "$VMAJ" -ge 24 ]; then
    systemctl stop ssh.socket 2>/dev/null || true
    systemctl disable ssh.socket 2>/dev/null || true
    systemctl mask ssh.socket 2>/dev/null || true
    systemctl enable ssh.service 2>/dev/null || true
    say "✓ Ubuntu 24+: ssh.socket замаскирован, ssh.service включён."
fi
mkdir -p /run/sshd
if [ -x /usr/sbin/sshd ]; then
    SSHD_TEST="$(/usr/sbin/sshd -t 2>&1)"
    if [ -z "$SSHD_TEST" ]; then
        systemctl restart ssh 2>/dev/null || systemctl restart sshd 2>/dev/null || true
        say "✓ sshd перезапущен (sshd -t прошёл)."
    else
        say "⚠ sshd -t провален: $SSHD_TEST"
        say "   ssh НЕ перезапущен (fail-safe). Устрани причину и: systemctl restart ssh"
    fi
fi

# === КОНТУР 8: PHP-миграция ===
OLD_PHP_VERSION=$(ls -1 /etc/php/ 2>/dev/null | grep -E '^[0-9]\.[0-9]$' | head -n 1)
if [ -n "$OLD_PHP_VERSION" ] && [ "$OLD_PHP_VERSION" != "$NEW_PHP_VERSION" ]; then
    say "→ Миграция PHP: $OLD_PHP_VERSION -> $NEW_PHP_VERSION"
    if [ -d "/etc/php/$OLD_PHP_VERSION" ] && [ ! -d "/etc/php/$NEW_PHP_VERSION" ]; then
        mv "/etc/php/$OLD_PHP_VERSION" "/etc/php/$NEW_PHP_VERSION"
    elif [ -d "/etc/php/$OLD_PHP_VERSION" ]; then
        cp -a "/etc/php/$OLD_PHP_VERSION/." "/etc/php/$NEW_PHP_VERSION/"
    fi
    find "/etc/php/$NEW_PHP_VERSION" -type f -exec sed -i "s|/etc/php/$OLD_PHP_VERSION|/etc/php/$NEW_PHP_VERSION|g" {} +
    find /etc/nginx -type f -name "*.conf" -exec sed -i "s|php$OLD_PHP_VERSION-fpm|php$NEW_PHP_VERSION-fpm|g" {} +
    find /etc/apache2 -type f -exec sed -i "s|php$OLD_PHP_VERSION|php$NEW_PHP_VERSION|g" {} +
    if "php-fpm$NEW_PHP_VERSION" -t; then
        say "✓ Адаптация PHP завершена успешно."
    else
        say "⚠ Тест конфига PHP провалился: требуется ручная проверка!"
    fi
fi

# === КОНТУР 9: возврат родной сети ===
if [ -n "$(ls -A "$NET_BACKUP" 2>/dev/null)" ]; then
    rm -rf /etc/netplan
    mkdir -p /etc/netplan
    cp -a "$NET_BACKUP"/. /etc/netplan/
    say "✓ Netplan нового хоста возвращён. ПЕРЕД reboot проверь имена интерфейсов!"
fi

# === КОНТУР 10: пакеты ===
if [ -f "$PKGLIST" ]; then
    PACKAGES=$(grep -w 'install' "$PKGLIST" \
        | grep -v -E 'docker|containerd|wireguard|wireguard-tools|snoopy|iptables-persistent|wireshark|kdump-tools' \
        | awk '{print $1}' | xargs) || true
    if [ -n "${PACKAGES:-}" ]; then
        say "→ Установка пакетов из списка..."
        if DEBIAN_FRONTEND=noninteractive apt-get update -q && \
            DEBIAN_FRONTEND=noninteractive apt-get install -y -q -o DPkg::Lock::Timeout=600 \
                -o Dpkg::Options::="--force-confdef" -o Dpkg::Options::="--force-confold" $PACKAGES; then
            say "✓ Пакеты установлены: конфиги защищены от затирания."
        else
            say "⚠ apt сбоит. Доустанови ВРУЧНУЮ позже теми же флагами confdef/confold."
        fi
    else
        say "· Список пакетов пуст."
    fi
else
    say "⚠ В сессии нет *_package_list.txt: пакетный контур пропущен."
fi
DEBIAN_FRONTEND=noninteractive apt-get install -y -q -o DPkg::Lock::Timeout=600 \
    -o Dpkg::Options::="--force-confdef" -o Dpkg::Options::="--force-confold" cron mc >/dev/null 2>&1 || true
systemctl enable --now cron >/dev/null 2>&1 || true

# === КОНТУР 11: АКТИВАЦИЯ СЕТЕВОЙ БРОНИ (UFW) ===
say "→ Активация восстановленных правил Firewall..."
if command -v ufw >/dev/null 2>&1; then
	# Перезагружаем ufw, чтобы он подтянул распакованные из бэкапа файлы конфигурации
	ufw reload >/dev/null 2>&1 || true
	# Анти-локаут: дверь для текущей сессии ДО взвода стены.
	# SSH_CONNECTION выставляет сам sshd для каждого входящего подключения;
	# на локальной консоли она пуста → охрана пропускает дверь (риска нет).
	if [ -n "${SSH_CONNECTION:-}" ]; then
		ADMIN_IP="$(echo "$SSH_CONNECTION" | awk '{print $1}')"
		SSH_PORT="$(sshd -T 2>/dev/null | awk '/^port /{print $2; exit}')"
		SSH_PORT="${SSH_PORT:-22}"
		ufw allow from "$ADMIN_IP" to any port "$SSH_PORT" proto tcp \
			comment 'AGL restore anti-lockout door' >/dev/null 2>&1 || true
		say "✓ Дверь анти-локаута: $ADMIN_IP → port $SSH_PORT"
	fi
	# Принудительно включаем файрвол
	ufw --force enable
	say "✓ Firewall успешно активирован с оригинальными правилами из бэкапа."
else
    say "⚠ Утилита ufw не найдена, активация контура защиты пропущена."
fi

say "===================================================="
say " ВОССТАНОВЛЕНИЕ ЗАВЕРШЕНО: $TS"
say " СЛЕДУЮЩИЙ ШАГ: install.sh (AGL) → 03_finalize_web.sh"
say " ПОМНИ: 3X-UI — обнови публичный IP в Inbounds после старта."
say "===================================================="