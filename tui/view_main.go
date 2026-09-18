// tui/view_main.go
package tui

import (
	"agl/config"
	"agl/i18n"
	"agl/internal/buildinfo"
	"agl/internal/service"
	"agl/internal/sys"
	"agl/internal/tarpit"
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
)

// Глобальные переменные для расчета дельты процессора
var prevIdle, prevTotal uint64

// ───────────────────────────────────────────────────────────────
// БЛОК 7: КЭШИ ДЛЯ СТАТИСТИКИ ГЛАВНОГО ЭКРАНА (защита от I/O)
// ────────────────────────────────────────────────────────────────
var (
	// Кэш статистики (обновляется раз в 30 сек)
	statsCacheMu   sync.Mutex
	statsCacheTime time.Time
	statsCacheData struct {
		logProgSize   int64
		logConnSize   int64
		logRptSize    int64
		snapshotTime  time.Time
		snapshotCount int
		activeTraps   int
		totalTraps    int
		openPorts     int
	}

	// Кэш логов (обновляется только при изменении размера файла)
	logCacheMu    sync.Mutex
	logCacheSize  int64
	logCacheLines []string
)

// ─────────────────────────────────────────────────────────────────
// ГЛАВНЫЙ ЭКРАН v8.1.3 — СТАТИКА И КЭШ
// ─────────────────────────────────────────────────────────────────
var (
	// Статика CPU: модель, ядра, потоки. Парсится ОДИН раз при старте.
	cpuModelCached   string
	cpuCoresCached   int
	cpuThreadsCached int
	cpuInfoOnce      sync.Once

	// Кэш статусов юнитов и UFW. Обновляется раз в 10 сек,
	// чтобы тикер главного меню (1 сек) не дёргал systemctl/ufw.
	unitCacheMu sync.Mutex
	unitCacheAt time.Time
	unitUfwOn   bool
	// unitTarpitOn  bool
	unitWatcherOn bool

	// v8.2.2 (3.1-F2): раздельные edge-triggered флаги деградации зондов.
	// UFW и systemd-watcher — независимые внешние команды.
	// Общий флаг был бы некорректен: восстановление одной команды
	// ложно закрывало бы деградацию другой.
	// Гонок нет: чтение/запись выполняется только под unitCacheMu в refreshUnitCache.
	ufwDegraded     bool
	watcherDegraded bool
)

// loadCpuInfoOnce парсит /proc/cpuinfo один раз при старте процесса.
// Извлекает: модель (первая встреча "model name"), физ. ядра ("cpu cores"),
// логические потоки (количество строк "processor : N").
func loadCpuInfoOnce() {
	cpuInfoOnce.Do(func() {
		file, err := os.Open("/proc/cpuinfo")
		if err != nil {
			cpuModelCached = i18n.T.ViewMainFallbackUnknownCpu
			cpuCoresCached = 1
			cpuThreadsCached = 1
			return
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		threads := 0
		coresSet := make(map[int]bool)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "model name") && cpuModelCached == "" {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) == 2 {
					cpuModelCached = strings.TrimSpace(parts[1])
				}
			} else if strings.HasPrefix(line, "processor") {
				threads++
			} else if strings.HasPrefix(line, "core id") {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) == 2 {
					if v, err := strconv.Atoi(strings.TrimSpace(parts[1])); err == nil {
						coresSet[v] = true
					}
				}
			}
		}
		cpuThreadsCached = threads
		cpuCoresCached = len(coresSet)
		if cpuCoresCached == 0 {
			cpuCoresCached = threads // fallback для виртуалок без core id
		}
		if cpuModelCached == "" {
			cpuModelCached = i18n.T.ViewMainFallbackUnknownCpu
		}
	})
}

