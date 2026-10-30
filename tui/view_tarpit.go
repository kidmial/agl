// tui/view_tarpit.go
// ============================================================================
// КОМПОНЕНТ: Экран управления базовой Дегтярной Ямой (порт 22)
// ВЕРСИЯ: v8.0.1
// РОЛЬ: Многослойная интерактивная панель управления базовой ловушкой.
//
//	Отображает статус движка, рокировку, джиттер, счётчик ботов
//	и два LIVE-терминала (мгновенные касания + хроника истощения).
//
// ============================================================================
//
// АРХИТЕКТУРНАЯ ПОЗИЦИЯ:
//
//	Этот файл — TUI-компонент экрана «Дегтярная Яма».
//	Вызывается из `view_router.go` при навигации на экран `TARPIT`.
//
//	Экран отображает:
//	- Статус движка (через `service.CheckTarpitProtocol()`)
//	- Статус рокировки (через маркер `agl_ssh_moved`)
//	- Параметры джиттера из глобального конфига
//	- Счётчик ботов из IPC-моста `/dev/shm/`
//	- Два LIVE-терминала с логами
//
// КЛЮЧЕВОЕ ИЗМЕНЕНИЕ v8.0.1:
//   - `service.CheckTarpitProtocol()` теперь опрашивает
//     `tarpitctl.IsDaemonAlive()` + маркер на диске, а не `systemctl is-active`.
//   - Счётчик ботов читается из `/dev/shm/` через обновлённый `tarpit_tracker.go`.
//   - Все вызовы совместимы с новой декларативной архитектурой.
//
// ### [2026-08-22] — Мажорный рефакторинг экрана (v8.0.1)
// - Обновлены комментарии для соответствия новой архитектуре.
// - Статус движка проверяется через обновлённый `SetTarpitProtocol("CHECK")`.
// - Счётчик ботов читается из IPC-моста `/dev/shm/`.
// - Добавлена история изменений.
// ============================================================================
package tui

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"agl/config"
	"agl/i18n"
	"agl/internal/service"
	"agl/internal/sys"
	"agl/internal/tarpit"

	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
)

// ============================================================================
// ГЛОБАЛЬНЫЕ ПЕРЕМЕННЫЕ
// ============================================================================

// Глобальный указатель на подвальный SIEM-терминал (синглтон).
// Используется для обновления заголовка с счётчиком ботов без пересоздания виджета.
var ReportTerminal *widgets.List

// Глобальный указатель на средний LIVE-терминал касаний сокетов (синглтон).
// v8.2.3 (R-8): используется для динамического изменения цвета рамки
// (cyan → magenta при появлении ботов в капкане).
var liveTerminal *widgets.List

// ============================================================================
// ВСПОМОГАТЕЛЬНЫЕ ФУНКЦИИ
// ============================================================================

// getLastLogLines извлекает последние N строк из указанного файла журнала
// для отображения в TUI-терминалах. Заменяет табуляцию на вертикальную черту
// для корректного отображения в сетке.
//
// ПАРАМЕТРЫ:
//   - path: путь к лог-файлу
//   - maxLines: максимальное количество строк для извлечения
//
// ВОЗВРАЩАЕТ:
//   - Срез строк для отображения в виджете `widgets.List`
//
// ОБНОВЛЕНО В v8.1.3: Убраны фризы из-за overload CPU вследствие чтения лог-файлов целиком.
// logCacheEntry — M3e: кэш хвоста лога, чтобы не перечитывать файл на каждый клавиш.
type logCacheEntry struct {
	lines []string
	at    time.Time
}

var logCache = map[string]logCacheEntry{}

// getLastLogLines извлекает последние N строк журнала с кэшем 2 сек (M3e).
// Защита от CPU 100%: навигация по меню больше не перечитывает логи.
func getLastLogLines(path string, maxLines int) []string {
	if e, ok := logCache[path]; ok && time.Since(e.at) < 2*time.Second {
		return e.lines
	}
	lines := readLogTail(path, maxLines)
	logCache[path] = logCacheEntry{lines: lines, at: time.Now()}
	return lines
}

// readLogTail читает только ХВОСТ файла (последние 64 КБ) вместо полного
// сканирования. Под DDoS логи растут в МБ — полный скан сжигал CPU.
func readLogTail(path string, maxLines int) []string {
	file, err := os.Open(path)
	if err != nil {
		return []string{i18n.T.ViewTarpitLogUnavailable}
	}
	defer file.Close()

	const tailSize = 64 * 1024
	fi, err := file.Stat()
	if err != nil || fi.Size() == 0 {
		return []string{i18n.T.ViewTarpitLogUnavailable}
	}
	start := int64(0)
	if fi.Size() > tailSize {
		start = fi.Size() - tailSize
	}
	if _, err := file.Seek(start, 0); err != nil {
		return []string{}
	}

	lines := []string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		cleanLine := strings.ReplaceAll(scanner.Text(), "\t", " │ ")
		lines = append(lines, "  "+cleanLine)
	}
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return lines
}

