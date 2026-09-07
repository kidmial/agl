// internal/service/backup_service.go
package service

import (
	"agl/internal/backup"
	"context"
)

// ============================================================================
// СЕРВИСНЫЙ СЛОЙ: РЕЗЕРВНОЕ КОПИРОВАНИЕ СИСТЕМЫ (BACKUP)
// ============================================================================
// PURPOSE: Единая точка входа для TUI к функции создания бэкапов.
// CONTRACT: Инкапсулирует вызов backup.RunSystemBackup.
//           Возвращает канал прогресса для отрисовки в TUI.
// SIDE-EFFECTS: Создаёт архивы в /backup, может занимать много места на диске.
// ============================================================================

// RunSystemBackup запускает процесс резервного копирования системы.
// Возвращает канал, через который TUI получает обновления прогресса.
func RunSystemBackup(ctx context.Context) (<-chan backup.BackupProgress, error) {
	progressChan := backup.RunSystemBackup()
	return progressChan, nil
}
