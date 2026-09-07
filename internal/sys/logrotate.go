// internal/sys/logrotate.go
package sys

import (
	"os"
	"path/filepath"
)

const (
	LogrotateConfigPath = "/etc/logrotate.d/amberguard"
	LogrotateConfigBody = `/var/log/amberguard/*.log {
    weekly
    rotate 4
    size 10M
    compress
    delaycompress
    missingok
    notifempty
    create 0644 root root
}`
)

// EnsureLogrotate проверяет наличие системного конфига ротации логов.
// Если файла нет, он создается автоматически.
func EnsureLogrotate() error {
	// 1. Проверяем, существует ли уже конфигурация
	if _, err := os.Stat(LogrotateConfigPath); err == nil {
		// Конфиг уже на месте, ничего делать не нужно
		return nil
	}

	// 2. Убедимся, что родительская директория существует (на случай кастомных сред)
	dir := filepath.Dir(LogrotateConfigPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// 3. Записываем промышленный конфиг ротации логов
	err := os.WriteFile(LogrotateConfigPath, []byte(LogrotateConfigBody), 0644)
	if err != nil {
		return err
	}

	return nil
}