// ============================================================================
// ОТРИСОВКА ЭКРАНА ДЕГТЯРНОЙ ЯМЫ
// ============================================================================

// DrawTarpitMenuGrid генерирует многослойную интерактивную панель управления
// базовой Дегтярной Ямой на порту 22. Включает:
//   - Шапку с статусами и метриками
//   - Меню управления (7 пунктов)
//   - Блок контекстной справки
//   - Два LIVE-терминала (мгновенные касания + хроника истощения)
//
// МАППИНГ СТРОК МЕНЮ:
//
//	0 — LIVE_BOX:    Просмотр живой таблицы инцидентов
//	1 — SETTINGS:    Глобальные настройки параметров
//	2 — MOVE_PORT:   Рокировка портов (Кастлинг)
//	3 — TARPIT_ON:   Активация ловушки на порту 22
//	4 — TARPIT_OFF:  Деактивация ловушки на порту 22
//	5 — HELP:        Справка по протоколу
//	6 — BACK:        Возврат в главное меню
//
// ОБНОВЛЕНО В v8.0.1: Статус движка проверяется через обновлённый
// `service.CheckTarpitProtocol()`, который опрашивает `tarpitctl`.
func DrawTarpitMenuGrid(selectedRow int, hintText string) ([]ui.Drawable, int) {
	// -------------------------------------------------------------
	// ШАГ 1: ОПРЕДЕЛЕНИЕ СТАТУСА ДВИЖКА
	// Проверяем, активна ли Дегтярная Яма на порту 22.
	//
	// ОБНОВЛЕНО В v8.0.1: `service.CheckTarpitProtocol()` теперь
	// опрашивает `tarpitctl.IsDaemonAlive()` + маркер на диске,
	// а не `systemctl is-active` напрямую.
	// -------------------------------------------------------------
	statusEngine := " [[ OFF ]](fg:red,mod:bold)"
	if errCheck := service.CheckTarpitProtocol(); errCheck == nil {
		statusEngine = " [[ ON ]](fg:green,mod:bold)"
	}

	// -------------------------------------------------------------
	// ШАГ 2: ОПРЕДЕЛЕНИЕ СТАТУСА РОКИРОВКИ
	// Проверяем маркер `agl_ssh_moved` на диске.
	// Путь соответствует регламенту маркеров v6.3.9.
	// -------------------------------------------------------------
	markerFile := config.MarkerSshMoved
	activeSshPort := sys.DetectActiveSshPort()

	statusMove := " [[ OFF ]](fg:red,mod:bold)  "
	if _, err := os.Stat(markerFile); err == nil {
		statusMove = " [[ ON ]](fg:green,mod:bold)   "
	}

	// -------------------------------------------------------------
	// ШАГ 3: РАСЧЁТ ДИНАМИЧЕСКИХ ГРАНИЦ УДЕРЖАНИЯ
	// Диапазон вычисляется из глобального конфига.
	// Для базовой ловушки порта 22 параметры берутся из `config.C`.
	//
	// ОБНОВЛЕНО В v8.0.1: Без изменений логики.
	// Базовая ловушка использует глобальный конфиг при активации
	// через `SetTarpitProtocol("ON")`.
	// -------------------------------------------------------------
	minDelay := config.C.DelayStep - config.C.DelayVariance
	if minDelay < 0 {
		minDelay = 0
	}
	maxDelay := config.C.DelayStep + config.C.DelayVariance

	// -------------------------------------------------------------
	// ШАГ 4: ИЗВЛЕЧЕНИЕ ВРЕМЕНИ И ДАТЫ
	// -------------------------------------------------------------
	dateStr := time.Now().Format("2006-01-02")
	timeStr := time.Now().Format("15:04:05")

	// -------------------------------------------------------------
	// ШАГ 5: ИЗВЛЕЧЕНИЕ СЧЁТЧИКА БОТОВ ИЗ IPC-МОСТА
	// Читаем из `/dev/shm/amberguard.counter` через обновлённый
	// `tarpit_tracker.go`. Если демон не запущен, используется
	// резервное сканирование `/proc/net/tcp`.
	//
	// ОБНОВЛЕНО В v8.0.1: Счётчик читается через межпроцессный
	// IPC-мост, а не атомарную переменную в ОЗУ одного процесса.
	// -------------------------------------------------------------
	liveBots := tarpit.GetLiveBotsShmCount()

	// -------------------------------------------------------------
	// ШАПКА ПАНЕЛИ
	// Высота: 6 строк (y: 1 - 7)
	// -------------------------------------------------------------
	header := widgets.NewParagraph()
	header.Title = fmt.Sprintf(" %s │ %s │ %s ", i18n.T.ViewTarpitHeaderTitle, dateStr, timeStr)

	// ВЫРАВНИВАНИЕ КОЛОНОК ПО СЕТКЕ С УЧЁТОМ СЧЁТЧИКА AGL_CRON_WATCHER
	// ОБНОВЛЕНО В v8.0.1: Без изменений формата.
	header.Text = fmt.Sprintf(
		"  %s %-5s     %s %s\n"+
			"  %s %2dc ± %-2dc    %s %s\n"+
			"  %s %d - %-2d %s    %s %d",
		// Строка 01
		i18n.T.ViewTarpitLabelCurrentSshPort, activeSshPort, i18n.T.ViewTarpitLabelCastling, statusMove,
		// Строка 02
		i18n.T.ViewTarpitLabelJitterSec, config.C.DelayStep, config.C.DelayVariance, i18n.T.ViewTarpitLabelEngineStatus, statusEngine,
		// Строка 03
		i18n.T.ViewTarpitLabelRetentionRange, minDelay, maxDelay, i18n.T.ViewTarpitLabelSec, i18n.T.ViewTarpitLabelBotsInTrap, liveBots,
	)

	header.SetRect(2, 1, 102, 6)
	header.BorderStyle.Fg = ui.ColorCyan
	header.TitleStyle.Fg = ui.ColorYellow

	// -------------------------------------------------------------
	// МЕНЮ УПРАВЛЕНИЯ
	// Высота: 7 строк (y: 7 - 14)
	// -------------------------------------------------------------
	tarpitList := widgets.NewList()
	tarpitList.Title = i18n.T.ViewTarpitListTitle
	tarpitList.Rows = []string{
		fmt.Sprintf("  LIVE_BOX    │ %s", i18n.T.ViewTarpitRowLiveBoxDesc),
		fmt.Sprintf("  SETTINGS    │ %s", i18n.T.ViewTarpitRowSettingsDesc),
		fmt.Sprintf("  MOVE_PORT   │ "+i18n.T.ViewTarpitRowMovePortDesc, config.C.RealSshPort),
		fmt.Sprintf("  TARPIT_ON   │ %s", i18n.T.ViewTarpitRowOnDesc),
		fmt.Sprintf("  TARPIT_OFF  │ %s", i18n.T.ViewTarpitRowOffDesc),
		fmt.Sprintf("  HELP        │ %s", i18n.T.ViewTarpitRowHelpDesc),
		fmt.Sprintf("  BACK        │ %s", i18n.T.ViewTarpitRowBackDesc),
	}
	tarpitList.SetRect(2, 6, 102, 14)
	tarpitList.BorderStyle.Fg = ui.ColorCyan
	tarpitList.TitleStyle.Fg = ui.ColorYellow
	tarpitList.TitleStyle.Modifier = ui.ModifierBold
	tarpitList.SelectedRow = selectedRow
	tarpitList.SelectedRowStyle = ui.NewStyle(ui.ColorBlack, ui.ColorGreen, ui.ModifierBold)

	// -------------------------------------------------------------
	// БЛОК КОНТЕКСТНОЙ СПРАВКИ
	// Высота: 4 строки (y: 14 - 18)
	// -------------------------------------------------------------
	infoBox := widgets.NewParagraph()
	infoBox.Title = i18n.T.ViewTarpitInfoBoxTitle
	infoBox.Text = hintText
	infoBox.SetRect(2, 14, 102, 18)
	infoBox.BorderStyle.Fg = ui.ColorYellow
	infoBox.TextStyle.Fg = ui.ColorCyan
	infoBox.WrapText = false

	// -------------------------------------------------------------
	// СРЕДНИЙ LIVE-ТЕРМИНАЛ: МГНОВЕННЫЕ КАСАНИЯ СОКЕТОВ
	// Читает `tarpit_connection.log` (лёгкий лог касаний).
	// Высота: 6 строк (y: 18 - 24)
	//
	// ОБНОВЛЕНО В v8.0.1: Путь берётся из `config.C.TarpitConnLog`.
	// В новой архитектуре лог пишет ТОЛЬКО демон.
	// TUI только читает файл для отображения.
	// -------------------------------------------------------------
	connLogPath := config.C.TarpitConnLog
	if connLogPath == "" {
		connLogPath = config.FileTarpitConnLog
	}

	// v8.2.3 (R-8): liveTerminal — глобальная переменная, инициализируется один раз.
	// Цвет рамки меняется динамически в UpdateTarpitMenuCounters().
	if liveTerminal == nil {
		liveTerminal = widgets.NewList()
		liveTerminal.BorderStyle.Fg = ui.ColorCyan
		liveTerminal.TitleStyle.Fg = ui.ColorYellow
	}
	liveTerminal.Title = i18n.T.ViewTarpitLiveTerminalTitle
	liveTerminal.Rows = getLastLogLines(connLogPath, 4)
	liveTerminal.SetRect(2, 18, 102, 24)

	// -------------------------------------------------------------
	// ПОДВАЛЬНЫЙ SIEM-ТЕРМИНАЛ: ХРОНИКА ИСТОЩЕНИЯ
	// Читает `tarpit_report.log` (тяжёлый криминалистический профиль).
	// Высота: 6 строк (y: 24 - 30)
	//
	// ОБНОВЛЕНО В v8.0.1: Путь берётся из `config.C.TarpitRptLog`.
	// Заголовок обновляется через `UpdateTarpitMenuCounters()`
	// с счётчиком ботов из IPC-моста.
	// -------------------------------------------------------------
	rptLogPath := config.C.TarpitRptLog
	if rptLogPath == "" {
		rptLogPath = config.FileTarpitRptLog
	}

	if ReportTerminal == nil {
		ReportTerminal = widgets.NewList()
		ReportTerminal.BorderStyle.Fg = ui.ColorCyan
		ReportTerminal.TitleStyle.Fg = ui.ColorYellow
	}
	ReportTerminal.SetRect(2, 24, 102, 30)
	ReportTerminal.Rows = getLastLogLines(rptLogPath, 4)

	// Нативно инкрементируем ОЗУ-счётчики перед выводом на холст
	// ОБНОВЛЕНО В v8.0.1: Вызов `UpdateTarpitMenuCounters()` обновляет
	// заголовок подвального терминала с счётчиком из IPC-моста.
	UpdateTarpitMenuCounters()

	return []ui.Drawable{header, tarpitList, infoBox, liveTerminal, ReportTerminal},
		len(tarpitList.Rows)
}

