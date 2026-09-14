// internal/service/armor_service.go
package service

import (
	"agl/i18n"
	"agl/internal/armor"
	"agl/internal/sys"
	"fmt"
	"strings"
	"time"
)

// ============================================================================
// СЕРВИСНЫЙ СЛОЙ: УПРАВЛЕНИЕ СЕТЕВОЙ БРОНЁЙ (ARMOR)
// ============================================================================
// PURPOSE: Единая точка входа для TUI к функциям управления бронёй периметра.
// CONTRACT: Инкапсулирует вызовы armor.ExecuteArmorRoute, SetSysctlProtocol.
//           Возвращает явные error, логирует события через sys.LogAction.
// SIDE-EFFECTS: Изменяет правила UFW, модифицирует sysctl параметры ядра.
// ============================================================================

// ToggleArmorRoute включает или выключает броню на заданном порту/сервисе.
// targetID может быть UUID из инвентаря или "port_auto_N" для авто-портов.
// action: "ON", "OFF", "CHECK".
func ToggleArmorRoute(targetID string, action string) error {
	err := armor.ExecuteArmorRoute(targetID, action)
	if err != nil {
		sys.LogAction("SERVICE_ARMOR", "ROUTE_FAIL", "ERROR",
			fmt.Sprintf("event=armor.route_failed target=%s action=%s err=%v", targetID, action, err))
		return fmt.Errorf(i18n.T.ArmorRouteFail, targetID, err)
	}
	sys.LogAction("SERVICE_ARMOR", "ROUTE_OK", "SUCCESS",
		fmt.Sprintf("event=armor.route_triggered target=%s action=%s", targetID, action))
	return nil
}

// ToggleSysctlProtocol применяет или откатывает оптимизации ядра Linux.
// action: "ON", "OFF", "CHECK".
func ToggleSysctlProtocol(action string) error {
	err := armor.SetSysctlProtocol(action)
	if err != nil {
		sys.LogAction("SERVICE_SYSCTL", "TOGGLE_FAIL", "ERROR",
			fmt.Sprintf("event=sysctl.toggle_failed action=%s err=%v", action, err))
		return fmt.Errorf(i18n.T.ArmorSysctlToggleFail, err)
	}
	sys.LogAction("SERVICE_SYSCTL", "TOGGLE_OK", "SUCCESS",
		fmt.Sprintf("event=sysctl.toggled action=%s", action))
	return nil
}

// SetCustomRateLimit управляет кастомным rate-limit правилом порта.
// PURPOSE: Единая точка входа TUI для индивидуальных лимитов рубежей.
// CONTRACT: Зеркалирует сигнатуру armor.SetCustomRateLimit:
// port — целевой порт; sysTag — системный тег рубежа;
// targetName — человекочитаемое имя службы; action — "ON"/"OFF"/"CHECK".
// SIDE-EFFECTS: Меняет правила UFW и парные квитанции в states.
func SetCustomRateLimit(port int, sysTag, targetName, action string) error {
	err := armor.SetCustomRateLimit(port, sysTag, targetName, action)
	if err != nil {
		sys.LogAction("SERVICE_ARMOR", "RATE_LIMIT_FAIL", "ERROR",
			fmt.Sprintf("event=armor.custom_rate_failed port=%d action=%s err=%v", port, action, err))
		return fmt.Errorf(i18n.T.ArmorRateLimitFail, port, err)
	}
	sys.LogAction("SERVICE_ARMOR", "RATE_LIMIT_OK", "SUCCESS",
		fmt.Sprintf("event=armor.custom_rate_set port=%d action=%s", port, action))
	return nil
}

// PurgeLimitRule принудительно удаляет rate-limit правило UFW для порта.
func PurgeLimitRule(port int) error {
	armor.PurgeLimitRule(port)
	sys.LogAction("SERVICE_ARMOR", "PURGE_LIMIT", "SUCCESS",
		fmt.Sprintf("event=armor.purge_limit port=%d", port))
	return nil
}

// CheckArmorRouteStatus проверяет статус брони для заданного targetID.
// PURPOSE: Тихий читатель для TUI (hot path, вызывается при каждой отрисовке).
// CONTRACT: Возвращает error, если броня активна (UFW правило найдено).
//
//	Возвращает nil, если броня неактивна.
//
// SIDE-EFFECTS: Нет (только чтение маркеров и UFW status).
func CheckArmorRouteStatus(targetID string) error {
	return armor.ExecuteArmorRoute(targetID, "CHECK")
}

