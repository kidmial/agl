// internal/backup/backup.go
package backup

import (
	"archive/tar"
	"compress/gzip"
	"context" // Каноничный импорт пакета контекста для изоляции системных процессов
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"agl/config"
)

// BackupProgress описывает стейт-контракт транзакции для Микро-Консоли TUI.
type BackupProgress struct {
	Percent     int
	CurrentFile string
	LogLine     string
	Done        bool
}

// МАССИВ №1: Системный контур восстановления (Только настройки программ, без логов)
// Полностью синхронизирован со спецификацией мажорной версии v6.0.0
var SystemTargetDirs = []string{
	"/var/www",
	"/etc/nginx",
	"/etc/apache2",
	"/etc/php",
	config.DirEtc, // Каноничный каталог конфигурации JSON (РЕБРЕНДИНГ)
	config.DirLib, // Директория персистентных маркеров рокировки и капканов (РЕБРЕНДИНГ)
	"/root/.ssh",
	"/etc/ufw",
	"/etc/default/ufw",
	"/etc/fail2ban",
}

// МАССИВ №2: Криминалистический контур лаборатории (Только SIEM-Логи).
var ForensicsLogFiles = []string{
	"/var/log/tarpit_connection.log",
	"/var/log/tarpit_report.log",
	"/var/log/tarpit_error.log",
	"/var/log/auth.log",
	"/var/log/fail2ban.log",
	"/var/log/ufw.log",
	config.FileMainLog, // Внедрение собственного лог-файла в контур бэкапа
}

// RunSystemBackup запускает асинхронную генерацию трех файлов с автоматической FIFO-ротацией.
func RunSystemBackup() chan BackupProgress {
	progressChan := make(chan BackupProgress, 100)

	go func() {
		defer close(progressChan)

		backupRoot := config.C.BackupRootDir
		if backupRoot == "" {
			backupRoot = "/backup"
		}

		progressChan <- BackupProgress{Percent: 1, LogLine: "[КОНТРОЛЬ]: Проверка свободного места на диске хоста..."}

		// Запуск алгоритма упреждающей FIFO-очистки диска при дефиците места
		if err := EnsureDiskSpaceSpaceFIFO(backupRoot, progressChan); err != nil {
			progressChan <- BackupProgress{Percent: 1, LogLine: "[ПРЕДУПРЕЖДЕНИЕ]: " + err.Error()}
		}

		currentTime := time.Now().Format("2006.01.02_15.04")
		// Жестко забиваем независимый внешний путь /backup
		sessionDir := filepath.Join(backupRoot, currentTime)
		if err := os.MkdirAll(sessionDir, 0700); err != nil {
			progressChan <- BackupProgress{Percent: 0, LogLine: "[СБОЙ]: Отказ создания директории сессии: " + err.Error()}
			return
		}

		// Имена ТРЕХ изолированных файлов внутри одной сессии
		sysArchiveName := fmt.Sprintf("%s_server_snapshot.tar.gz", currentTime)
		packageListName := fmt.Sprintf("%s_package_list.txt", currentTime)
		logArchiveName := fmt.Sprintf("%s_forensics_logs.tar.gz", currentTime)

		sysArchivePath := filepath.Join(sessionDir, sysArchiveName)
		packageListPath := filepath.Join(sessionDir, packageListName)
		logArchivePath := filepath.Join(sessionDir, logArchiveName)

		// -------------------------------------------------------------
		// ФАЙЛ 1: Экспорт списка пакетов apt
		// -------------------------------------------------------------
		progressChan <- BackupProgress{Percent: 5, LogLine: "[DPKG]: Экспорт списка пакетов apt для Шага 3..."}

		// Локальный изолированный контекст безопасности строго для dpkg
		ctxDpkg, cancelDpkg := context.WithTimeout(context.Background(), 30*time.Second)
		dpkgCmd := exec.CommandContext(ctxDpkg, "dpkg", "--get-selections")
		dpkgOut, err := dpkgCmd.Output()
		cancelDpkg() // Моментально освобождаем дескрипторы терминала хоста

		if err == nil {
			var filteredLines []string
			lines := strings.Split(string(dpkgOut), "\n")
			for _, line := range lines {
				if line == "" {
					continue
				}
				if !strings.Contains(line, "snoopy") && !strings.Contains(line, "iptables-persistent") &&
					!strings.Contains(line, "wireshark") && !strings.Contains(line, "kdump-tools") {
					filteredLines = append(filteredLines, line)
				}
			}
			_ = os.WriteFile(packageListPath, []byte(strings.Join(filteredLines, "\n")), 0600)
			progressChan <- BackupProgress{Percent: 15, LogLine: "[УСПЕХ]: Файл package_list.txt зафиксирован."}
		} else {
			progressChan <- BackupProgress{Percent: 15, LogLine: "[ОШИБКА DPKG]: Ошибка экспорта: " + err.Error()}
		}

		// -------------------------------------------------------------
		// ФАЙЛ 2: Монолитный архив настроек программ (0% - 60% прогресса TUI)
		// -------------------------------------------------------------
		progressChan <- BackupProgress{Percent: 20, LogLine: "[TAR]: Сборка Системного контура восстановления (server_snapshot)..."}
		if err := packTargetArray(SystemTargetDirs, sysArchivePath, 20, 60, progressChan); err != nil {
			progressChan <- BackupProgress{Percent: 20, LogLine: "[КРИТИЧЕСКИЙ СБОЙ SYS-TAR]: " + err.Error()}
			return
		}

		// Усиливаем права на архив: только root может читать.
		_ = os.Chmod(sysArchivePath, 0600)

		// -------------------------------------------------------------
		// ФАЙЛ 3: Выделенный архив SIEM-логов (60% - 98% прогресса TUI)
		// -------------------------------------------------------------
		progressChan <- BackupProgress{Percent: 60, LogLine: "[LOG-TAR]: Сборка Криминалистического контура (forensics_logs)..."}
		if err := packTargetArray(ForensicsLogFiles, logArchivePath, 60, 98, progressChan); err != nil {
			progressChan <- BackupProgress{Percent: 60, LogLine: "[КРИТИЧЕСКИЙ СБОЙ LOG-TAR]: " + err.Error()}
			return
		}

		// Усиливаем права на архив: только root может читать.
		_ = os.Chmod(logArchivePath, 0600)

		// Финальная точка: отправляем 100% и даем горутине закрыть канал через defer close
		progressChan <- BackupProgress{Percent: 100, LogLine: "[УСПЕХ]: Архивы сформированы. Операция завершена."}
	}()

	return progressChan
}

