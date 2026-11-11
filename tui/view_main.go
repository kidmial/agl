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
	"bytes"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
)

// Дельта CPU: состояние предыдущего среза /proc/stat.
// Под мьютексом: метрику может запросить не только тикер TUI, но и фоновый
// сборщик — без замка предыдущий срез перезатирается и процент врёт.
var (
	cpuSampleMu sync.Mutex
	prevIdle    uint64
	prevTotal   uint64
)

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

	// Кэш статусов юнитов и UFW. Обновляется раз в 3 сек,
	// чтобы тикер главного меню (1 сек) не дёргал systemctl/ufw.
	unitCacheMu   sync.Mutex
	unitCacheAt   time.Time
	unitUfwOn     bool
	unitTarpitOn  bool
	unitWatcherOn bool

	// ГЛАВНЫЙ ЭКРАН — СТАТИКА И КЭШ
	ufwDegraded     bool
	watcherDegraded bool
)

// loadCpuInfoOnce парсит /proc/cpuinfo один раз при старте процесса.
// Извлекает: модель (первая встреча "model name"), физ. ядра (уникальные "core id"),
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

		// Hardening: если /proc/cpuinfo оборвался (строка > 64 КБ — лимит bufio.Scanner),
		// не записываем в кэш мусор с 0 ядер/0 потоков.
		if err := scanner.Err(); err != nil && threads == 0 {
			cpuModelCached = i18n.T.ViewMainFallbackUnknownCpu
			cpuCoresCached = 1
			cpuThreadsCached = 1
			return
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

// ─────────────────────────────────────────────────────────────
// БЛОК 7.2: РУЧНАЯ СЕТКА ШАПКИ (v9.0.0) — ПОСИМВОЛЬНАЯ ПОДГОНКА
// ─────────────────────────────────────────────────────────────
// PURPOSE: каждая ячейка шапки имеет ПОЛНУЮ фиксированную видимую ширину
// (метка + значение + воздух). Границы посадочных мест (позиции "|")
// считаются накопительно и НЕ зависят от длины меток и значений:
type headerRow1Grid struct {
	Cpu      int // вся ячейка "CPU: Intel(R) Xeon… 2.30GHz"
	Cores    int // вся ячейка "1x1" (без метки)
	Load     int // вся ячейка "Нагрузка:  35%" (БЕЗ скобок: head + руны)
	SshAdmin int // МАКС. ширина крайней ячейки "SSH Админ IP: [ ON ]" (буфер урежет сам)
}
type headerRow2Grid struct {
	Ram   int // вся ячейка "RAM: 373/961 МБ"
	Usage int // вся ячейка "Занято: 38%" (БЕЗ скобок: head + руны)
	Ports int // вся ячейка "NETWORK (открытых портов):    2" (БЕЗ скобок: head + руны)
	Pass  int // МАКС. ширина крайней ячейки "Пароли: [ ● ] [ ● ]" (буфер урежет сам)
}

var headerGrid = struct {
	Row1 headerRow1Grid
	Row2 headerRow2Grid
}{
	// Вывод строк верхнего блока
	Row1: headerRow1Grid{Cpu: 47, Cores: 5, Load: 16, SshAdmin: 21},
	Row2: headerRow2Grid{Ram: 20, Usage: 14, Ports: 34, Pass: 22},
}

// headerInnerWidth — полезная ширина строки шапки: SetRect(2,1,102,5)
// даёт Dx=100, минус 2 руны границ. Всё, что больше, termui обрежет троеточием.
const headerInnerWidth = 98

// headerLayoutWarned — edge-triggered флаг: один WARN на событие переполнения,
// тикер шапки (1 сек) не спамит лог. Сбрасывается при возврате в бюджет.
var headerLayoutWarned bool

// warnHeaderOverflow сверяет ширины строк шапки с бюджетом headerInnerWidth.
func warnHeaderOverflow(w1, w2 int) {
	over := 0
	switch {
	case w1 > headerInnerWidth && w2 > headerInnerWidth:
		over = 3
	case w1 > headerInnerWidth:
		over = 1
	case w2 > headerInnerWidth:
		over = 2
	}
	if over == 0 {
		headerLayoutWarned = false // возврат в бюджет: событие закрыто
		return
	}
	if headerLayoutWarned {
		return
	}
	headerLayoutWarned = true
	sys.LogAction("TUI", "HEADER_LAYOUT", "WARN", fmt.Sprintf(
		"event=header.row_overflow rows=%d width1=%d width2=%d budget=%d hint=headerGrid",
		over, w1, w2, headerInnerWidth))
}

// systemdUnitStates — словарь штатных ответов `systemctl is-active`.
// Всё, что вне словаря (в т.ч. текст ошибки в stderr вида
// "Failed to connect to bus"), — НЕ статус, а деградация зонда.
var systemdUnitStates = map[string]bool{
	"active": true, "reloading": true, "inactive": true,
	"failed": true, "activating": true, "deactivating": true,
}

// probeUnitStatus извлекает печатный статус юнита из результата зонда.
// Возвращает (статус, probeOK). probeOK = true только если печатный статус
// входит в словарь systemd — в stdout либо (страховка, как прежде) в stderr.
// Текст ошибки в stderr больше не маскируется под статус юнита.
func probeUnitStatus(stdout, stderr string) (string, bool) {
	if s := strings.TrimSpace(stdout); systemdUnitStates[s] {
		return s, true
	}
	if s := strings.TrimSpace(stderr); systemdUnitStates[s] {
		return s, true
	}
	return "", false
}

// refreshUnitCache обновляет статусы юнитов/UFW раз в 3 сек.
// Тикер главного меню (1 сек) больше не дёргает systemctl.
func refreshUnitCache() {
	unitCacheMu.Lock()
	defer unitCacheMu.Unlock()
	// Интервал 3 сек: оперативная реакция на остановку сервисов
	// без существенной нагрузки на systemctl.
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

	// Ground truth — печатный статус из словаря systemdUnitStates (см. probeUnitStatus):
	// exit-код не участвует в решении; "inactive" при code 3 — штатный ответ.
	watcherStatus, watcherProbeOK := probeUnitStatus(watcherRes.Stdout, watcherRes.Stderr)

	// Юнит демона ловушек.
	tarpitRes := sys.RunTimeout(3*time.Second, "systemctl", "is-active", "agld_tarpit.service")
	tarpitStatus, tarpitProbeOK := probeUnitStatus(tarpitRes.Stdout, tarpitRes.Stderr)
	if tarpitProbeOK {
		unitTarpitOn = tarpitStatus == "active"
	}
	// Зонд недоступен (таймаут, systemd сломан): держим последнее известное
	// состояние — индикатор не должен мигать.

	if watcherProbeOK {
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
	} else {
		// Ни в stdout, ни в stderr нет валидного статуса: таймаут, команда
		// недоступна, systemd сломан. Индикатор оставляем последним известным.
		// Анти-спам: пишем только переход в деградацию, не каждый тик кэша.
		if !watcherDegraded {
			watcherDegraded = true
			if watcherRes.Err != nil {
				watcherRes.LogWarnIfErr("TUI", "UNIT_CACHE", "event=systemd.watcher_probe_failed")
			} else {
				sys.LogAction("TUI", "UNIT_CACHE", "WARN",
					"event=systemd.watcher_probe_empty hint=no_unit_state_in_stdout_stderr")
			}
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

		cpuSampleMu.Lock()
		defer cpuSampleMu.Unlock()

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
	var memFree, memBuffers, memCached uint64
	hasMemAvailable := false
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
			hasMemAvailable = true
		case "MemFree:":
			memFree, _ = strconv.ParseUint(fields[1], 10, 64)
		case "Buffers:":
			memBuffers, _ = strconv.ParseUint(fields[1], 10, 64)
		case "Cached:":
			memCached, _ = strconv.ParseUint(fields[1], 10, 64)
		case "SwapTotal:":
			swapTotal, _ = strconv.ParseUint(fields[1], 10, 64)
		case "SwapFree:":
			swapFree, _ = strconv.ParseUint(fields[1], 10, 64)
		}
	}

	// Hardening: если чтение /proc/meminfo было прервано (например, строка > 64 КБ —
	// лимит bufio.Scanner), возвращаем фоллбэк, а не частичные данные.
	if err := scanner.Err(); err != nil {
		return 0, i18n.T.ViewMainFallbackError, 0, i18n.T.ViewMainFallbackError
	}

	// Fallback для ядер < 3.14 без MemAvailable: available ≈ Free+Buffers+Cached.
	// Без этого RAM на таких хостах показывалась бы загруженной на 100%.
	if !hasMemAvailable && memTotal > 0 {
		memAvailable = memFree + memBuffers + memCached
		if memAvailable > memTotal {
			memAvailable = memTotal
		}
	}

	// Расчет RAM
	ramPercent := 0
	ramText := i18n.T.ViewMainFallbackZeroMb
	if memTotal > 0 {
		ramUsed := memTotal - memAvailable
		ramPercent = int(ramUsed * 100 / memTotal)
		// v9.0.0: КОМПАКТНЫЙ ФОРМАТ ПОД ЯКОРЬ ШАПКИ.
		// CONTRACT: вид "used/total МБ" ограничен 16 рунами ("131072/131072 МБ")
		// и гарантированно садится в якорь Row2.Ram=20 без усечения (fitText).
		ramText = fmt.Sprintf("%d/%d %s", ramUsed/1024, memTotal/1024, i18n.T.UnitMB)
	}

	// Расчет Swap
	swapPercent := 0
	swapText := i18n.T.ViewMainFallbackZeroMb
	if swapTotal > 0 {
		swapUsed := swapTotal - swapFree
		swapPercent = int(swapUsed * 100 / swapTotal)
		swapText = fmt.Sprintf("%d %s / %d %s", swapUsed/1024, i18n.T.UnitMB, swapTotal/1024, i18n.T.UnitMB)
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

	// Файл вырос — читаем последние 5 строк
	logCacheLines = readLastLines(logPath, 5)
	logCacheSize = currentSize
}

// readLastLines читает последние N строк файла, идя ОТ КОНЦА к началу.
// CONTRACT (сохранён со старой версии): ошибка открытия → [ViewMainFallbackError],
// пустой файл → [EmptyLog]; строки возвращаются в исходном порядке (сверху вниз).
// СЛОЖНОСТЬ: O(размер хвоста), а не O(весь файл): лог на 100 МБ больше не
// заставляет TUI перечитывать его целиком при каждом изменении размера.
func readLastLines(path string, n int) []string {
	if n < 1 {
		n = 1
	}
	file, err := os.Open(path)
	if err != nil {
		return []string{i18n.T.ViewMainFallbackError}
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return []string{i18n.T.ViewMainFallbackError}
	}
	size := info.Size()
	if size == 0 {
		return []string{i18n.T.EmptyLog}
	}

	// Закрывающий '\n' — терминатор строки, а не начало новой (пустой):
	// отступаем на байт, чтобы обратный скан не дал пустой «хвост».
	last := make([]byte, 1)
	if _, err := file.ReadAt(last, size-1); err != nil {
		return []string{i18n.T.ViewMainFallbackError}
	}
	if last[0] == '\n' {
		size--
		if size == 0 {
			return []string{i18n.T.EmptyLog}
		}
	}

	const chunk = 64 * 1024
	lines := make([]string, 0, n)
	var carry []byte // левый остаток строки, клеится к следующему чанку
	pos := size

	for pos > 0 && len(lines) < n {
		readSize := int64(chunk)
		if pos < readSize {
			readSize = pos
		}
		pos -= readSize

		buf := make([]byte, readSize)
		readN, err := file.ReadAt(buf, pos)
		if err != nil && err != io.EOF {
			return []string{i18n.T.ViewMainFallbackError}
		}
		block := append(buf[:readN], carry...) // чанк + правый остаток строки
		carry = nil

		// Разбираем блок СПРАВА НАЛЕВО: каждый '\n' терминирует строку.
		end := len(block)
		for len(lines) < n {
			idx := bytes.LastIndexByte(block[:end], '\n')
			if idx < 0 {
				break
			}
			lines = append(lines, string(block[idx+1:end]))
			end = idx
		}
		if end > 0 && len(lines) < n {
			carry = append([]byte(nil), block[:end]...)
		}
	}
	// Дошли до начала файла: левый остаток — это первая строка.
	if len(lines) < n && len(carry) > 0 {
		lines = append(lines, string(carry))
	}

	// Обратный скан дал строки с хвоста к голове — разворачиваем.
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
	}

	if len(lines) == 0 {
		return []string{i18n.T.EmptyLog}
	}
	return lines
}

// DrawMainMenuGrid компонует сетку виджетов Главного Меню Сервера:
// шапка (паспорт системы + дата/время), службы, меню разделов,
// краткая статистика, логи. Возвращает виджеты и количество строк меню.
func DrawMainMenuGrid(selectedRow int) ([]ui.Drawable, int) {
	// Загружаем статику CPU (один раз за процесс)
	loadCpuInfoOnce()

	// Считываем динамические метрики
	cpuUsage := getLinuxCpuPercent()
	// v9.0.0: swap скрыт по макету главного экрана (строки 02-03);
	// метрика продолжает собираться бэкендом, но не выводится в шапку.
	ramPercent, ramText, _, _ := getLinuxMemoryMetrics()

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
	// v9.0.0: булевы флаги статусов вместо готовых label-строк:
	// таблица служб собирает значения сама
	castlingOn := false
	if _, err := os.Stat(config.MarkerSshMoved); err == nil {
		castlingOn = true
	}
	tlsOn := service.IsRescueTLSReady()
	coreTrapOn := false
	if _, errT := os.Stat(config.TarpitMarkerPath(22)); errT == nil {
		coreTrapOn = true
	}

	// Счётчик ботов из IPC-моста
	liveBots := tarpit.GetLiveBotsShmCount()

	// ─────────────────────────────────────────────────────────────
	// 1. ШАПКА (3 строки: y: 1-4)
	// ─────────────────────────────────────────────────────────────
	// v9.0.0: ШАПКА + СИСТЕМНЫЙ ПАСПОРТ объединены (макет строки 01-05).
	header := widgets.NewParagraph()
	// v9.0.0: ДАТА/ВРЕМЯ НА ЛИНИИ ТИТУЛА (макет строка 01), единый стиль TitleStyle.
	// CONTRACT: титульная область = 96 рун (эмпирика): дефисы рамки дорисовывают
	// хвост сами, дата встаёт вплотную к правому краю с одним дефисом перед уголком.
	now := time.Now()
	titleLeft := fmt.Sprintf(" Amber Guard Laboratory - %s ", buildinfo.Version)
	titleRight := fmt.Sprintf(" %s: %s | %s: %s ",
		i18n.T.ViewMainLabelDate, now.Format("2006-01-02"),
		i18n.T.ViewMainLabelTime, now.Format("15:04:05"))
	padRunes := 96 - utf8.RuneCountInString(titleLeft) - utf8.RuneCountInString(titleRight)
	if padRunes < 1 {
		padRunes = 1
	}
	header.Title = titleLeft + strings.Repeat("─", padRunes) + titleRight
	// v9.0.0: ПРАВЫЙ КРАЙ ШАПКИ — опция A и статус паролей (макет строки 02-03).
	// PURPOSE: две охранно-критичные декларации видны без входа в SETTINGS:
	// доступ админа по IP и установленность обоих паролей канала спасения.
	// CONTRACT: первая точка = пароль веб-стука (псевдо-2FA), вторая = пароль
	// веб-панели. Точка всегда ●; цвет несёт смысл: зелёный = установлен,
	// красный = отсутствует (спецификация ON/OFF-точки из ТЗ).
	sshAdminPlain, sshAdminStyle := "ON", "fg:green,mod:bold"
	if !config.C.SshAllowInFromIp {
		sshAdminPlain, sshAdminStyle = "OFF", "fg:red"
	}
	// v9.0.0: Мини-тег "[ ● ]" собирается формой "[[ ● ]](стиль)" (семейство hTag).
	passRescueStyle := "fg:red,mod:bold"
	if config.C.WebRescuePasswordHash != "" {
		passRescueStyle = "fg:green,mod:bold"
	}
	passPanelStyle := "fg:red,mod:bold"
	if config.C.WebPanelPasswordHash != "" {
		passPanelStyle = "fg:green,mod:bold"
	}
	// v9.0.0: БЛОЧНАЯ СБОРКА ШАПКИ
	// Каждая ячейка возвращает (markup, занятая ширина): ширина строки
	// копится арифметически и сверяется с headerInnerWidth охраной макета.
	// CONTRACT: последняя ячейка строки паддируется так же равномерно —
	// формула ширины точна, а хвостовые пробелы невидимы при итоге ≤ 98.
	// РУЧНАЯ СЕТКА. Каждая ячейка приводится ТОЧНО к ширине из
	// headerGrid: короче — добивка пробелами, длиннее — усечение значения.
	// Возвращаемая ширина ВСЕГДА равна конфигу, поэтому ширина строки
	// считается прямо из таблицы (охрана макета ниже).
	grid := headerGrid
	hTag := func(plain, style string) string { return fmt.Sprintf("[[ %s ]](%s)", plain, style) }

	// fitText: plain-ячейка («метка: значение») точно по width.
	fitText := func(label, plain string, width int) string {
		head := ""
		if label != "" {
			head = label + ": "
		}
		out := head + plain
		switch vis := utf8.RuneCountInString(out); {
		case vis < width:
			out += strings.Repeat(" ", width-vis)
		case vis > width:
			keep := width - utf8.RuneCountInString(head)
			if keep < 1 {
				keep = 1
			}
			out = head + trunc(plain, keep) // метка целая, режется значение
		}
		return out
	}

	// fitTag: tag-ячейка («метка: [ X ]») точно по width; при нехватке
	// режется plain ВНУТРИ скобок, скобки и метка сохраняются.
	fitTag := func(label, plain, style string, width int) string {
		head := label + ": "
		headRunes := utf8.RuneCountInString(head)
		p := plain
		if headRunes+utf8.RuneCountInString(p)+4 > width {
			keep := width - headRunes - 4
			if keep < 1 {
				keep = 1
			}
			p = trunc(p, keep)
		}
		out := head + hTag(p, style)
		if vis := headRunes + utf8.RuneCountInString(p) + 4; vis < width {
			out += strings.Repeat(" ", width-vis)
		}
		return out
	}

	// fitColor: цветное значение БЕЗ скобок: markup "[x](style)" съедается termui,
	// на экране остаётся только цветной текст. Ширина ячейки = head + руны(plain).
	// PURPOSE (v9.0.0): литеральные скобки съедали 4 руны бюджета на ячейку и
	// провоцировали переполнение строки → троеточие termui у правой рамки.
	// По решению: «Нагрузка» и «NETWORK (открытых портов)» выводятся без скобок.
	fitColor := func(label, plain, style string, width int) string {
		head := label + ": "
		p := plain
		if keep := width - utf8.RuneCountInString(head); utf8.RuneCountInString(p) > keep {
			if keep < 1 {
				keep = 1
			}
			p = trunc(p, keep)
		}
		out := head + fmt.Sprintf("[%s](%s)", p, style)
		if vis := utf8.RuneCountInString(head) + utf8.RuneCountInString(p); vis < width {
			out += strings.Repeat(" ", width-vis)
		}
		return out
	}

	// fitDots: статус паролей как два мини-тега "[ ● ] [ ● ]" точно по width.
	// ВИДИМАЯ ширина ВСЕГДА 11 рун: 5 + 1 + 5. Вложенной разметки нет:
	// каждый тег — атомарный сегмент формы "[[ ● ]](стиль)" (семейство hTag).
	fitDots := func(label, style1, style2 string, width int) string {
		// Разделитель
		head := label + " "
		out := head + fmt.Sprintf("[[ ● ]](%s)", style1) + " " + fmt.Sprintf("[[ ● ]](%s)", style2)
		if vis := utf8.RuneCountInString(head) + 11; vis < width {
			out += strings.Repeat(" ", width-vis)
		}
		return out
	}

	const sep = "| "
	const sepW = 2
	stateLoad := "fg:green,mod:bold"
	stateNum := "fg:yellow,mod:bold"

	// v9.0.0: КРАЙНЯЯ ЯЧЕЙКА — БУФЕР ТРОЕТОЧИЯ. Её эффективная ширина сама
	// ужимается, если сумма остальных ячеек строки съела бюджет: termui-«…»
	// на правом крае становится невозможным. Конфигурируемое значение в
	// headerGrid = МАКСИМАЛЬНАЯ ширина крайней ячейки (резерв под [ OFF ] и т.п.).
	baseW1 := 1 + grid.Row1.Cpu + grid.Row1.Cores + grid.Row1.Load + 3*sepW
	tailW1 := grid.Row1.SshAdmin
	if baseW1+tailW1 > headerInnerWidth {
		tailW1 = headerInnerWidth - baseW1
	}

	// СТРОКА 1: CPU | ядра | нагрузка (без скобок) | SSH админ IP (буфер)
	rowH1 := " " + fitText(i18n.T.ViewMainLabelCpu, trunc(cpuModelCached, grid.Row1.Cpu), grid.Row1.Cpu)
	rowH1 += sep + fitText("", fmt.Sprintf("%dx%d", cpuCoresCached, cpuThreadsCached), grid.Row1.Cores)
	// v9.0.0: ВЫРАВНИВАНИЕ ЗНАЧЕНИЙ — ВЛЕВО, К ДВОЕТОЧИЮ.
	// PURPOSE: "%3d%%"/"%4d" добивали резерв пробелами СЛЕВА, и короткое значение
	// ("5%", "2") прилипало к правому краю ячейки — к разделителю. Без паддинга
	// значение садится сразу после ": ", а резервный воздух уходит ЗА значение;
	// границы ячеек неизменны (ширины держит headerGrid), соседние колонки не едут.
	rowH1 += sep + fitColor(i18n.T.ViewMainLabelLoad, fmt.Sprintf("%d%%", cpuUsage), stateLoad, grid.Row1.Load)
	rowH1 += sep + fitTag(i18n.T.ViewMainLabelSshAdminIp, sshAdminPlain, sshAdminStyle, tailW1)

	// v9.0.0: разделитель перед паролями сохранён; сдвиг блока паролей вправо
	// держит хвостовой паддинг ячейки Ports (Ports=33), без отдельного спейсера.
	baseW2 := 1 + grid.Row2.Ram + grid.Row2.Usage + grid.Row2.Ports + 3*sepW

	tailW2 := grid.Row2.Pass
	if baseW2+tailW2 > headerInnerWidth {
		tailW2 = headerInnerWidth - baseW2
	}
	// Страховка: markup тегов резать нельзя — минимум head + 11 видимых рун.
	if minW := utf8.RuneCountInString(i18n.T.ViewMainLabelPasswords) + 2 + 11; tailW2 < minW {
		tailW2 = minW
	}

	// СТРОКА 2: RAM | занято | порты (без скобок) | пароли (буфер)
	rowH2 := " " + fitText(i18n.T.ViewMainLabelRam, ramText, grid.Row2.Ram)
	rowH2 += sep + fitColor(i18n.T.ViewMainLabelUsage, fmt.Sprintf("%d%%", ramPercent), stateLoad, grid.Row2.Usage)
	rowH2 += sep + fitColor(i18n.T.StatsOpenPorts, fmt.Sprintf("%d", openPorts), stateNum, grid.Row2.Ports)
	rowH2 += sep + fitDots(i18n.T.ViewMainLabelPasswords, passRescueStyle, passPanelStyle, tailW2)

	header.Text = rowH1 + "\n" + rowH2

	// Ширины строк: крайняя ячейка уже ужата буфером, переполнение невозможно.
	rowW1 := baseW1 + tailW1
	rowW2 := baseW2 + tailW2
	warnHeaderOverflow(rowW1, rowW2)

	header.SetRect(2, 1, 102, 5)
	header.BorderStyle.Fg = ui.ColorCyan
	header.TitleStyle.Fg = ui.ColorCyan
	header.WrapText = false

	// ─────────────────────────────────────────────────────────────
	// СТАТУС СЛУЖБ КОМПЛЕКСА
	// ─────────────────────────────────────────────────────────────
	// v9.0.0: АРИФМЕТИЧЕСКАЯ СБОРКА КОЛОНОК ТАБЛИЦЫ СЛУЖБ.
	// PURPOSE: устраняет «лесенку»: паддинг считается по PLAIN-строкам,
	// длина цветного тега известна явно (vis = руны(plain) + 4 для «[ X ]»).
	// Никаких регулярок по тегам — никаких сюрпризов длины на RU/EN.

	// v9.0.0: ВИДЖЕТ БЛОКА СЛУЖБ.
	// Виджет блока служб: объявляется ровно один раз здесь.
	servicesBlock := widgets.NewParagraph()
	servicesBlock.Title = i18n.T.ViewMainServicesTitle
	servicesBlock.BorderStyle.Fg = ui.ColorCyan
	servicesBlock.TitleStyle.Fg = ui.ColorYellow
	servicesBlock.TitleStyle.Modifier = ui.ModifierBold
	servicesBlock.WrapText = false

	stateOn := "fg:green,mod:bold"
	stateOff := "fg:red"

	tag := func(plain, style string) string { return fmt.Sprintf("[[ %s ]](%s)", plain, style) }
	onOff := func(on bool) (string, string) {
		if on {
			return "ON", stateOn
		}
		return "OFF", stateOff
	}
	// cell: «метка: [ значение ]» + хвостовой паддинг до width (тег в конце ячейки).
	cell := func(label, plain, style string, width int) string {
		head := label + " "
		vis := utf8.RuneCountInString(head) + utf8.RuneCountInString(plain) + 4
		out := head + tag(plain, style)
		if vis < width {
			out += strings.Repeat(" ", width-vis)
		}
		return out
	}

	// cellNoPad: как cell, но без хвостового паддинга.
	// PURPOSE: последняя ячейка строки не нуждается в воздухе справа —
	// край виджета уже даёт отступ. Хвостовой паддинг последней ячейки
	// переполняет inner width Paragraph на несколько пробелов и вызывает
	// троеточие termui (WrapText=false обрезает хвост).
	cellNoPad := func(label, plain, style string) string {
		head := label + " "
		return head + tag(plain, style)
	}
	cellPlainNoPad := func(label, plain string) string {
		return label + ": " + plain
	}

	//  Единая сетка на все строки.
	// СТРОКА 1: [24 | 22 | 24]
	plain, style := onOff(unitWatcherOn)
	row1 := "  " + cell(i18n.T.ViewMainLabelWebWatcher, plain, style, 24)

	plain, style = onOff(unitUfwOn)
	row1 += "| " + cell(i18n.T.ViewMainLabelUfwStatus, plain, style, 22)

	plain, style = onOff(castlingOn)
	row1 += "| " + cell(i18n.T.ViewMainLabelCastlingSsh, plain, style, 24)

	row1 += "| " + cellNoPad(i18n.T.ViewMainLabelSshPort, activeSshPort, stateNum)

	// СТРОКА 2: [24 | 22 | 24]
	plain, style = onOff(tlsOn)
	row2 := "  " + cell(i18n.T.ViewMainLabelTlsCert, plain, style, 24)

	plain, style = onOff(config.C.WebServerEnabled)
	row2 += "| " + cell(i18n.T.ViewMainLabelWebServer, plain, style, 22)

	plain, style = onOff(config.C.WebHttpsPanelActive)
	row2 += "| " + cell(i18n.T.ViewMainLabelWebStat, plain, style, 24)

	plain, style = onOff(config.C.SshIpWebKnock)
	row2 += "| " + cellNoPad(i18n.T.ViewMainLabelWebKnock, plain, style)

	// СТРОКА 3: [24 | 22 | 18]
	plain, style = onOff(unitTarpitOn)
	row3 := "  " + cell(i18n.T.ViewMainLabelTrapsDaemon, plain, style, 24)

	plain, style = onOff(coreTrapOn)
	row3 += "| " + cell(i18n.T.ViewMainLabelCoreTrap, plain, style, 22)

	row3 += "| " + cell(i18n.T.ViewMainLabelTraps, fmt.Sprintf("%d/%d", activeTraps, totalTraps), stateNum, 18)

	// Переход к счётчику ботов: стрелка, после неё двойной воздух (дизайн-решение).
	row3 += "->  " + cellPlainNoPad(i18n.T.ViewMainLabelTrappedNow, fmt.Sprintf("[%d](fg:yellow,mod:bold)", liveBots))

	// Собираем все строки для блока вывода
	servicesBlock.Text = row1 + "\n" + row2 + "\n" + row3
	servicesBlock.SetRect(2, 5, 102, 10)

	// ─────────────────────────────────────────────────────────────
	// 4. БЛОК C: КРАТКАЯ СТАТИСТИКА (координаты — в SetRect ниже)
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

	statsBlock := widgets.NewParagraph()
	statsBlock.Title = i18n.T.StatsBlockTitle

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
	statsBlock.SetRect(2, 19, 102, 23)
	statsBlock.BorderStyle.Fg = ui.ColorCyan
	statsBlock.TitleStyle.Fg = ui.ColorYellow
	statsBlock.TitleStyle.Modifier = ui.ModifierBold
	statsBlock.WrapText = false

	// ─────────────────────────────────────────────────────────────
	// 5. МЕНЮ РАЗДЕЛОВ (7 пунктов; координаты — в SetRect ниже)
	// ─────────────────────────────────────────────────────────────
	menuList := widgets.NewList()
	menuList.Title = i18n.T.ViewMainMenuTitle
	menuList.Rows = []string{
		fmt.Sprintf(" SETTINGS      │ %s", i18n.T.ViewMainRowSettings),
		fmt.Sprintf(" ANTIDDOS      │ %s", i18n.T.ViewMainRowAntiddos),
		fmt.Sprintf(" CORE TRAP     │ %s", i18n.T.ViewMainRowTarpit),
		fmt.Sprintf(" MULTI-TRAP    │ %s", i18n.T.ViewMainRowMultiTarpit),
		fmt.Sprintf(" PORT MAP      │ %s", i18n.T.ViewMainRowPortMap),
		fmt.Sprintf(" BACKUP        │ %s", i18n.T.ViewMainRowBackup),
		fmt.Sprintf(" EXIT          │ %s", i18n.T.ViewMainRowExit),
	}
	menuList.SetRect(2, 10, 102, 19)

	// v9.0.0: КОСМЕТИКА — полоса выделения до правого края блока.
	innerWidth := menuList.GetRect().Dx() - 2
	paddedRows := make([]string, len(menuList.Rows))
	for i, row := range menuList.Rows {
		if n := utf8.RuneCountInString(row); n < innerWidth {
			row += strings.Repeat(" ", innerWidth-n)
		}
		paddedRows[i] = row
	}
	menuList.Rows = paddedRows

	menuList.BorderStyle.Fg = ui.ColorCyan
	menuList.SelectedRow = selectedRow
	menuList.SelectedRowStyle = ui.NewStyle(ui.ColorBlack, ui.ColorGreen, ui.ModifierBold)

	// ─────────────────────────────────────────────────────────────
	// 6. БЛОК D: ЛОГИ КОМПЛЕКСА (5 строк логов; координаты — в SetRect ниже)
	// ─────────────────────────────────────────────────────────────
	updateLogCache()

	logCacheMu.Lock()
	logLines := make([]string, len(logCacheLines))
	copy(logLines, logCacheLines)
	logCacheMu.Unlock()

	// Обрезаем строки до 96 символов (102 - 6 служебных).
	// v9.0.0: многоточие унифицировано с trunc() на одиночную "…":
	// экономит 2 руны полезного текста строки (95+1 вместо 93+3).
	for i := range logLines {
		// Используем rune для правильного подсчёта символов (не байт!)
		runes := []rune(logLines[i])
		if len(runes) > 96 {
			logLines[i] = string(runes[:95]) + "…"
		}
	}

	logBlock := widgets.NewParagraph()
	logBlock.Title = i18n.T.StatsLogBlockTitle
	logBlock.Text = " " + strings.Join(logLines, "\n ")
	logBlock.SetRect(2, 23, 102, 30)
	logBlock.BorderStyle.Fg = ui.ColorCyan
	logBlock.TitleStyle.Fg = ui.ColorYellow
	logBlock.TitleStyle.Modifier = ui.ModifierBold
	logBlock.WrapText = false

	return []ui.Drawable{header, servicesBlock, menuList, statsBlock, logBlock}, len(menuList.Rows)
}

// formatLogSize возвращает человеко-читаемый размер файла.
// Если размер < 1 МБ — показывает в КБ, иначе в МБ с одним знаком после запятой.
func formatLogSize(bytes int64) string {
	if bytes < 1024*1024 {
		return fmt.Sprintf("%d %s", bytes/1024, i18n.T.UnitKB)
	}
	return fmt.Sprintf("%.1f %s", float64(bytes)/(1024*1024), i18n.T.UnitMB)
}

// trunc обрезает строку до maxLen ВИДИМЫХ символов (рун) с многоточием.
// CONTRACT верстки сохранён и ужесточён: результат всегда ровно maxLen рун
// при переполнении (maxLen-1 руна + "…") и не длиннее maxLen иначе.
// На ASCII-строках поведение побайтово идентично старому — сетка не едет.
func trunc(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen < 1 {
		return ""
	}
	return string(runes[:maxLen-1]) + "…"
}
