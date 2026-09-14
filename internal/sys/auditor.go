// internal/sys/auditor.go
// v8.3.0 (6.6-а): реструктуризация журнала аудита под машинные коды событий.
//
// АРХИТЕКТУРА ТРЁХ СЛОЁВ:
//   1. logActionCore  — низкоуровневая запись (замок, файл, форматирование).
//   2. LogActionEvent — целевой публичный API с машинным кодом события.
//   3. LogAction      — обёртка совместимости для существующих вызовов.
//
// ФОРМАТ ЗАПИСИ (задача 6.4):
//   2026-09-07 12:00:00 [COMPONENT] [ACTION] -> [STATUS] | event=CODE | локализованное описание
//
// Если машинный код пуст (старые вызовы через обёртку) — пишем:
//   2026-09-07 12:00:00 [COMPONENT] [ACTION] -> [STATUS] | details
//
// Это гарантирует, что строка журнала НИКОГДА не пуста (А.2),
// а существующие вызовы продолжают работать без изменений.

package sys

import (
	"agl/config"
	"agl/i18n"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	// auditorMu защищает файл журнала от одновременной конкурентной записи
	auditorMu sync.Mutex
	// logFilePath определяет каноничное системное расположение лога бренда
	logFilePath = config.FileMainLog
)

// logActionCore — низкоуровневая запись в журнал аудита.
// НЕ вызывать напрямую из бизнес-кода. Используется только из
// LogActionEvent и LogAction.
//
// PURPOSE: инкапсулировать замок, открытие файла, форматирование и запись,
// чтобы оба публичных входа делили одну механику без дублирования.
//
// CONTRACT: вызывается только с уже сформированной строкой logLine.
// Сам лог не формирует — только пишет.
//
// SIDE-EFFECTS: пишет в logFilePath; при сбое I/O дублирует в stderr.
func logActionCore(logLine string) {
	auditorMu.Lock()
	defer auditorMu.Unlock()

	// 1. Гарантируем наличие папки (на случай удаления каталога в рантайме)
	_ = os.MkdirAll(filepath.Dir(logFilePath), 0755)

	// 2. Открываем файл в режиме дозаписи
	file, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		// Если системный I/O заблокирован, аварийно дублируем в stderr
		fmt.Fprintf(os.Stderr, "[CRITICAL AUDIT ERROR] Cannot write to log: %v. Line: %s", err, logLine)
		return
	}
	defer file.Close()

	// 3. Записываем строку на диск
	_, _ = file.WriteString(logLine)
}

// LogActionEvent — целевой публичный API журнала аудита (задача 6.6).
//
// Сигнатура: компонент, действие, статус, машинный код, детали.
//
// ФОРМАТ ВЫВОДА:
//   - С кодом:    ... | event=castling.complete | Рокировка портов завершена
//   - Без кода:   ... | <детали> (совместимость со старыми вызовами)
//
// ЛОКАЛИЗАЦИЯ:
//   - Локализованное описание берётся из i18n.TrEvent(eventCode, args...).
//   - Если код пуст или не найден — используется details как fallback (А.2).
//   - Строка журнала НИКОГДА не пуста.
//
// ПРИМЕР ВЫЗОВА:
//
//	sys.LogActionEvent("PORT_CASTLING", "CASTLING_COMPLETE", "SUCCESS",
//	    sys.EventCastlingComplete, "порт 22 → 2244")
func LogActionEvent(component, action, status, eventCode, details string) {
	// 1. Временная метка по стандарту ISO
	timestamp := time.Now().Format("2006-01-02 15:04:05")

	// 2. Формируем строку в зависимости от наличия машинного кода
	var logLine string
	if eventCode != "" {
		// Берём локализованное описание события из активного языкового пакета.
		// Если код не найден в L.Events, TrEvent вернёт сам код (А.2, фолбэк).
		localized := i18n.TrEvent(eventCode)

		// Если details не пусты — добавляем их как контекст после описания.
		// Это позволяет сохранить динамические данные (порты, IP, ошибки)
		// без дублирования в переводе.
		if details != "" {
			logLine = fmt.Sprintf("%s [%s] [%s] -> [%s] | event=%s | %s | %s\n",
				timestamp, component, action, status, eventCode, localized, details)
		} else {
			logLine = fmt.Sprintf("%s [%s] [%s] -> [%s] | event=%s | %s\n",
				timestamp, component, action, status, eventCode, localized)
		}
	} else {
		// Старый формат без машинного кода — полная совместимость.
		// Используется обёрткой LogAction и немаркированными вызовами.
		logLine = fmt.Sprintf("%s [%s] [%s] -> [%s] | %s\n",
			timestamp, component, action, status, details)
	}

	// 3. Делегируем запись низкоуровневому слою
	logActionCore(logLine)
}

// LogAction — обёртка совместимости для существующих вызовов.
//
// Сигнатура сохранена: (component, action, status, details).
// Внутри вызывает LogActionEvent с пустым машинным кодом,
// что даёт формат без префикса "event=".
//
// МИГРАЦИЯ (задача 6.7): по директориям вызовы переводятся на
// LogActionEvent с реальным машинным кодом. После закрытия всех
// директорий эта обёртка удаляется, а LogActionEvent
// переименовывается в LogAction (закрытие задачи 6.6 в букве).
//
// НЕ ДОБАВЛЯТЬ новых вызовов через эту обёртку — только миграция.
func LogAction(component, action, status, details string) {
	LogActionEvent(component, action, status, "", details)
}
