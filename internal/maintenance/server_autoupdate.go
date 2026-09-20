// internal/maintenance/server_autoupdate.go
package maintenance

import (
	"agl/config"
	"agl/i18n"
	"agl/internal/sys"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ============================================================================
// PURPOSE: Автономный модуль гигиенических обновлений ОС (unattended-upgrades semantic).
//          Oneshot-режим: отрабатывает один цикл (update + dist-upgrade + reboot-check)
//          и завершает процесс. Управляется systemd-таймером agld_autoupdate.timer.
//
// CONTRACT:
//   - Вызывается из main.go под флагом --autoupdate.
//   - Читает тумблеры AutoUpdateEnabled / AutoUpdateReboot из config.C.
//   - Использует flock (FileAutoUpdateLock) для защиты от параллельного запуска
//     (мягкий fail-safe: занято → WARNING + exit 0, а не ALERT + exit 1).
//   - Все внешние команды (apt-get, systemctl, shutdown) через sys.RunTimeout.
//   - Все события — машинные коды через sys.LogAction + i18n.TrEvent.
//   - Никогда не мутирует config.C напрямую (только читает).
//
// SIDE-EFFECTS:
//   - Может запустить apt-get update и apt-get dist-upgrade.
//   - Может инициировать отложенный ребут через `shutdown -r +1`.
//   - Пишет персистентные маркеры last_run и pending_reboot в DirLib.
// ============================================================================

const (
	// aptTimeoutUpdate — таймаут apt-get update (медленные зеркала).
	aptTimeoutUpdate = 300 * time.Second
	// aptTimeoutUpgrade — таймаут apt-get dist-upgrade (может тянуть ядра).
	aptTimeoutUpgrade = 1800 * time.Second
	// rebootGraceMin — гранс-период перед ребутон (в минутах, шаг shutdown).
	rebootGraceMin = 1
	// ComponentName — машинное имя компонента для журнала.
	ComponentName = "AUTOUPDATE"
)

// RunServerAutoUpdate выполняет один цикл автообновления.
// Возвращает код выхода: 0 = успех / no-op, 1 = критический сбой.
func RunServerAutoUpdate() int {
	// === КОНТУР 1: Мягкий flock-лок ===
	// Перехлёст oneshot-запусков (cron + manual) не должен приводить к APT-локу.
	if !acquireAutoUpdateLock() {
		// Занято — не авария, а штатная коллизия. Логируем и выходим чисто.
		sys.LogAction(ComponentName, "LOCK_BUSY", "WARNING",
			i18n.TrEvent("autoupdate.lock_busy"))
		return 0
	}
	// Лок держится открытым fd до выхода процесса (ядро освободит сам).

	// === КОНТУР 2: Проверка главного тумблера ===
	if err := config.LoadConfig(config.FileConfig); err != nil {
		sys.LogAction(ComponentName, "CONFIG_LOAD_FAIL", "ERROR",
			i18n.TrEventf("autoupdate.config_load_fail", err))
		return 1
	}
	if !config.C.AutoUpdateEnabled {
		sys.LogAction(ComponentName, "DISABLED", "INFO",
			i18n.TrEvent("autoupdate.disabled"))
		return 0
	}

	sys.LogAction(ComponentName, "CHECK_START", "PENDING",
		i18n.TrEvent("autoupdate.check_start"))

	// === КОНТУР 3: apt-get update (списки пакетов) ===
	// Сбой обновления списков НЕ фатален: пробуем upgrade по текущим спискам.
	updateRes := sys.RunTimeout(aptTimeoutUpdate,
		"env", "DEBIAN_FRONTEND=noninteractive", "apt-get", "update", "-q")
	if updateRes.Err != nil {
		sys.LogAction(ComponentName, "APT_UPDATE_FAIL", "WARNING",
			i18n.TrEventf("autoupdate.apt_update_fail", updateRes.Err,
				strings.TrimSpace(updateRes.Stderr)))
		// НЕ возвращаем 1: продолжаем с текущими списками.
	}

	// === КОНТУР 4: Симуляция upgrade для подсчёта пакетов ===
	simRes := sys.RunTimeout(60*time.Second, "apt-get", "-s", "upgrade")
	upgradableCount := parseUpgradableCount(simRes.Stdout)
	if upgradableCount == 0 {
		// Нечего ставить — записываем last_run (подтверждение живого цикла) и выходим.
		writeLastRunMarker()
		sys.LogAction(ComponentName, "CHECK_DONE_NO_UPDATES", "SUCCESS",
			i18n.TrEvent("autoupdate.check_no_updates"))
		return 0
	}

	// === КОНТУР 5: Реальный dist-upgrade ===
	sys.LogAction(ComponentName, "UPGRADE_START", "PENDING",
		i18n.TrEventf("autoupdate.upgrade_start", upgradableCount))

	upgradeRes := sys.RunTimeout(aptTimeoutUpgrade,
		"env", "DEBIAN_FRONTEND=noninteractive",
		"apt-get", "dist-upgrade", "-y",
		"-o", "Dpkg::Options::=--force-confdef",
		"-o", "Dpkg::Options::=--force-confold",
	)
	if upgradeRes.Err != nil {
		sys.LogAction(ComponentName, "UPGRADE_FAIL", "ERROR",
			i18n.TrEventf("autoupdate.upgrade_fail", upgradeRes.Err,
				strings.TrimSpace(upgradeRes.Stderr)))
		return 1
	}

	writeLastRunMarker()
	sys.LogAction(ComponentName, "UPGRADE_DONE", "SUCCESS",
		i18n.TrEventf("autoupdate.upgrade_done", upgradableCount))

	// === КОНТУР 6: Проверка необходимости ребута ===
	if !isRebootRequired() {
		return 0
	}

	pkgs := readRebootPackages()
	sys.LogAction(ComponentName, "REBOOT_REQUIRED", "PENDING",
		i18n.TrEventf("autoupdate.reboot_required", pkgs))

	if !config.C.AutoUpdateReboot {
		// Тумблер ребута выключен — только логируем, ничего не делаем.
		sys.LogAction(ComponentName, "REBOOT_SKIPPED", "INFO",
			i18n.TrEvent("autoupdate.reboot_skipped"))
		return 0
	}

	// === КОНТУР 7: Отложенный ребут ===
	writePendingRebootMarker()
	sys.LogAction(ComponentName, "REBOOT_SCHEDULED", "ALERT",
		i18n.TrEventf("autoupdate.reboot_scheduled", rebootGraceMin))

	rebootRes := sys.RunTimeout(10*time.Second,
		"shutdown", "-r", fmt.Sprintf("+%d", rebootGraceMin),
		"AGL: reboot after security updates")
	if rebootRes.Err != nil {
		sys.LogAction(ComponentName, "REBOOT_FAIL", "ERROR",
			i18n.TrEventf("autoupdate.reboot_fail", rebootRes.Err))
		return 1
	}
	return 0
}

// EnsureAutoUpdateUnits декларативно создаёт или удаляет systemd-юниты
// автообновления в зависимости от тумблера AutoUpdateEnabled.
// Вызывается из TUI после сохранения настройки (сервисный слой).
//
// CONTRACT:
//   - Идемпотентна: повторный вызов не переписывает идентичные файлы.
//   - При enabled=true: создаёт service+timer, daemon-reload, enable+start timer.
//   - При enabled=false: stop+disable timer, удаляет файлы, daemon-reload.
//   - Все systemctl через sys.RunTimeout.
func EnsureAutoUpdateUnits(enabled bool) error {
	if enabled {
		return enableAutoUpdateUnits()
	}
	return disableAutoUpdateUnits()
}

// ============================================================================
// ВСПОМОГАТЕЛЬНЫЕ ФУНКЦИИ
// ============================================================================

// acquireAutoUpdateLock захватывает неблокирующий flock на FileAutoUpdateLock.
// Возвращает false, если лок уже занят другим процессом.
// Файловый дескриптор намеренно НЕ закрываем: ядро освобождает flock при выходе.
func acquireAutoUpdateLock() bool {
	_ = os.MkdirAll(config.DirRun, 0755)
	f, err := os.OpenFile(config.FileAutoUpdateLock, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return false
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return false
	}
	// fd остаётся открытым до выхода процесса (ссылка на *os.File теряется,
	// но ядро держит flock на inode — это архитектурный паттерн R-6).
	return true
}

// parseUpgradableCount извлекает число пакетов из вывода `apt-get -s upgrade`.
// Формат строки: "42 upgraded, 0 newly installed, 0 to remove and 0 not upgraded."
// Возвращает 0 при любом сбое парсинга (fail-safe: «нечего ставить»).
func parseUpgradableCount(stdout string) int {
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "upgraded,") && !strings.Contains(line, "upgraded, ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if n, err := strconv.Atoi(fields[0]); err == nil && n >= 0 {
			return n
		}
	}
	return 0
}

