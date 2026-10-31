// internal/service/maintenance_service.go
package service

import (
	"agl/config"
	"agl/internal/maintenance"
	"agl/internal/sys"
	"fmt"
	"os/exec"
	"strings"
)

// SyncAutoUpdateUnits reconciles systemd units of server auto-update
// (agld_autoupdate.service / agld_autoupdate.timer) with the config declaration.
//
// PURPOSE: Bridge between TUI and the maintenance protocol per layering doctrine:
// TUI writes only the declaration (config.Update); unit reconciliation
// happens here, never in the presentation layer.
//
// CONTRACT: enabled=true  → write units, daemon-reload, enable+start timer;
//
//	enabled=false → disable+stop timer, remove units, daemon-reload.
//	Idempotent: identical unit files are not rewritten.
//
// SIDE-EFFECTS: Writes/removes /etc/systemd/system/agld_autoupdate.{service,timer};
// invokes systemctl via sys.RunTimeout.
func SyncAutoUpdateUnits(enabled bool) error {
	return maintenance.EnsureAutoUpdateUnits(enabled)
}

// SyncAnalyticsRebuild приводит факт к декларации загрузки истории.
// CONTRACT: если analytics_auto_rebuild включён, фаза загрузки истории
// обязана выполниться немедленно — перезапускаем watcher.
// При выключении ничего не делаем: декларация применится на следующем старте.
//
// Почему рестарт, а не живой reload:
//   - живой reload удваивает счётчики профилей (Store.Clear не трогает менеджер);
//   - рестарт бесшовен по данным: rebuild читает весь файл, включая окно рестарта.
func SyncAnalyticsRebuild() error {
	// Симметричная декларативность: любое переключение приводит витрину к факту.
	//   1 → рестарт + rebuild: витрина показывает полную историю из лога.
	//   0 → рестарт без rebuild: витрина показывает живой срез с момента переключения.
	// Данные не теряются: лог цел, включение тумблера вернёт полную историю.
	cmd := exec.Command("systemctl", "restart", "agld_watcher.service")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl restart agld_watcher: %v: %s",
			err, strings.TrimSpace(string(out)))
	}
	sys.LogAction("ANALYTICS", "REBUILD_SYNC", "INFO",
		fmt.Sprintf("Watcher restarted to apply analytics mode declaration (rebuild=%v)",
			config.C.AnalyticsAutoRebuild))
	return nil
}