// refreshUnitCache обновляет статусы юнитов/UFW раз в 10 сек.
// Тикер главного меню (1 сек) больше не дёргает systemctl.
func refreshUnitCache() {
	unitCacheMu.Lock()
	defer unitCacheMu.Unlock()
	// v8.2.3: интервал обновления кэша юнитов уменьшен с 10 до 3 секунд.
	// Это даёт более оперативную реакцию на изменение состояния сервисов
	// (например, остановку демона ловушек) без существенной нагрузки на systemctl.
	if time.Since(unitCacheAt) < 3*time.Second {
		return
	}
	// UFW: ищем "Status: active" в выводе.
	// БЛОК 3.1: единый интерфейс sys.RunTimeout; сбой не игнорируется,
	// но и не спамит: один WARNING на переход в деградацию и один SUCCESS на восстановление.
	ufwRes := sys.RunTimeout(3*time.Second, "ufw", "status")
	if ufwRes.Err != nil {
		// Команда недоступна, упала или превысила таймаут.
		// Индикатор в TUI оставляем последним известным, чтобы не мигать.
		// Логируем только первое событие сбоя, не на каждый тик кэша.
		if !ufwDegraded {
			ufwDegraded = true
			ufwRes.LogWarnIfErr("TUI", "UNIT_CACHE", "event=ufw.status_failed")
		}
	} else {
		// Команда отвечает: обновляем факт по наличию строки активного статуса.
		unitUfwOn = strings.Contains(ufwRes.Stdout, "Status: active")

		// Если до этого зонд был в деградации — закрываем событие одним SUCCESS.
		if ufwDegraded {
			ufwDegraded = false
			sys.LogAction("TUI", "UNIT_CACHE", "SUCCESS",
				i18n.TrEvent("ufw.status_restored"))
		}
	}

	// Юнит вотчера.
	// Здесь тоже отдельный флаг: сбой/остановка вотчера не должна
	// маскировать сбой UFW и наоборот.
	watcherRes := sys.RunTimeout(3*time.Second, "systemctl", "is-active", "agld_watcher.service")

	// v8.2.3 (hotfix): `systemctl is-active` возвращает exit code 3 и печатает
	// "inactive" для остановленного юнита. Это НОРМАЛЬНЫЙ результат опроса,
	// а не деградация зонда.
	//
	// ПРЕЖНИЙ БАГ: ветка Err != nil не обновляла unitWatcherOn, поэтому после
	// остановки вотчера (обратная рокировка) индикатор WEB WATCHER залипал
	// в устаревшем состоянии ON до перезахода в панель.
	//
	// НОВАЯ СЕМАНТИКА: ground truth — печатный статус, независимо от exit-кода:
	//   - статус непустой → зонд здоров, обновляем unitWatcherOn;
	//   - статус пустой И Err != nil → команда реально недоступна (деградация).
	watcherStatus := strings.TrimSpace(watcherRes.Stdout)
	if watcherStatus == "" {
		// Страховка: некоторые сборки systemd могут увести состояние в stderr.
		watcherStatus = strings.TrimSpace(watcherRes.Stderr)
	}

	if watcherStatus != "" {
		// systemd ответил содержательно: active / inactive / failed / activating.
		// Юнит считаем включённым только при точном совпадении "active".
		// Exit code 3 с печатью "inactive" сюда же — это штатный ответ.
		unitWatcherOn = watcherStatus == "active"

		// Если ранее зонд был в деградации — закрываем событие одним SUCCESS.
		if watcherDegraded {
			watcherDegraded = false
			sys.LogAction("TUI", "UNIT_CACHE", "SUCCESS",
				i18n.TrEvent("systemd.watcher_probe_restored"))
		}
	} else if watcherRes.Err != nil {
		// Нет ни stdout, ни stderr — таймаут, команда недоступна, systemd сломан.
		// Индикатор оставляем последним известным, чтобы не мигать.
		// Анти-спам: пишем только переход в деградацию, не каждый тик кэша.
		if !watcherDegraded {
			watcherDegraded = true
			watcherRes.LogWarnIfErr("TUI", "UNIT_CACHE", "event=systemd.watcher_probe_failed")
		}
	}
	unitCacheAt = time.Now()
}

// getLinuxCpuPercent парсит /proc/stat и вычисляет чистый процент нагрузки CPU
func getLinuxCpuPercent() int {
	file, err := os.Open("/proc/stat")
	if err != nil {
		return 0
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	if scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 5 || fields[0] != "cpu" {
			return 0
		}

		var total uint64 = 0
		var idle uint64 = 0
		for i, field := range fields[1:] {
			val, _ := strconv.ParseUint(field, 10, 64)
			total += val
			if i == 3 { // Индекс idle в /proc/stat
				idle = val
			}
		}

		if prevTotal == 0 {
			prevIdle = idle
			prevTotal = total
			return 0
		}

		diffIdle := idle - prevIdle
		diffTotal := total - prevTotal

		prevIdle = idle
		prevTotal = total

		if diffTotal == 0 {
			return 0
		}
		return int(100 - (diffIdle * 100 / diffTotal))
	}
	return 0
}