// EnsureDiskSpaceSpaceFIFO проверяет свободное место через системные вызовы и ротирует старые сессии бэкапов.
func EnsureDiskSpaceSpaceFIFO(backupRoot string, progressChan chan BackupProgress) error {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(backupRoot, &stat); err != nil {
		return fmt.Errorf("не удалось получить статистику fs: %w", err)
	}

	// Вычисляем процент свободного места на накопителе хоста
	allBlocks := stat.Blocks
	freeBlocks := stat.Bavail
	if allBlocks == 0 {
		return nil
	}
	percentFree := (float64(freeBlocks) / float64(allBlocks)) * 100.0

	// Если места достаточно (больше 15%) — ротация не требуется
	if percentFree >= 15.0 {
		return nil
	}

	progressChan <- BackupProgress{Percent: 1, LogLine: fmt.Sprintf("[ДИСК]: Дефицит места (Свободно: %.2f%%). Включение FIFO-ротации...", percentFree)}

	// Читаем директорию бэкапов для поиска старых сессионных папок
	entries, err := os.ReadDir(backupRoot)
	if err != nil {
		return fmt.Errorf("ошибка чтения корня бэкапов: %w", err)
	}

	var sessions []os.DirEntry
	for _, entry := range entries {
		if entry.IsDir() {
			sessions = append(sessions, entry)
		}
	}

	// Сортируем папки по дате изменения (старые — в начале среза)
	sort.Slice(sessions, func(i, j int) bool {
		infoI, _ := sessions[i].Info()
		infoJ, _ := sessions[j].Info()
		if infoI != nil && infoJ != nil {
			return infoI.ModTime().Before(infoJ.ModTime())
		}
		return sessions[i].Name() < sessions[j].Name()
	})

	// Последовательно удаляем старые сессии бэкапов, оставляя как минимум одну последнюю
	for len(sessions) > 1 {
		oldest := sessions[0]
		targetPath := filepath.Join(backupRoot, oldest.Name())

		progressChan <- BackupProgress{Percent: 1, LogLine: fmt.Sprintf("[FIFO]: Удаление старого архива сессии: %s", oldest.Name())}
		_ = os.RemoveAll(targetPath)

		// Перепроверяем метрики диска после удаления папки
		if errStat := syscall.Statfs(backupRoot, &stat); errStat == nil {
			if ((float64(stat.Bavail) / float64(stat.Blocks)) * 100.0) >= 15.0 {
				progressChan <- BackupProgress{Percent: 1, LogLine: "[FIFO]: Безопасный порог в 15% места успешно восстановлен."}
				return nil
			}
		}
		sessions = sessions[1:]
	}

	return fmt.Errorf("FIFO очистка завершена, но не удалось достичь 15%% свободного места")
}