// isRebootRequired проверяет наличие маркера /var/run/reboot-required.
func isRebootRequired() bool {
	_, err := os.Stat("/var/run/reboot-required")
	return err == nil
}

// readRebootPackages читает список пакетов, потребовавших ребут.
// Возвращает "(unknown)" при недоступности файла.
func readRebootPackages() string {
	data, err := os.ReadFile("/var/run/reboot-required.pkgs")
	if err != nil {
		return "(unknown)"
	}
	pkgs := strings.TrimSpace(string(data))
	if pkgs == "" {
		return "(unknown)"
	}
	// Сжимаем многострочник в однострочник для лога.
	return strings.ReplaceAll(pkgs, "\n", ", ")
}

// writeLastRunMarker атомарно записывает Unix-time текущего запуска.
func writeLastRunMarker() {
	_ = os.MkdirAll(config.DirLib, 0755)
	content := strconv.FormatInt(time.Now().Unix(), 10)
	if err := sys.WriteAtomic(config.MarkerAutoUpdateLastRun, []byte(content), 0644); err != nil {
		sys.LogAction(ComponentName, "LAST_RUN_WRITE_FAIL", "WARNING",
			i18n.TrEventf("autoupdate.last_run_write_fail", err))
	}
}

// writePendingRebootMarker помечает, что ребут запланирован.
// Маркер снимается вручную админом или автоматически после ребута (TODO v8.5).
func writePendingRebootMarker() {
	_ = os.MkdirAll(config.DirLib, 0755)
	content := fmt.Sprintf("%d", time.Now().Unix())
	if err := sys.WriteAtomic(config.MarkerAutoUpdatePendingReboot, []byte(content), 0644); err != nil {
		sys.LogAction(ComponentName, "PENDING_REBOOT_WRITE_FAIL", "WARNING",
			i18n.TrEventf("autoupdate.pending_reboot_write_fail", err))
	}
}

