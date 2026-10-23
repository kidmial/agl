// internal/analytics/engine.go
// =====================================================================
// КОМПОНЕНТ: ГЛОБАЛЬНЫЙ ДВИЖОК АНАЛИТИКИ
// РОЛЬ: Единая точка инициализации и запуска инжеста.
// ПРИНЦИП:
//   - инициализация происходит один раз (sync.Once);
//   - устойчив к ошибкам (не падает, если файл лога отсутствует);
//   - потокобезопасный доступ из любого места комплекса.
//
// =====================================================================
package analytics

import (
	"fmt"
	"os"
	"sync"

	"agl/config"
	"agl/internal/sys"
)

var (
	// GlobalStore — глобальное хранилище событий.
	GlobalStore *Store

	// GlobalManager — глобальный менеджер профилей акторов.
	GlobalManager *ProfileManager

	// engineOnce гарантирует однократную инициализацию.
	engineOnce sync.Once
)

// Start инициализирует движок и запускает фоновый инжест лога.
// Безопасен для многократного вызова (гарантирует однократный запуск).
//
// Аргументы:
//   - logPath: путь к файлу лога (если пустой, берётся из конфига).
func Start(logPath string) {
	engineOnce.Do(func() {
		// 1. Инициализация структур данных
		GlobalStore = NewStore(DefaultStoreCapacity)
		GlobalManager = NewProfileManager(GlobalStore)

		sys.LogAction("ANALYTICS", "ENGINE_INIT", "INFO",
			"Analytics structures initialized (Store + ProfileManager)")

		// 2. Определяем путь к логу
		if logPath == "" {
			logPath = config.C.TarpitRptLog
		}
		if logPath == "" {
			logPath = "/var/log/amberguard/tarpit_report.log"
		}

		sys.LogAction("ANALYTICS", "RESOLVED_PATH", "INFO",
			fmt.Sprintf("Итоговый путь к логу для инжеста: '%s'", logPath))

		// 3. Проверяем существование файла перед запуском инжеста
		if _, err := os.Stat(logPath); err != nil {
			// Файл не существует — это нормально для первого запуска.
			// Логируем INFO и НЕ запускаем инжест.
			sys.LogAction("ANALYTICS", "INGEST_SKIP", "INFO",
				fmt.Sprintf("Log file not found, ingest deferred: %s", logPath))
			return
		}

		// 4. Запускаем инжест только если файл существует
		StartIngest(DefaultIngestConfig(logPath))

		// 5. Автозагрузка истории (v8.6.2)
		// Если флаг включён, загружаем всю историю из лога.
		// Это происходит ПОСЛЕ запуска инжеста, чтобы новые события
		// не дублировались (инжест читает хвост, rebuild читает всё с начала).
		if config.C.AnalyticsAutoRebuild {
			sys.LogAction("ANALYTICS", "AUTO_REBUILD_START", "INFO",
				"Auto-rebuild enabled, loading history from log")

			result, err := RebuildHistory(logPath)
			if err != nil {
				sys.LogAction("ANALYTICS", "AUTO_REBUILD_FAIL", "ERROR",
					fmt.Sprintf("Failed to load history: %v", err))
			} else if result != nil {
				sys.LogAction("ANALYTICS", "AUTO_REBUILD_DONE", "INFO",
					fmt.Sprintf("History loaded: %d lines, %d valid, %d broken",
						result.LinesRead, result.Valid, result.Broken))
			} else {
				// result == nil означает, что история уже загружена (guard сработал)
				sys.LogAction("ANALYTICS", "AUTO_REBUILD_SKIP", "INFO",
					"History already loaded (guard triggered)")
			}
		}

		sys.LogAction("ANALYTICS", "ENGINE_STARTED", "INFO",
			fmt.Sprintf("Analytics engine and ingest started for: %s", logPath))
	})
}

// ResetEngine сбрасывает состояние движка (ТОЛЬКО ДЛЯ ТЕСТОВ!).
// Вызывать в продакшене ЗАПРЕЩЕНО.
func ResetEngine() {
	engineOnce = sync.Once{}
	GlobalStore = nil
	GlobalManager = nil
}
