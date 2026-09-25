// internal/backup/backup.go
package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"       // Каноничный импорт пакета контекста для изоляции системных процессов
	"encoding/json" // <-- ДОБАВЛЕНО: для парсинга backup_targets.json
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
	"agl/i18n"
	"agl/internal/sys"
)

// BackupProgress описывает стейт-контракт транзакции для Микро-Консоли TUI.
type BackupProgress struct {
	Percent     int
	CurrentFile string
	LogLine     string
	Done        bool
}

// FileBackupTargets — путь к JSON-файлу конфигурации целей бэкапа.
const FileBackupTargets = config.DirEtc + "/backup_targets.json"

// BackupTargets описывает контракт JSON-файла конфигурации бэкапов.
type BackupTargets struct {
	Version    string   `json:"version"`
	SystemDirs []string `json:"system_dirs"`
	LogFiles   []string `json:"log_files"`
}

// defaultSystemDirs — дефолтные пути для системного архива (фолбэк).
var defaultSystemDirs = []string{
	"/var/www",
	"/etc/nginx",
	"/etc/apache2",
	"/etc/php",
	config.DirEtc,
	config.DirLib,
	"/root/.ssh",
	"/etc/ufw",
	"/etc/default/ufw",
	"/etc/fail2ban",
}

// defaultLogFiles — дефолтные пути для архива криминалистики (фолбэк).
var defaultLogFiles = []string{
	"/var/log/amberguard/tarpit_connection.log",
	"/var/log/amberguard/tarpit_report.log",
	"/var/log/amberguard/tarpit_error.log",
	"/var/log/auth.log",
	"/var/log/fail2ban.log",
	"/var/log/ufw.log",
	config.FileMainLog,
}

// LoadBackupTargets читает конфигурацию бэкапов из JSON.
// Если файл отсутствует — создаёт его с дефолтами (UX для админа).
// Если файл битый — логирует предупреждение и возвращает дефолты.
func LoadBackupTargets() BackupTargets {
	defaults := BackupTargets{
		Version:    "1.0",
		SystemDirs: defaultSystemDirs,
		LogFiles:   defaultLogFiles,
	}

	data, err := os.ReadFile(FileBackupTargets)
	if err != nil {
		// Файла нет — создаём его с дефолтами, чтобы админ видел, что бэкапится.
		if os.IsNotExist(err) {
			jsonData, _ := json.MarshalIndent(defaults, "", "  ")
			_ = os.WriteFile(FileBackupTargets, jsonData, 0644)
		}
		return defaults
	}

	var targets BackupTargets
	if err := json.Unmarshal(data, &targets); err != nil {
		// Файл битый — сохраняем оригинал как .bak и создаём дефолт.
		sys.LogAction("BACKUP", "TARGETS_INVALID", "WARNING", i18n.TrEventf("backup.targets_invalid", err))

		// Сохраняем битый файл как резервную копию
		bakPath := FileBackupTargets + ".bak"
		_ = os.Rename(FileBackupTargets, bakPath)

		// Создаём новый файл с дефолтными значениями
		jsonData, _ := json.MarshalIndent(defaults, "", "  ")
		_ = os.WriteFile(FileBackupTargets, jsonData, 0644)

		return defaults
	}

	// Валидация: если массивы пустые после парсинга — подставляем дефолты.
	if len(targets.SystemDirs) == 0 {
		targets.SystemDirs = defaultSystemDirs
	}
	if len(targets.LogFiles) == 0 {
		targets.LogFiles = defaultLogFiles
	}

	return targets
}

// RunSystemBackup запускает асинхронную генерацию трех файлов с автоматической FIFO-ротацией.
func RunSystemBackup() chan BackupProgress {
	progressChan := make(chan BackupProgress, 100)

	go func() {
		defer close(progressChan)
		sys.LogAction("BACKUP", "TRANSACTION_START", "INFO", i18n.TrEvent("backup.transaction_start"))
		backupRoot := config.C.BackupRootDir
		if backupRoot == "" {
			backupRoot = "/backup"
		}

		progressChan <- BackupProgress{Percent: 1, LogLine: i18n.T.BackupControlDiskCheck}

		// Запуск алгоритма упреждающей FIFO-очистки диска при дефиците места
		if err := EnsureDiskSpaceSpaceFIFO(backupRoot, progressChan); err != nil {
			progressChan <- BackupProgress{Percent: 1, LogLine: i18n.T.BackupWarningPrefix + err.Error()}
		}

		currentTime := time.Now().Format("2006.01.02_15.04")
		// Жестко забиваем независимый внешний путь /backup
		sessionDir := filepath.Join(backupRoot, currentTime)
		if err := os.MkdirAll(sessionDir, 0700); err != nil {
			progressChan <- BackupProgress{Percent: 0, LogLine: i18n.T.BackupFailSessionDirCreate + err.Error()}
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
		progressChan <- BackupProgress{Percent: 5, LogLine: i18n.T.BackupDpkgExport}

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
			progressChan <- BackupProgress{Percent: 15, LogLine: i18n.T.BackupSuccessPackageList}
		} else {
			progressChan <- BackupProgress{Percent: 15, LogLine: i18n.T.BackupErrorDpkgExport + err.Error()}
		}

		// -------------------------------------------------------------
		// ФАЙЛ 2: Монолитный архив настроек программ (0% - 60% прогресса TUI)
		// -------------------------------------------------------------

		// Загружаем цели бэкапа из JSON (или дефолты, если файла нет).
		targets := LoadBackupTargets()

		progressChan <- BackupProgress{Percent: 20, LogLine: i18n.T.BackupTarSysRecovery}
		if err := packTargetArray(targets.SystemDirs, sysArchivePath, 20, 60, progressChan); err != nil {
			sys.LogAction("BACKUP", "SYS_TAR_FAIL", "ERROR", i18n.TrEventf("backup.sys_tar_fail", err))
			progressChan <- BackupProgress{Percent: 20, LogLine: i18n.T.BackupCriticalSysTarFail + err.Error()}
			return
		}

		// Усиливаем права на архив: только root может читать.
		_ = os.Chmod(sysArchivePath, 0600)

		// -------------------------------------------------------------
		// ФАЙЛ 3: Выделенный архив SIEM-логов (60% - 98% прогресса TUI)
		// -------------------------------------------------------------
		progressChan <- BackupProgress{Percent: 60, LogLine: i18n.T.BackupLogTarForensics}
		if err := packTargetArray(targets.LogFiles, logArchivePath, 60, 98, progressChan); err != nil {
			sys.LogAction("BACKUP", "LOG_TAR_FAIL", "ERROR", i18n.TrEventf("backup.log_tar_fail", err))
			progressChan <- BackupProgress{Percent: 60, LogLine: i18n.T.BackupCriticalLogTarFail + err.Error()}
			return
		}

		// Усиливаем права на архив: только root может читать.
		_ = os.Chmod(logArchivePath, 0600)

		// Финальная точка: отправляем 100% и даем горутине закрыть канал через defer close
		sys.LogAction("BACKUP", "TRANSACTION_SUCCESS", "SUCCESS", i18n.TrEventf("backup.transaction_success", sessionDir))
		progressChan <- BackupProgress{Percent: 100, LogLine: i18n.T.BackupSuccessArchivesDone}
	}()

	return progressChan
}