// getLinuxMemoryMetrics парсит /proc/meminfo и возвращает (RAM%, RAM_Text, Swap%, Swap_Text)
func getLinuxMemoryMetrics() (int, string, int, string) {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, i18n.T.ViewMainFallbackError, 0, i18n.T.ViewMainFallbackError
	}
	defer file.Close()

	var memTotal, memAvailable, swapTotal, swapFree uint64
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			memTotal, _ = strconv.ParseUint(fields[1], 10, 64)
		case "MemAvailable:":
			memAvailable, _ = strconv.ParseUint(fields[1], 10, 64)
		case "SwapTotal:":
			swapTotal, _ = strconv.ParseUint(fields[1], 10, 64)
		case "SwapFree:":
			swapFree, _ = strconv.ParseUint(fields[1], 10, 64)
		}
	}

	// Расчет RAM
	ramPercent := 0
	ramText := i18n.T.ViewMainFallbackZeroMb
	if memTotal > 0 {
		ramUsed := memTotal - memAvailable
		ramPercent = int(ramUsed * 100 / memTotal)
		ramText = fmt.Sprintf("%d %s / %d %s", ramUsed/1024, i18n.T.UnitMB, memTotal/1024, i18n.T.UnitMB)
		// ← Строка 282 УДАЛЕНА
	}

	// Расчет Swap
	swapPercent := 0
	swapText := i18n.T.ViewMainFallbackZeroMb
	if swapTotal > 0 {
		swapUsed := swapTotal - swapFree
		swapPercent = int(swapUsed * 100 / swapTotal)
		swapText = fmt.Sprintf("%d %s / %d %s", swapUsed/1024, i18n.T.UnitMB, swapTotal/1024, i18n.T.UnitMB) // ← v8.3.0: i18n
	}

	return ramPercent, ramText, swapPercent, swapText
}

// updateStatsCache обновляет статистику, если прошло > 30 сек.
func updateStatsCache() {
	statsCacheMu.Lock()
	defer statsCacheMu.Unlock()

	if time.Since(statsCacheTime) < 30*time.Second {
		return // Кэш ещё свежий
	}

	// 1. Размеры логов (os.Stat — быстро)
	if info, err := os.Stat(config.FileMainLog); err == nil {
		statsCacheData.logProgSize = info.Size()
	}
	if info, err := os.Stat(config.FileTarpitConnLog); err == nil {
		statsCacheData.logConnSize = info.Size()
	}
	if info, err := os.Stat(config.FileTarpitRptLog); err == nil {
		statsCacheData.logRptSize = info.Size()
	}

	// 2. Снапшоты (readdir /backup)
	backupDir := "/backup"
	if entries, err := os.ReadDir(backupDir); err == nil {
		count := 0
		var latest time.Time
		for _, entry := range entries {
			if entry.IsDir() {
				count++
				if info, err := entry.Info(); err == nil {
					if info.ModTime().After(latest) {
						latest = info.ModTime()
					}
				}
			}
		}
		statsCacheData.snapshotCount = count
		statsCacheData.snapshotTime = latest
	}

	// 3. Ловушки из инвентаря
	if inv, err := sys.LoadTarpitInventory(sys.TarpitInventoryPath); err == nil {
		total := 0
		active := 0
		for _, trap := range inv.Traps {
			total++
			if trap.Status == "ACTIVE" {
				active++
			}
		}
		statsCacheData.totalTraps = total
		statsCacheData.activeTraps = active
	}

	// 4. Открытые порты из периметра
	if apps, err := config.LoadDetectedApps(); err == nil {
		count := 0
		for _, app := range apps {
			if app.IsActive && app.Port > 0 {
				count++
			}
		}
		statsCacheData.openPorts = count
	}

	statsCacheTime = time.Now()
}

// updateLogCache читает хвост amberguard.log, только если файл вырос.
func updateLogCache() {
	logCacheMu.Lock()
	defer logCacheMu.Unlock()

	logPath := config.FileMainLog
	info, err := os.Stat(logPath)
	if err != nil {
		return
	}

	currentSize := info.Size()

	// Если размер не изменился — возвращаем кэш
	if currentSize == logCacheSize && len(logCacheLines) > 0 {
		return
	}

	// Файл вырос — читаем последние 3 строки
	logCacheLines = readLastLines(logPath, 3)
	logCacheSize = currentSize
}

