// internal/analytics/globals.go
// =====================================================================
// КОМПОНЕНТ: ГЛОБАЛЬНАЯ ИНИЦИАЛИЗАЦИЯ АНALYTICS
// РОЛЬ: Инициализация глобальных переменных пакета analytics при старте.
// =====================================================================
package analytics

import (
	"fmt"
	"time"

	"agl/config"
	"agl/internal/sys"
)

// GlobalRollup — глобальные кольца агрегатной отчётности.
// (GlobalStore и GlobalManager уже объявлены в engine.go, не дублируем их здесь).
var GlobalRollup = &Rollup{}

// InitAnalytics инициализирует глобальные переменные аналитики.
// Вызывается из main.go ОДИН раз после config.LoadConfig().
func InitAnalytics(capacity int) {
	// Инициализируем Store, если ещё не создан.
	if GlobalStore == nil {
		if capacity <= 0 {
			capacity = DefaultStoreCapacity
		}
		GlobalStore = NewStore(capacity)
		sys.LogAction("ANALYTICS", "GLOBAL_STORE_INIT", "INFO", fmt.Sprintf("GlobalStore initialized with capacity %d", capacity))
	}

	// Инициализируем ProfileManager, если ещё не создан.
	if GlobalManager == nil {
		GlobalManager = NewProfileManager(GlobalStore)
		sys.LogAction("ANALYTICS", "GLOBAL_MANAGER_INIT", "INFO", "GlobalManager initialized and linked to GlobalStore")
	}

	// Устанавливаем витринную таймзону для колец отчётности.
	loc := time.Local
	timezoneLabel := "Local"
	if tz := config.C.WebPanelTimezone; tz != "" {
		if tz == "local" {
			// Уже time.Local
		} else if tz == "UTC" {
			loc = time.UTC
			timezoneLabel = "UTC"
		} else {
			if loaded, err := time.LoadLocation(tz); err == nil {
				loc = loaded
				timezoneLabel = loaded.String()
				if len(timezoneLabel) > 10 {
					timezoneLabel = timezoneLabel[:10]
				}
			}
		}
	}

	GlobalRollup.SetTimezone(loc)
	sys.LogAction("ANALYTICS", "GLOBAL_ROLLUP_TIMEZONE", "INFO", fmt.Sprintf("GlobalRollup timezone set to %s", timezoneLabel))
}
