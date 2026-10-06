// internal/service/rescue_service.go
package service

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"agl/config"
	"agl/i18n"
	"agl/internal/armor"
	"agl/internal/sys"
)

// SyncRescueChannelState приводит факт канала спасения к декларации v8.5.0.
// PURPOSE: Единственная точка входа TUI в синхронизацию трёх опций (A, B, C).
// CONTRACT:
//   - Опция A (SshAllowInFromIp): постоянная дверь админа (whitelist IP).
//   - Опция B (SshIpWebKnock): веб-стук с испепелением. Требует A (валидация в TUI).
//   - Опция C (WebHttpsPanelActive): декларативна, без побочных эффектов (панель в будущем).
//   - G2: при выключении обеих защит и активной рокировке восстанавливает правило
//     перенесённого порта из маркера MarkerMovedRuleType.
//
// SIDE-EFFECTS: UFW-правила и маркеры меняются внутри пакета armor.
// SyncRescueChannelState — ЯВНЫЙ путь (сохранение в SETTINGS): полная сходимость,
// включая создание постоянной двери админа. Декларация подтверждена намерением.
func SyncRescueChannelState() error {
	return syncRescueChannel(true)
}

// ReconcileRescueChannel — АВТОМАТИЧЕСКИЙ путь (вход в TUI, контур 1.7):
// сходимость только в безопасную сторону. Дверь НЕ создаётся: сверка не вправе
// додумывать намерение за админа. Снятие двери, деактивация канала,
// пург/восстановление правила перенесённого порта — выполняются.
func ReconcileRescueChannel() error {
	return syncRescueChannel(false)
}

// syncRescueChannel приводит факт канала спасения к декларации v8.5.1.
// CONTRACT: allowCreate=true — создание двери разрешено (явное намерение админа);
//
// allowCreate=false — только снятие и восстановление (безопасная сторона).
// SIDE-EFFECTS: UFW-правила и маркеры меняются внутри пакета armor.
func syncRescueChannel(allowCreate bool) error {
	// v8.5.0: жизненный цикл юнита вотчера следует за физическим сервером.
	armor.SyncWatcherUnit()

	castling := armor.MarkerExists(config.MarkerSshMoved)
	marker := armor.MarkerExists(config.MarkerRescueActive)

	// --- Опция A: постоянная дверь админа (whitelist IP) ---
	// Создаётся ТОЛЬКО по явному намерению (SETTINGS). Удаляется всегда при A=false:
	// асимметрия обоснована безопасностью — лишняя дверь риск, отсутствующая лечится стуком.
	if config.C.SshAllowInFromIp {
		if allowCreate {
			armor.CreateAdminDoor()
		}
	} else {
		armor.RemoveAdminDoor()
	}

	// --- Опция B: веб-стук с испепелением (требует A + рокировку) ---
	// Канал спасения активен только при: рокировка ∧ сервер ∧ опция B.
	// Опция A уже проверена валидатором TUI (B требует A).
	rescueDeclared := castling && config.C.SshIpWebKnock && config.C.WebServerEnabled

	switch {
	case rescueDeclared && !marker:
		armor.ActivateRescueChannel()
	case !rescueDeclared && marker:
		armor.DeactivateRescueChannel()
	}

	// --- Зачистка правила перенесённого порта при включении защиты ---
	// При включении опции A или B правило порта 22 (allow/limit/reject)
	// должно быть удалено, иначе оно остаётся и пропускает трафик наравне с дверью.
	if castling && (config.C.SshAllowInFromIp || config.C.SshIpWebKnock) {
		purgeMovedPortRule()
	}

	// --- G2: восстановление правила перенесённого порта ---
	// При выключении обеих защит (A=false ∧ B=false) и активной рокировке
	// правило перенесённого порта обязано быть восстановлено из маркера.
	// Иначе админ теряет внешний доступ к серверу через скрытый порт.
	if !config.C.SshAllowInFromIp && !config.C.SshIpWebKnock && castling {
		restoreMovedPortRule()
	}

	return nil
}

// restoreMovedPortRule восстанавливает правило перенесённого порта из маркера G2.
// PURPOSE: при выключении обеих защит правило порта 22 (allow/limit/reject)
// возвращается на скрытый порт, чтобы админ не потерял внешний доступ.
// CONTRACT: читает MarkerMovedRuleType; при отсутствии маркера — самовосстановление:
//
//	(а) правило живо → маркер перезаписывается с его типа, восстанавливать нечего;
//	(б) правила тоже нет → консервативный фолбэк "limit" + WARNING
//	    (доступ сохранён, брутфорс троттлится: ни локаута, ни широкого allow).
//	Маркер НЕ удаляется: он нужен каждому последующему циклу защит.
//
// SIDE-EFFECTS: модифицирует UFW; может записать маркер.
func restoreMovedPortRule() {
	port := armor.ReadMovedPort()
	if port == "" {
		return
	}
	ruleType := readMovedRuleTypeMarker()
	if ruleType == "" {
		// Самовосстановление (а): правило живо — маркер рождается из его типа.
		if detected := detectMovedRuleType(port); detected != "" {
			_ = os.WriteFile(config.MarkerMovedRuleType, []byte(detected), 0644)
			return
		}
		// Самовосстановление (б): ни маркера, ни правила — консервативный фолбэк.
		ruleType = "limit"
		sys.LogAction("RESCUE", "MOVED_RULE_TYPE_FALLBACK", "WARNING",
			i18n.TrEventf("rescue.moved_rule_type_fallback", port))
	}
	ufwRes := sys.RunTimeout(10*time.Second, "ufw", ruleType, port+"/tcp",
		"comment", "Amber Guard Lab. SSH Moved Port "+port)
	if ufwRes.Err != nil {
		ufwRes.LogWarnIfErr("RESCUE", "MOVED_RULE_RESTORE", "event=ufw.moved_rule_restore_failed")
		return
	}
	sys.LogAction("RESCUE", "MOVED_RULE_RESTORED", "SUCCESS",
		i18n.TrEventf("rescue.moved_rule_restored", port, ruleType))
}