// CheckSysctlProtocol проверяет статус оптимизаций ядра.
// PURPOSE: Тихий читатель для TUI.
// CONTRACT: Возвращает error, если sysctl не применены.
// SIDE-EFFECTS: Нет (только чтение /proc/sys).
func CheckSysctlProtocol() error {
	return armor.SetSysctlProtocol("CHECK")
}

// ReloadFirewall перезагружает правила UFW.
// PURPOSE: Командная функция для применения изменений в брандмауэре.
// CONTRACT: Вызывает `ufw reload` с таймаутом. Логирует событие.
// SIDE-EFFECTS: Перезагружает netfilter правила.
func ReloadFirewall() error {
	// Единый интерфейс внешних команд (Блок 3.1): ufw reload с таймаутом 30 сек.
	// Прямой exec.Command в сервисном слое запрещён контрактом 4.1.
	res := sys.RunTimeout(30*time.Second, "ufw", "reload")
	if res.Err != nil {
		sys.LogAction("SERVICE_FIREWALL", "RELOAD_FAIL", "ERROR",
			fmt.Sprintf("event=firewall.reload_failed err=%v stderr=%s",
				res.Err, strings.TrimSpace(res.Stderr)))
		return fmt.Errorf(i18n.T.ArmorUfwReloadFail, res.Err)
	}
	// После наката правил сбрасываем кэш-снепшот UFW, чтобы CHECK-функции
	// сразу видели свежую таблицу (иначе TUI покажет устаревший статус).
	armor.ResetUfwCacheSnapshot()
	sys.LogAction("SERVICE_FIREWALL", "RELOAD_OK", "SUCCESS",
		"event=firewall.reloaded")
	return nil
}

// ResetUfwCacheSnapshot сбрасывает кэш снимка UFW.
// PURPOSE: Командная функция для синхронизации состояния.
// CONTRACT: Вызывает armor.ResetUfwCacheSnapshot.
// SIDE-EFFECTS: Очищает in-memory кэш UFW.
func ResetUfwCacheSnapshot() {
	armor.ResetUfwCacheSnapshot()
	sys.LogAction("SERVICE_FIREWALL", "CACHE_RESET", "SUCCESS",
		"event=firewall.cache_reset")
}

// ToggleMultiTarpitProtocol включает или выключает мульти-ловушку.
// PURPOSE: Командная функция для управления декларативными ловушками.
// CONTRACT: Делегирует в armor.SetMultiTarpitProtocol.
// SIDE-EFFECTS: Меняет правила UFW и маркеры states.
func ToggleMultiTarpitProtocol(port int, action string) error {
	err := armor.SetMultiTarpitProtocol(port, action)
	if err != nil {
		sys.LogAction("SERVICE_TARPIT", "MULTI_TOGGLE_FAIL", "ERROR",
			fmt.Sprintf("event=tarpit.multi_toggle_failed port=%d action=%s err=%v", port, action, err))
		return fmt.Errorf(i18n.T.ArmorMultiTarpitToggleFail, port, err)
	}
	sys.LogAction("SERVICE_TARPIT", "MULTI_TOGGLE_OK", "SUCCESS",
		fmt.Sprintf("event=tarpit.multi_toggled port=%d action=%s", port, action))
	return nil
}

// CheckTarpitProtocol проверяет статус дегтярной ямы.
// PURPOSE: Тихий читатель для TUI.
// CONTRACT: Возвращает error, если демон не жив или маркер отсутствует.
// SIDE-EFFECTS: Нет (только чтение маркеров и IPC).
func CheckTarpitProtocol() error {
	return armor.SetTarpitProtocol("CHECK")
}

// IsRescueTLSReady проверяет готовность TLS-сертификата канала спасения.
// PURPOSE: Тихий читатель для TUI.
// CONTRACT: Возвращает true, если сертификат на месте.
// SIDE-EFFECTS: Нет (только os.Stat).
func IsRescueTLSReady() bool {
	return armor.RescueTLSReady()
}
