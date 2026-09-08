// internal/armor/ssh_limit_protocol.go
package armor

import (
	"agl/config"
	"agl/i18n"
	"agl/internal/sys" // Глобальный хаб утилит детекции и АУДИТОР v6.4.0
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// SetSshLimitProtocol динамически защищает актуальный порт легитимного OpenSSH в ОЗУ.
// Выступает интеллектуальным прокси-контроллером для универсального шлюза лимитов v6.0.0.
func SetSshLimitProtocol(action string) error {
	action = strings.ToUpper(action)

	// На лету вычисляем актуальный рабочий порт OpenSSH в системе
	targetPort := config.C.RealSshPort
	if targetPort == 0 {
		if activePortStr := sys.DetectActiveSshPort(); activePortStr != "" {
			targetPort, _ = strconv.Atoi(activePortStr)
		}
	}
	if targetPort == 0 {
		targetPort = 22 // Безопасный фолбэк по умолчанию
	}

	switch action {
	case "ON":
		// ЛОГИЧЕСКИЙ ЗАМОК: Если лимитер запрашивается на базовом порту 22, но там активен капкан — блокируем
		if targetPort == 22 && SetTarpitProtocol("CHECK") == nil {
			// АУДИТ: Фиксируем попытку опасной коллизии подсистем безопасности
			sys.LogAction("SSH_LIMIT", "COLLISION_LOCK", "ALERT",
				i18n.TrEvent("ssh_limit.collision_lock"))
			return fmt.Errorf("%s", i18n.T.SshLimitCollisionLockError)
		}

		// АУДИТ: Логируем, какой именно порт уходит под защиту лимитера пакетов
		sys.LogAction("SSH_LIMIT", "TARGET_PORT_RESOLVE", "SUCCESS",
			i18n.TrEventf("ssh_limit.target_port_resolve", targetPort))

		// Делегируем выполнение монолитному исполнительному движку Netfilter/UFW
		return SetCustomRateLimit(targetPort, "open_ssh", "Open SSH", "ON")

	case "OFF":
		return SetCustomRateLimit(targetPort, "open_ssh", "Open SSH", "OFF")

	case "CHECK":
		// Проверяем летучий ОЗУ-маркер по новой формуле Amber Guard Lab
		// v8.1.1 (M0b / K-30): маркер лимитера живёт в энергонезависимой зоне
		// `states` (DirLib), куда его пишет SetCustomRateLimit. Чтение из
		// летучего DirRun было рассинхроном — устранено.
		runtimeMarker := filepath.Join(config.DirLib, fmt.Sprintf("agl_armor_ratelimit_port.%d", targetPort))
		if _, err := os.Stat(runtimeMarker); err == nil {
			return nil // Лимитер активен
		}
		return fmt.Errorf("%s", i18n.T.SshLimitDisabled)
	}

	return fmt.Errorf("%s", fmt.Sprintf(i18n.T.SshLimitUnknownAction, action))
}
