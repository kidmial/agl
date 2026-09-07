// tui/view_main.go
package tui

import (
	"agl/config"
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
			cpuModelCached = "Unknown CPU"
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
			cpuModelCached = "Unknown CPU"
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
				"event=ufw.status_restored Опрос UFW снова доступен.")
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
				"event=systemd.watcher_probe_restored Опрос юнита вотчера снова доступен.")
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
		return 0, "Error", 0, "Error"
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
	ramText := "0 MB / 0 MB"
	if memTotal > 0 {
		ramUsed := memTotal - memAvailable
		ramPercent = int(ramUsed * 100 / memTotal)
		ramText = fmt.Sprintf("%d MB / %d MB", ramUsed/1024, memTotal/1024)
	}

	// Расчет Swap
	swapPercent := 0
	swapText := "0 MB / 0 MB"
	if swapTotal > 0 {
		swapUsed := swapTotal - swapFree
		swapPercent = int(swapUsed * 100 / swapTotal)
		swapText = fmt.Sprintf("%d MB / %d MB", swapUsed/1024, swapTotal/1024)
	}

	return ramPercent, ramText, swapPercent, swapText
}

// DrawMainMenuGrid компонует сетку виджетов Главного Меню Сервера (v8.1.3).
// Редизайн: убраны три Gauge-полосы → два смысловых блока текста.
// Блок A — СИСТЕМНЫЙ ПАСПОРТ (CPU, RAM, Swap).
// Блок B — СЛУЖБЫ КОМПЛЕКСА (SSH, UFW, яма, вотчер, боты).
func DrawMainMenuGrid(selectedRow int) ([]ui.Drawable, int) {
	// Загружаем статику CPU (один раз за процесс)
	loadCpuInfoOnce()

	// Считываем динамические метрики
	cpuUsage := getLinuxCpuPercent()
	ramPercent, ramText, swapPercent, swapText := getLinuxMemoryMetrics()

	// Обновляем кэш статусов юнитов (раз в 10 сек)
	refreshUnitCache()

	// Состояние SSH и кастлинга
	activeSshPort := sys.DetectActiveSshPort()
	castlingLabel := "[[ OFF ]](fg:red,mod:bold)"
	if _, err := os.Stat(config.MarkerSshMoved); err == nil {
		castlingLabel = fmt.Sprintf("[[ ON ] → %d](fg:green,mod:bold)", config.C.RealSshPort)
	}

	// Счётчик ботов из IPC-моста (закрывает C3)
	liveBots := tarpit.GetLiveBotsShmCount()

	// M3f (v8.1.3): индикатор TLS-шлюза спасения (два os.Stat, без exec)
	tlsLabel := "[[ OFF ]](fg:red)"
	if service.IsRescueTLSReady() {
		tlsLabel = "[[ ON ]](fg:green,mod:bold)"
	}

	// ─────────────────────────────────────────────────────────────
	// 1. ШАПКА (3 строки: y: 1-4)
	// ─────────────────────────────────────────────────────────────
	timeStr := time.Now().Format("15:04:05")
	dateStr := time.Now().Format("2006-01-02")
	header := widgets.NewParagraph()
	// v8.3.0 (5.1): версия больше не хардкодится в исходнике —
	// берётся из buildinfo и внедряется на этапе компиляции через -ldflags.
	// При голом go build (без Makefile) панель честно покажет "dev":
	// это маркер нерелизной сборки, а не скрытая подмена версии.
	header.Title = fmt.Sprintf(" Amber Guard Laboratory - %s ", buildinfo.Version)
	header.Text = fmt.Sprintf(" [ Главное Меню Комплекса ](fg:cyan,mod:bold) │ Дата: %s │ Время: %s",
		dateStr, timeStr)
	header.SetRect(2, 1, 102, 4)
	header.BorderStyle.Fg = ui.ColorCyan
	header.TitleStyle.Fg = ui.ColorYellow
	header.TitleStyle.Modifier = ui.ModifierBold

	// ─────────────────────────────────────────────────────────────
	// 2. БЛОК A: СИСТЕМНЫЙ ПАСПОРТ (4 строки: y: 4-8)
	//    CPU модель + ядра/потоки + нагрузка; RAM; Swap
	// ─────────────────────────────────────────────────────────────
	sysPassport := widgets.NewParagraph()
	sysPassport.Title = " Системный паспорт хоста "
	sysPassport.Text = fmt.Sprintf(
		" CPU: %-40s │ %d×%d │ Нагрузка: [%d%%](fg:green,mod:bold)\n"+
			" RAM: %-22s │ Использование: [%d%%](fg:green,mod:bold)\n"+
			" Swap: %-21s │ Использование: [%d%%](fg:cyan,mod:bold)",
		trunc(cpuModelCached, 40), cpuCoresCached, cpuThreadsCached, cpuUsage,
		ramText, ramPercent,
		swapText, swapPercent)
	sysPassport.SetRect(2, 4, 102, 8)
	sysPassport.BorderStyle.Fg = ui.ColorCyan
	sysPassport.TitleStyle.Fg = ui.ColorYellow
	sysPassport.TitleStyle.Modifier = ui.ModifierBold

	// ─────────────────────────────────────────────────────────────
	// 3. БЛОК B: СЛУЖБЫ КОМПЛЕКСА (4 строки: y: 8-12)
	//    SSH+кастлинг; UFW; Яма; Вотчер; Боты в капкане
	// ─────────────────────────────────────────────────────────────
	servicesBlock := widgets.NewParagraph()
	servicesBlock.Title = " Службы комплекса Amber Guard Lab."

	ufwLabel := "[[ OFF ]](fg:red,mod:bold)"
	if unitUfwOn {
		ufwLabel = "[[ ON ]](fg:green,mod:bold)"
	}
	// Источник правды — маркер базовой ловушки (демон пишет/удаляет мгновенно).
	// Юнит демона живёт постоянно и не отражает состояние ямы.
	tarpitLabel := "[[ OFF ]](fg:red,mod:bold)"
	if _, errT := os.Stat(config.TarpitMarkerPath(22)); errT == nil {
		tarpitLabel = "[[ ON ]](fg:green,mod:bold)"
	}
	watcherLabel := "[[ OFF ]](fg:red,mod:bold)"
	if unitWatcherOn {
		watcherLabel = "[[ ON ]](fg:green,mod:bold)"
	}

	servicesBlock.Text = fmt.Sprintf(
		" WEB WATCHER: %s │ UFW: %s │ SSH: %-5s │ Кастлинг: %s \n"+
			" TARPIT: %s │ TLS шлюз: %s │ В капкане сейчас: [%d](fg:yellow,mod:bold)",
		watcherLabel, ufwLabel, activeSshPort, castlingLabel,
		tarpitLabel, tlsLabel, liveBots)
	servicesBlock.SetRect(2, 8, 102, 12)
	servicesBlock.BorderStyle.Fg = ui.ColorCyan
	servicesBlock.TitleStyle.Fg = ui.ColorYellow
	servicesBlock.TitleStyle.Modifier = ui.ModifierBold

	// ─────────────────────────────────────────────────────────────
	// 4. МЕНЮ РАЗДЕЛОВ (9 строк: y: 12-21)
	// ─────────────────────────────────────────────────────────────
	menuList := widgets.NewList()
	menuList.Title = " Выберите административный раздел для работы: "
	menuList.Rows = []string{
		" SETTINGS      │ Глобальная  конфигурация  периметра  безопасности",
		" ANTIDDOS      │ Менеджер   сетевой   брони  (Защита  от  штормов)",
		" TARPIT        │ Алгоритм «Дегтярная Яма» для парализации ботнетов",
		" MULTI TARPIT  │ Управление  распределенным  пулом  мульти-ловушек",
		" PORT MAP      │ Картография  активных сокетов и  инвентарь  хоста",
		" BACKUP        │ Создание  снапшота  системы  (Архивация контуров)",
		" EXIT          │ Выход  из панели  управления  обратно  в  консоль",
	}
	menuList.SetRect(2, 12, 102, 21)
	menuList.BorderStyle.Fg = ui.ColorCyan
	menuList.SelectedRow = selectedRow
	menuList.SelectedRowStyle = ui.NewStyle(ui.ColorBlack, ui.ColorGreen, ui.ModifierBold)

	return []ui.Drawable{header, sysPassport, servicesBlock, menuList}, len(menuList.Rows)
}

// trunc обрезает строку до maxLen символов с многоточием при превышении.
func trunc(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "…"
}