// ============================================================================
// СОДЕРЖИМОЕ SYSTEMD-ЮНИТОВ (зашито в бинарник для декларативности)
// ============================================================================

const autoUpdateServiceUnit = `[Unit]
Description=Amber Guard Laboratory — Server AutoUpdate (oneshot)
Documentation=https://github.com/amberguard
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
Nice=10
IOSchedulingClass=idle
ExecStart=/opt/amberguard/agl --autoupdate
StandardOutput=journal
StandardError=journal

# ИБ-барьеры: oneshot не должен держать систему в заложниках
TimeoutStartSec=2400
`

const autoUpdateTimerUnit = `[Unit]
Description=Amber Guard Laboratory — Daily AutoUpdate Timer
Documentation=https://github.com/amberguard

[Timer]
# Ежедневно в 04:00 системного времени + случайный разброс до 1 часа
# (соседи по ДЦ не долбятся в зеркала Canonical одновременно).
OnCalendar=*-*-* 04:00:00
RandomizedDelaySec=3600
Persistent=true
Unit=agld_autoupdate.service

[Install]
WantedBy=timers.target
`

// enableAutoUpdateUnits записывает юниты, делает daemon-reload и запускает таймер.
func enableAutoUpdateUnits() error {
	servicePath := "/etc/systemd/system/agld_autoupdate.service"
	timerPath := "/etc/systemd/system/agld_autoupdate.timer"

	// Идемпотентность: если содержимое идентично — не переписываем (избегаем
	// лишних mtime, которые путают `systemctl daemon-reload`).
	if !fileContentEquals(servicePath, autoUpdateServiceUnit) {
		if err := sys.WriteAtomic(servicePath, []byte(autoUpdateServiceUnit), 0644); err != nil {
			sys.LogAction(ComponentName, "UNIT_WRITE_FAIL", "ERROR",
				i18n.TrEventf("autoupdate.unit_write_fail", servicePath, err))
			return err
		}
	}
	if !fileContentEquals(timerPath, autoUpdateTimerUnit) {
		if err := sys.WriteAtomic(timerPath, []byte(autoUpdateTimerUnit), 0644); err != nil {
			sys.LogAction(ComponentName, "UNIT_WRITE_FAIL", "ERROR",
				i18n.TrEventf("autoupdate.unit_write_fail", timerPath, err))
			return err
		}
	}

	// daemon-reload + enable + start timer
	reloadRes := sys.RunTimeout(10*time.Second, "systemctl", "daemon-reload")
	reloadRes.LogWarnIfErr(ComponentName, "DAEMON_RELOAD", "event=systemd.daemon_reload_fail")

	enableRes := sys.RunTimeout(10*time.Second, "systemctl", "enable", "--now", "agld_autoupdate.timer")
	if enableRes.Err != nil {
		sys.LogAction(ComponentName, "TIMER_ENABLE_FAIL", "ERROR",
			i18n.TrEventf("autoupdate.timer_enable_fail", enableRes.Err))
		return enableRes.Err
	}

	sys.LogAction(ComponentName, "UNITS_ENSURED", "SUCCESS",
		i18n.TrEvent("autoupdate.units_ensured"))
	return nil
}

// disableAutoUpdateUnits останавливает и удаляет юниты автообновления.
func disableAutoUpdateUnits() error {
	servicePath := "/etc/systemd/system/agld_autoupdate.service"
	timerPath := "/etc/systemd/system/agld_autoupdate.timer"

	// stop + disable (игнорируем ошибки — юнитов может уже не быть)
	_ = sys.RunTimeout(10*time.Second, "systemctl", "disable", "--now", "agld_autoupdate.timer")

	// Удаляем файлы
	_ = os.Remove(servicePath)
	_ = os.Remove(timerPath)

	reloadRes := sys.RunTimeout(10*time.Second, "systemctl", "daemon-reload")
	reloadRes.LogWarnIfErr(ComponentName, "DAEMON_RELOAD", "event=systemd.daemon_reload_fail")

	sys.LogAction(ComponentName, "UNITS_REMOVED", "SUCCESS",
		i18n.TrEvent("autoupdate.units_removed"))
	return nil
}

// fileContentEquals сверяет содержимое файла со строкой без чтения в память
// (для идемпотентности записи юнитов).
func fileContentEquals(path, expected string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return string(data) == expected
}
