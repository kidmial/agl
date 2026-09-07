// internal/sys/auditor.go
package sys

import (
	"agl/config"
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

// LogAction осуществляет структурированную, потокобезопасную запись
// действий пользователя или критических системных триггеров в журнал аудита.
func LogAction(component, action, status, details string) {
	auditorMu.Lock()
	defer auditorMu.Unlock()

	// 1. Формируем временную метку по стандарту ISO
	timestamp := time.Now().Format("2006-01-02 15:04:05")

	// 2. Форматируем строку в строгий структурированный текст
	// Шаблон: YYYY-MM-DD HH:MM:SS [COMPONENT] [ACTION] -> [STATUS] | Details
	logLine := fmt.Sprintf("%s [%s] [%s] -> [%s] | %s\n",
		timestamp,
		component,
		action,
		status,
		details,
	)

	// 3. Гарантируем наличие папки (на случай удаления каталога в рантайме)
	_ = os.MkdirAll(filepath.Dir(logFilePath), 0755)

	// 4. Открываем файл в режиме дозаписи атомарно
	file, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		// Если системный I/O заблокирован, аварийно дублируем в stderr
		fmt.Fprintf(os.Stderr, "[CRITICAL AUDIT ERROR] Cannot write to log: %v. Line: %s", err, logLine)
		return
	}
	defer file.Close()

	// 5. Записываем строку на диск
	_, _ = file.WriteString(logLine)
}