// ============================================================================
// ФОНОВОЕ ОБНОВЛЕНИЕ СЧЁТЧИКОВ
// ============================================================================

// UpdateTarpitMenuCounters фоново считывает IPC-мост памяти и обновляет
// заголовок подвального терминала без дисковой нагрузки.
// Вызывается из `view_router.go` на каждом тике таймера (1 секунда).
//
// ОБНОВЛЕНО В v8.0.1: Счётчик читается из `/dev/shm/amberguard.counter`
// через обновлённый `tarpit_tracker.go`. Если демон не запущен,
// используется резервное сканирование `/proc/net/tcp`.
func UpdateTarpitMenuCounters() {
	if ReportTerminal == nil {
		return
	}

	// Нативный и быстрый опрос разделяемой памяти ОЗУ через IPC-мост.
	// ОБНОВЛЕНО В v8.0.1: `GetLiveBotsShmCount()` читает из `/dev/shm/`,
	// а не из атомарной переменной в ОЗУ одного процесса.
	liveBots := tarpit.GetLiveBotsShmCount()

	// Атомарно подменяем строку заголовка без мерцания интерфейса панели.
	// ReportTerminal — финальный отчёт ЗАВЕРШЁННЫХ сессий: его логика не меняется.
	ReportTerminal.Title = fmt.Sprintf(i18n.T.ViewTarpitReportTerminalTitleTpl, liveBots)

	// v8.2.3 (R-8): динамический цвет рамки liveTerminal — glanceable-индикатор боя.
	// Меняем ТОЛЬКО рамку живого терминала касаний; ReportTerminal не трогаем.
	// liveBots заимствуем из вычисления выше — повторного чтения shm нет,
	// счётчик третий раз на экран не выводится: сигнал только цветовой.
	//
	// СЕМАНТИКА ЦВЕТА (визуальная грамматика комплекса):
	//   - ШТИЛЬ (0 ботов) → ColorCyan: штатная рамка мониторинга;
	//   - БОЙ (>0 ботов)  → ColorMagenta: цвет дегтярной ямы
	//     (им помечены капканы в PORT_MAP и списках брони).
	// Красный осознанно не используем: в грамматике он занят
	// состоянием OFF/DISARM, и боевой сигнал читался бы как отказ.
	if liveTerminal != nil {
		if liveBots > 0 {
			liveTerminal.BorderStyle.Fg = ui.ColorMagenta
		} else {
			liveTerminal.BorderStyle.Fg = ui.ColorCyan
		}
	}
}