// readMovedRuleTypeMarker читает и валидирует маркер типа правила перенесённого порта.
// CONTRACT: возвращает пустую строку при отсутствии файла или невалидном содержимом.
// SIDE-EFFECTS: нет (чистое чтение).
func readMovedRuleTypeMarker() string {
	data, err := os.ReadFile(config.MarkerMovedRuleType)
	if err != nil {
		return ""
	}
	t := strings.TrimSpace(string(data))
	switch t {
	case "allow", "limit", "reject", "deny":
		return t
	}
	return ""
}

// detectMovedRuleType парсит ufw status для живого правила перенесённого порта.
// PURPOSE: самовосстановление маркера на хостах, где маркер не родился или погиб.
// CONTRACT: матчит сигнатуру «SSH Moved Port НОМЕР» гранично по порту.
// SIDE-EFFECTS: нет (только чтение статуса UFW).
func detectMovedRuleType(port string) string {
	statusRes := sys.RunTimeout(5*time.Second, "ufw", "status", "numbered")
	if statusRes.Err != nil {
		return ""
	}
	sigMoved := "Amber Guard Lab. SSH Moved Port " + port
	for _, line := range strings.Split(statusRes.Stdout, "\n") {
		if strings.Contains(line, port+"/tcp") && strings.Contains(line, sigMoved) {
			switch {
			case strings.Contains(line, "LIMIT"):
				return "limit"
			case strings.Contains(line, "ALLOW"):
				return "allow"
			case strings.Contains(line, "REJECT"):
				return "reject"
			case strings.Contains(line, "DENY"):
				return "deny"
			}
		}
	}
	return ""
}

// purgeMovedPortRule удаляет правило перенесённого порта при включении защиты.
// PURPOSE: при включении опции A или B правило порта 22 (allow/limit/reject)
// должно быть удалено, иначе оно остаётся и пропускает трафик наравне с дверью.
// CONTRACT: удаляет все правила с сигнатурой "SSH Moved Port НОМЕР" для скрытого порта.
// SIDE-EFFECTS: модифицирует UFW; удаляет маркер типа после успешной зачистки.
func purgeMovedPortRule() {
	port := armor.ReadMovedPort()
	if port == "" {
		return
	}
	statusRes := sys.RunTimeout(5*time.Second, "ufw", "status", "numbered")
	if statusRes.Err != nil {
		statusRes.LogWarnIfErr("RESCUE", "MOVED_RULE_PURGE", "event=ufw.status_failed")
		return
	}
	count := 0
	sigMoved := fmt.Sprintf("Amber Guard Lab. SSH Moved Port %s", port)
	lines := strings.Split(statusRes.Stdout, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		if !strings.Contains(line, sigMoved) || !strings.Contains(line, port+"/tcp") {
			continue
		}
		numStr := extractRuleNumber(line)
		if numStr == "" {
			continue
		}
		delRes := sys.RunTimeout(10*time.Second, "ufw", "--force", "delete", numStr)
		if delRes.Err != nil {
			delRes.LogWarnIfErr("RESCUE", "MOVED_RULE_PURGE", "event=ufw.moved_rule_delete_failed")
			continue
		}
		count++
	}
	if count > 0 {
		sys.LogAction("RESCUE", "MOVED_RULE_PURGED", "SUCCESS",
			i18n.TrEventf("rescue.moved_rule_purged", port, count))
	}
	// v8.5.1 (A2′): включение защиты сносит ТОЛЬКО правила открытого доступа
	// (надгробия). LIMIT не трогаем: броня сосуществует с защитой по осознанному
	// выбору админа (A1′) — под атакой лимит бережёт RAM сервера.
	portInt, _ := strconv.Atoi(port)
	armor.PurgeTombstone(portInt)
}

// extractRuleNumber извлекает номер правила из строки `ufw status numbered` ("[ 12]").
func extractRuleNumber(line string) string {
	idx := strings.Index(line, "[")
	if idx == -1 {
		return ""
	}
	end := strings.Index(line[idx:], "]")
	if end == -1 {
		return ""
	}
	return strings.TrimSpace(line[idx+1 : idx+end])
}