// EnsureDiskSpaceSpaceFIFO проверяет свободное место через системные вызовы и ротирует старые сессии бэкапов.
func EnsureDiskSpaceSpaceFIFO(backupRoot string, progressChan chan BackupProgress) error {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(backupRoot, &stat); err != nil {
		return fmt.Errorf(i18n.T.BackupFsStatFailed, err)
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

	sys.LogAction("BACKUP", "DISK_DEFICIT", "WARNING", i18n.TrEventf("backup.disk_deficit", int(percentFree)))
	progressChan <- BackupProgress{Percent: 1, LogLine: fmt.Sprintf(i18n.T.BackupDiskDeficitFifo, percentFree)}

	// Читаем директорию бэкапов для поиска старых сессионных папок
	entries, err := os.ReadDir(backupRoot)
	if err != nil {
		return fmt.Errorf(i18n.T.BackupBackupRootReadErr, err)
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

		progressChan <- BackupProgress{Percent: 1, LogLine: fmt.Sprintf(i18n.T.BackupFifoDeleteOld, oldest.Name())}
		_ = os.RemoveAll(targetPath)

		// Перепроверяем метрики диска после удаления папки
		if errStat := syscall.Statfs(backupRoot, &stat); errStat == nil {
			if ((float64(stat.Bavail) / float64(stat.Blocks)) * 100.0) >= 15.0 {
				progressChan <- BackupProgress{Percent: 1, LogLine: i18n.T.BackupFifoSafeThreshold}
				return nil
			}
		}
		sessions = sessions[1:]
	}

	return fmt.Errorf("%s", i18n.T.BackupFifoFailThreshold)
}

func packTargetArray(targets []string, archivePath string, startPct, endPct int, progressChan chan BackupProgress) error {
	// Создаем физический файл архива в независимой директории /backup
	outArchive, err := os.Create(archivePath)
	if err != nil {
		return fmt.Errorf(i18n.T.BackupArchiveFileCreateFail, err)
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
			LogLine:     fmt.Sprintf(i18n.T.BackupTarCompressObject, target),
			Done:        false,
		}

		// Предохранитель существования: если лог или конфиг службы отсутствует — мягко пропускаем
		_, err = os.Stat(target)
		if os.IsNotExist(err) {
			progressChan <- BackupProgress{
				Percent: currentPct,
				LogLine: fmt.Sprintf(i18n.T.BackupSkipObjectMissing, target),
				Done:    false,
			}
			continue
		}

		// Запускаем рекурсивный обход вложенных каталогов файловой системы Ubuntu
		err = filepath.Walk(target, func(path string, fileInfo os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			// ФИКС v8.4.3: Для симлинков читаем РЕАЛЬНЫЙ target через os.Readlink.
			// fileInfo.Name() возвращает только имя файла, что создаёт самоссылающиеся
			// симлинки (rewrite.load -> rewrite.load) и ломает Apache/Nginx после restore.
			var linkTarget string
			if fileInfo.Mode()&os.ModeSymlink != 0 {
				if t, errLink := os.Readlink(path); errLink == nil {
					linkTarget = t
				}
			}
			header, err := tar.FileInfoHeader(fileInfo, linkTarget)
			if err != nil {
				return err
			}
			// ФИКС v8.4.1: Вычисляем относительный путь от корня файловой системы,
			// а не от родительской директории target. Это сохраняет полную структуру
			// путей в архиве: /etc/apache2 → etc/apache2, а не apache2.
			// При распаковке в корень / структура восстанавливается корректно.
			relPath, err := filepath.Rel("/", path)
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
			return fmt.Errorf(i18n.T.BackupRecursivePackFail, target, err)
		}
	}
	// СИНХРОННЫЙ ФЛАШ ДИСКА: Явно закрываем писатели строго до передачи флага Done
	// Это принудительно вытолкнет последние байты и контрольные суммы архивов из RAM на диск!
	if err := tw.Close(); err != nil {
		return fmt.Errorf(i18n.T.BackupTarFlushFail, err)
	}
	if err := gw.Close(); err != nil {
		return fmt.Errorf(i18n.T.BackupGzipFlushFail, err)
	}
	return nil
}