func packTargetArray(targets []string, archivePath string, startPct, endPct int, progressChan chan BackupProgress) error {
	// Создаем физический файл архива в независимой директории /backup
	outArchive, err := os.Create(archivePath)
	if err != nil {
		return fmt.Errorf("не удалось создать файл архива: %w", err)
	}
	defer outArchive.Close()

	// Взводим каскад компрессии поверх файлового дескриптора
	gw := gzip.NewWriter(outArchive)
	tw := tar.NewWriter(gw)

	totalItems := len(targets)
	if totalItems == 0 {
		totalItems = 1
	}

	// Перебираем целевые объекты (конфигурации или журналы форензики) из массива
	for idx, target := range targets {
		target = strings.TrimSpace(target)
		if target == "" {
			continue
		}

		// Динамический расчет процента выполнения для Микро-Консоли TUI
		currentPct := startPct + int((float64(idx)/float64(totalItems))*(float64(endPct-startPct)))
		shortName := filepath.Base(target)

		// Отправляем текущий статус в живой поток логов
		progressChan <- BackupProgress{
			Percent:     currentPct,
			CurrentFile: shortName,
			LogLine:     fmt.Sprintf("[TAR] Сжатие объекта: %s ...", target),
			Done:        false,
		}

		// Предохранитель существования: если лог или конфиг службы отсутствует — мягко пропускаем
		_, err = os.Stat(target)
		if os.IsNotExist(err) {
			progressChan <- BackupProgress{
				Percent: currentPct,
				LogLine: fmt.Sprintf("[ПРОПУСК]: Объект %s отсутствует на сервере", target),
				Done:    false,
			}
			continue
		}

		// Запускаем рекурсивный обход вложенных каталогов файловой системы Ubuntu
		err = filepath.Walk(target, func(path string, fileInfo os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}

			// Генерируем внутренний заголовок TAR на основе слепка метаданных
			header, err := tar.FileInfoHeader(fileInfo, fileInfo.Name())
			if err != nil {
				return err
			}

			// Вычисляем чистый относительный путь для сохранения структуры при распаковке в корень /
			relPath, err := filepath.Rel(filepath.Dir(target), path)
			if err != nil {
				header.Name = filepath.ToSlash(path)
			} else {
				header.Name = filepath.ToSlash(relPath)
			}

			// Фиксируем метаданные в TAR-поток
			if err := tw.WriteHeader(header); err != nil {
				return err
			}
			// Если объект — каталог, переходим к следующему элементу
			if fileInfo.IsDir() {
				return nil
			}
			// Атомарно открываем файл для чтения байт
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()
			// Защита от динамического изменения файлов (паника write too long)
			limitedReader := io.LimitReader(file, header.Size)
			// Перекачиваем байты из дескриптора в архив через ограничитель
			_, err = io.Copy(tw, limitedReader)
			return err
		})
		if err != nil {
			_ = tw.Close()
			_ = gw.Close()
			return fmt.Errorf("сбой рекурсивной сборки %s: %w", target, err)
		}
	}
	// СИНХРОННЫЙ ФЛАШ ДИСКА: Явно закрываем писатели строго до передачи флага Done
	// Это принудительно вытолкнет последние байты и контрольные суммы архивов из RAM на диск!
	if err := tw.Close(); err != nil {
		return fmt.Errorf("ошибка фиксации TAR: %w", err)
	}
	if err := gw.Close(); err != nil {
		return fmt.Errorf("ошибка фиксации GZIP: %w", err)
	}
	return nil
}