// readLastLines читает последние N строк из файла.
func readLastLines(path string, n int) []string {
	file, err := os.Open(path)
	if err != nil {
		return []string{i18n.T.ViewMainFallbackError}
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
		if len(lines) > n {
			lines = lines[1:] // Удаляем старую строку
		}
	}

	if len(lines) == 0 {
		return []string{i18n.T.EmptyLog}
	}

	return lines
}

// DrawMainMenuGrid компонует сетку виджетов Главного Меню Сервера (v8.1.3).
// Редизайн: убраны три Gauge-полосы → два смысловых блока текста.
// Блок A — СИСТЕМНЫЙ ПАСПОРТ (CPU, RAM, Swap).
// Блок B — СЛУЖБЫ КОМПЛЕКСА (SSH, UFW, яма, вотчер, боты, ловушки, порты).
func DrawMainMenuGrid(selectedRow int) ([]ui.Drawable, int) {
	// Загружаем статику CPU (один раз за процесс)
	loadCpuInfoOnce()

	// Считываем динамические метрики
	cpuUsage := getLinuxCpuPercent()
	ramPercent, ramText, swapPercent, swapText := getLinuxMemoryMetrics()

	// Обновляем кэш статусов юнитов (раз в 3 сек)
	refreshUnitCache()

	// Собираем статистику СРАЗУ, чтобы использовать данные в блоке служб
	updateStatsCache()
	statsCacheMu.Lock()
	activeTraps := statsCacheData.activeTraps
	totalTraps := statsCacheData.totalTraps
	openPorts := statsCacheData.openPorts
	statsCacheMu.Unlock()

	// Состояние SSH и кастлинга
	activeSshPort := sys.DetectActiveSshPort()
	castlingLabel := fmt.Sprintf("[[ %s ]](fg:red,mod:bold)", i18n.T.StatusOff)
	if _, err := os.Stat(config.MarkerSshMoved); err == nil {
		castlingLabel = fmt.Sprintf("[[ %s ] → %d](fg:green,mod:bold)", i18n.T.StatusOn, config.C.RealSshPort)
	}

	// Счётчик ботов из IPC-моста
	liveBots := tarpit.GetLiveBotsShmCount()

	// M3f (v8.1.3): индикатор TLS-шлюза спасения
	tlsLabel := fmt.Sprintf("[[ %s ]](fg:red)", i18n.T.StatusOff)
	if service.IsRescueTLSReady() {
		tlsLabel = fmt.Sprintf("[[ %s ]](fg:green,mod:bold)", i18n.T.StatusOn)
	}

	// ─────────────────────────────────────────────────────────────
	// 1. ШАПКА (3 строки: y: 1-4)
	// ─────────────────────────────────────────────────────────────
	timeStr := time.Now().Format(i18n.T.TimeFormat)
	dateStr := time.Now().Format(i18n.T.DateFormat)
	header := widgets.NewParagraph()
	header.Title = fmt.Sprintf(" Amber Guard Laboratory - %s ", buildinfo.Version)
	header.Text = fmt.Sprintf(i18n.T.ViewMainHeaderTpl,
		i18n.T.ViewMainLabelMainMenu, i18n.T.ViewMainLabelDate, dateStr,
		i18n.T.ViewMainLabelTime, timeStr)
	header.SetRect(2, 1, 102, 4)
	header.BorderStyle.Fg = ui.ColorCyan
	header.TitleStyle.Fg = ui.ColorYellow
	header.TitleStyle.Modifier = ui.ModifierBold

	// ─────────────────────────────────────────────────────────────
	// 2. БЛОК A: СИСТЕМНЫЙ ПАСПОРТ (4 строки: y: 4-8)
	// ─────────────────────────────────────────────────────────────
	sysPassport := widgets.NewParagraph()
	sysPassport.Title = i18n.T.ViewMainPassportTitle
	sysPassport.Text = fmt.Sprintf(i18n.T.ViewMainPassportTpl,
		i18n.T.ViewMainLabelCpu, trunc(cpuModelCached, 40), cpuCoresCached, cpuThreadsCached,
		i18n.T.ViewMainLabelLoad, cpuUsage,
		i18n.T.ViewMainLabelRam, ramText, i18n.T.ViewMainLabelUsage, ramPercent,
		i18n.T.ViewMainLabelSwap, swapText, i18n.T.ViewMainLabelUsage, swapPercent)
	sysPassport.SetRect(2, 4, 102, 8)
	sysPassport.BorderStyle.Fg = ui.ColorCyan
	sysPassport.TitleStyle.Fg = ui.ColorYellow
	sysPassport.TitleStyle.Modifier = ui.ModifierBold

	// ─────────────────────────────────────────────────────────────
	// 3. БЛОК B: СЛУЖБЫ КОМПЛЕКСА (4 строки: y: 8-12)
	//    Теперь включает: Ловушки (акт/всего) и Открытые порты
	// ─────────────────────────────────────────────────────────────
	servicesBlock := widgets.NewParagraph()
	servicesBlock.Title = i18n.T.ViewMainServicesTitle
	ufwLabel := fmt.Sprintf("[[ %s ]](fg:red,mod:bold)", i18n.T.StatusOff)
	if unitUfwOn {
		ufwLabel = fmt.Sprintf("[[ %s ]](fg:green,mod:bold)", i18n.T.StatusOn)
	}
	tarpitLabel := fmt.Sprintf("[[ %s ]](fg:red,mod:bold)", i18n.T.StatusOff)
	if _, errT := os.Stat(config.TarpitMarkerPath(22)); errT == nil {
		tarpitLabel = fmt.Sprintf("[[ %s ]](fg:green,mod:bold)", i18n.T.StatusOn)
	}
	watcherLabel := fmt.Sprintf("[[ %s ]](fg:red,mod:bold)", i18n.T.StatusOff)
	if unitWatcherOn {
		watcherLabel = fmt.Sprintf("[[ %s ]](fg:green,mod:bold)", i18n.T.StatusOn)
	}

	servicesBlock.Text = fmt.Sprintf(i18n.T.ViewMainServicesTpl,
		i18n.T.ViewMainLabelWebWatcher, watcherLabel, ufwLabel, activeSshPort,
		i18n.T.ViewMainLabelCastling, castlingLabel,
		i18n.T.ViewMainLabelTarpit, tarpitLabel, i18n.T.ViewMainLabelTlsGate, tlsLabel,
		i18n.T.ViewMainLabelBotsTrapped, liveBots,
		i18n.T.StatsTrapsActive, activeTraps, totalTraps, i18n.T.StatsOpenPorts, openPorts)

	servicesBlock.SetRect(2, 8, 102, 12)
	servicesBlock.BorderStyle.Fg = ui.ColorCyan
	servicesBlock.TitleStyle.Fg = ui.ColorYellow
	servicesBlock.TitleStyle.Modifier = ui.ModifierBold
	servicesBlock.WrapText = false

	// ─────────────────────────────────────────────────────────────
	// 4. БЛОК C: КРАТКАЯ СТАТИСТИКА (4 строки: y: 12-16)
	//    Сокращён на 1 строку (убраны боты, перенесены ловушки/порты)
	// ─────────────────────────────────────────────────────────────
	statsCacheMu.Lock()
	progSizeStr := formatLogSize(statsCacheData.logProgSize)
	connSizeStr := formatLogSize(statsCacheData.logConnSize)
	rptSizeStr := formatLogSize(statsCacheData.logRptSize)

	snapshotAgo := i18n.T.StatsNever
	if !statsCacheData.snapshotTime.IsZero() {
		d := time.Since(statsCacheData.snapshotTime)
		if d.Minutes() < 60 {
			snapshotAgo = fmt.Sprintf(i18n.T.StatsMinutesAgo, int(d.Minutes()))
		} else if d.Hours() < 24 {
			snapshotAgo = fmt.Sprintf(i18n.T.StatsHoursAgo, int(d.Hours()))
		} else {
			snapshotAgo = fmt.Sprintf(i18n.T.StatsDaysAgo, int(d.Hours()/24))
		}
	}
	snapshotCount := statsCacheData.snapshotCount
	statsCacheMu.Unlock()

	// ── СОЗДАНИЕ ВИДЖЕТА ──
	statsBlock := widgets.NewParagraph()
	statsBlock.Title = i18n.T.StatsBlockTitle
	// ──────────────────────────────────────────

	statsBlock.Text = fmt.Sprintf(
		" %s: %s (%s) + %s (%s) + %s (%s)\n"+
			" %s: %s │ %s: %d",
		i18n.T.StatsLabelLogs,
		progSizeStr, i18n.T.StatsLogProgram,
		connSizeStr, i18n.T.StatsLogConnection,
		rptSizeStr, i18n.T.StatsLogReport,
		i18n.T.StatsLastSnapshot, snapshotAgo,
		i18n.T.StatsBackupCount, snapshotCount)

	// Высота 4: 1 (заголовок) + 2 (текст) + 1 (нижняя граница)
	statsBlock.SetRect(2, 12, 102, 16)
	statsBlock.BorderStyle.Fg = ui.ColorCyan
	statsBlock.TitleStyle.Fg = ui.ColorYellow
	statsBlock.TitleStyle.Modifier = ui.ModifierBold
	statsBlock.WrapText = false

	// ─────────────────────────────────────────────────────────────
	// 5. МЕНЮ РАЗДЕЛОВ (9 строк: y: 16-25)
	//    Сдвинуто вверх на 1 строку благодаря оптимизации блока C
	// ─────────────────────────────────────────────────────────────
	menuList := widgets.NewList()
	menuList.Title = i18n.T.ViewMainMenuTitle
	menuList.Rows = []string{
		fmt.Sprintf(" SETTINGS      │ %s", i18n.T.ViewMainRowSettings),
		fmt.Sprintf(" ANTIDDOS      │ %s", i18n.T.ViewMainRowAntiddos),
		fmt.Sprintf(" TARPIT        │ %s", i18n.T.ViewMainRowTarpit),
		fmt.Sprintf(" MULTI TARPIT  │ %s", i18n.T.ViewMainRowMultiTarpit),
		fmt.Sprintf(" PORT MAP      │ %s", i18n.T.ViewMainRowPortMap),
		fmt.Sprintf(" BACKUP        │ %s", i18n.T.ViewMainRowBackup),
		fmt.Sprintf(" EXIT          │ %s", i18n.T.ViewMainRowExit),
	}
	menuList.SetRect(2, 16, 102, 25)
	menuList.BorderStyle.Fg = ui.ColorCyan
	menuList.SelectedRow = selectedRow
	menuList.SelectedRowStyle = ui.NewStyle(ui.ColorBlack, ui.ColorGreen, ui.ModifierBold)

	// ─────────────────────────────────────────────────────────────
	// 6. БЛОК D: ЛОГИ КОМПЛЕКСА (3 строки: y: 25-30)
	//    Сдвинуто вверх, теперь вмещает 3 строки логов + заголовок + границы
	// ─────────────────────────────────────────────────────────────
	updateLogCache()

	logCacheMu.Lock()
	logLines := make([]string, len(logCacheLines))
	copy(logLines, logCacheLines)
	logCacheMu.Unlock()

	// Обрезаем строки до 96 символов (102 - 6 служебных)
	for i := range logLines {
		// Используем rune для правильного подсчёта символов (не байт!)
		runes := []rune(logLines[i])
		if len(runes) > 96 {
			logLines[i] = string(runes[:93]) + "..."
		}
	}

	logBlock := widgets.NewParagraph()
	logBlock.Title = i18n.T.StatsLogBlockTitle
	logBlock.Text = " " + strings.Join(logLines, "\n ")
	logBlock.SetRect(2, 25, 102, 30)
	logBlock.BorderStyle.Fg = ui.ColorCyan
	logBlock.TitleStyle.Fg = ui.ColorYellow
	logBlock.TitleStyle.Modifier = ui.ModifierBold
	logBlock.WrapText = false

	return []ui.Drawable{header, sysPassport, servicesBlock, statsBlock, menuList, logBlock}, len(menuList.Rows)
}

// formatLogSize возвращает человеко-читаемый размер файла.
// Если размер < 1 МБ — показывает в КБ, иначе в МБ с одним знаком после запятой.
func formatLogSize(bytes int64) string {
	if bytes < 1024*1024 {
		return fmt.Sprintf("%d %s", bytes/1024, i18n.T.UnitKB)
	}
	return fmt.Sprintf("%.1f %s", float64(bytes)/(1024*1024), i18n.T.UnitMB)
}

// trunc обрезает строку до maxLen символов с многоточием при превышении.
func trunc(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "…"
}
