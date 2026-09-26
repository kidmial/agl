// internal/forensics/tarpit_report.go
// ============================================================================
// КОМПОНЕНТ: Верификация и поведенческий анализ сканеров «Робот-Судья»
// ВЕРСИЯ: v8.0.1
// РОЛЬ: Высокоскоростный парсер криминалистической истории ловушек.
//
//	Возвращает массив строк для отображения в TUI (LIVE_BOX)
//	или выводит отчёт в консоль (режим `RunTarpitForensics`).
//
// ============================================================================
//
// АРХИТЕКТУРНАЯ ПОЗИЦИЯ:
//
//	Этот файл — ЕДИНСТВЕННЫЙ источник данных для экрана LIVE_BOX.
//	Вызывается из `tui/view_tarpit_live_box.go` через `GetTarpitLiveRows()`.
//
// КЛЮЧЕВОЕ ИЗМЕНЕНИЕ v8.0.1:
//
//	В новой архитектуре единого демона воркеры ловушек живут в ОТДЕЛЬНОМ
//	процессе (`agl --tarpit-daemon`). Глобальная переменная
//	`tarpit.ActiveSessions` (sync.Map) в процессе TUI ВСЕГДА ПУСТА,
//	потому что она не разделяется между процессами.
//
//	Поэтому блок «СЛЕД ПЕРВЫЙ» (активные боты из RAM-карты) УДАЛЁН.
//	Количество ботов в настоящий момент отображается через счётчик
//	в шапке экрана (из `/dev/shm/` через `tarpit.GetLiveBotsShmCount()`).
//
//	LIVE_BOX показывает ТОЛЬКО завершённые сессии из `tarpit_report.log`.
//	Это Вариант В, согласованный с напарником для первой итерации.
//
// ФОРМАТ ЛОГА `tarpit_report.log` (13 колонок через табуляцию):
//
//	[0]  Дата/Время       — момент начала атаки (якорь SIEM)
//	[1]  TarpitID         — идентификатор ловушки ("TARPIT:22" или "TARPIT:8080")
//	[2]  IP               — адрес атакующего
//	[3]  План удержания   — запланированный джиттер (в секундах)
//	[4]  Факт удержания   — фактическое время удержания (в секундах)
//	[5]  Поведение        — метка поведения ("PASSIVE" или "AGGRESSIVE")
//	[6]  Страна           — страна источника (результат GeoIP-lookup)
//	[7]  Исходный порт    — реальный порт атакующей стороны
//	[8]  Токен сессии     — уникальный идентификатор сессии
//	[9]  MSS              — максимальный размер сегмента (из TCP_INFO)
//	[10] SndCwnd          — окно отправки TCP (из TCP_INFO)
//	[11] RcvSpace         — буфер приёма (из TCP_INFO)
//	[12] RTT              — сетевая задержка в мс (из TCP_INFO)
//
// ### [2026-08-22] — Мажорный рефакторинг парсера (v8.0.1)
//   - Удалён блок «След 1» (активные боты из `tarpit.ActiveSessions`).
//     В новой архитектуре воркеры живут в отдельном процессе демона,
//     и `sync.Map` не разделяется между процессами.
//   - Количество ботов отображается через счётчик в шапке экрана
//     (из `/dev/shm/` через `tarpit.GetLiveBotsShmCount()`).
//   - LIVE_BOX показывает только завершённые сессии из `tarpit_report.log`.
//   - `RunTarpitForensics()` получил минимальную реализацию для консольного режима.
//   - Добавлены подробные комментарии по формату лога и архитектуре.
//
// ============================================================================
package forensics

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"agl/config"
	"agl/i18n"
)

// ============================================================================
// КОНСТАНТЫ СТИЛЕЙ ДЛЯ ИЗОЛЯЦИИ ОТ ЦИКЛИЧЕСКОГО ИМПОРТА
// ============================================================================

// Локальный реестр стилей для полной изоляции от циклического импорта пакета `tui`.
// Эти константы используются для форматирования строк в `GetTarpitLiveRows()`.
const (
	colorGreen  = "(fg:green,mod:bold)"
	colorRed    = "(fg:red,mod:bold)"
	colorYellow = "(fg:yellow,mod:bold)"
	colorReset  = "[-]"
)

// shortTarpitID нормализует идентификатор ловушки для компактного отображения (v8.2.3).
// Колонки отчёта обязаны быть фиксированной ширины, иначе таблица «поедет»
// при длинных идентификаторах.
// Эталонный формат "TARPIT:65535" = 12 символов — это и есть лимит колонки.
// Всё, что длиннее, обрезается: различимость ловушек по порту сохраняется.
func shortTarpitID(id string) string {
	id = strings.TrimSpace(id)
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// ============================================================================
// КОНСОЛЬНЫЙ РЕЖИМ: ЗАПУСК ФОМЕНЗИКИ ИЗ РОУТЕРА
// ============================================================================

// RunTarpitForensics выводит криминалистический отчёт по ловушкам в консоль.
// Вызывается из `forensicsrouter.go` при выборе маршрута `TARPIT_LOG`.
//
// АЛГОРИТМ:
//  1. Проверяет, активна ли ловушка (через маркер на диске)
//  2. Читает последние записи из `tarpit_report.log`
//  3. Вывает форматированный отчёт в консоль
//
// ОБНОВЛЕНО В v8.0.1: Получил минимальную реализацию вместо пустой заглушки.
// Раньше эта функция была пустой: `func RunTarpitForensics() {}`
func RunTarpitForensics() {
	rptLog := config.C.TarpitRptLog
	if rptLog == "" {
		rptLog = config.FileTarpitRptLog
	}

	fmt.Println("\n==============================================================")
	fmt.Println("           " + i18n.T.ForensicsReportTitle)
	fmt.Println("==============================================================")
	fmt.Printf("  "+i18n.T.ForensicsGenDate+"\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Printf("  "+i18n.T.ForensicsTargetLog+"\n", rptLog)
	fmt.Println("--------------------------------------------------------------")
	fmt.Printf("  %-9s | %-13s | %-15s | %-10s | %-10s | %s\n", i18n.T.ForensicsColTime, i18n.T.ForensicsColTrap, i18n.T.ForensicsColIP, i18n.T.ForensicsColPlanFact, i18n.T.ForensicsColBehavior, i18n.T.ForensicsColCountry)
	fmt.Println("--------------------------------------------------------------")

	// Открываем лог-файл для чтения
	file, err := os.Open(rptLog)
	if err != nil {
		fmt.Println("  " + i18n.T.ForensicsLogEmptyOrUnavailable)
		fmt.Println("==============================================================")
		return
	}
	defer file.Close()

	// Собираем все строки лога в память
	var historyLines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if text := scanner.Text(); strings.TrimSpace(text) != "" {
			historyLines = append(historyLines, text)
		}
	}

	// Если лог пуст — выводим заглушку
	if len(historyLines) == 0 {
		fmt.Println("  " + i18n.T.ForensicsLogEmpty)
		fmt.Println("==============================================================")
		return
	}

	// Выводим последние 50 записей в обратном порядке (новые сверху)
	displayCount := 0
	maxDisplay := 50
	for idx := len(historyLines) - 1; idx >= 0 && displayCount < maxDisplay; idx-- {
		lineText := historyLines[idx]
		parts := strings.Split(lineText, "\t")
		if len(parts) < 7 {
			continue
		}

		// Извлекаем ключевые поля для консольного отображения
		tsFull := parts[0]   // Дата/Время
		tarpitID := parts[1] // Идентификатор ловушки (TARPIT:22 / TARPIT:8080)
		ip := parts[2]       // IP-адрес
		planRaw := parts[3]  // План удержания
		factRaw := parts[4]  // Факт удержания
		behavior := parts[5] // Поведение
		country := parts[6]  // Страна
		// v8.2.3: идентификатор в фиксированную колонку, как в TUI-версии.
		idShort := shortTarpitID(tarpitID)

		// Извлекаем только время из полной даты
		timePart := tsFull
		if len(tsFull) > 11 {
			timePart = tsFull[11:]
		}

		// Форматируем план/факт для компактного отображения
		planShort := strings.ReplaceAll(planRaw, " sec", i18n.T.ForensicsSecShort)
		factShort := strings.ReplaceAll(factRaw, " sec", i18n.T.ForensicsSecShort)
		timeWindow := planShort + "/" + factShort

		// Сокращаем метку поведения для компактности
		behaviorShort := behavior
		if strings.Contains(behavior, "PASSIVE") {
			behaviorShort = "PASSIVE"
		} else if strings.Contains(behavior, "AGGRESSIVE") {
			behaviorShort = "AGRESS"
		}
		if len(behaviorShort) > 10 {
			behaviorShort = behaviorShort[:10]
		}

		// Очищаем страну от пробелов
		countryClean := strings.ReplaceAll(country, " ", "_")
		if countryClean == "" {
			countryClean = "UNKNOWN"
		}

		fmt.Printf("  %-9s | %-13s | %-15s | %-10s | %-10s | %s\n",
			timePart, idShort, ip, timeWindow, behaviorShort, countryClean)
		displayCount++
	}

	fmt.Println("--------------------------------------------------------------")
	fmt.Printf("  "+i18n.T.ForensicsTotalRecords+"\n", len(historyLines), displayCount)
	fmt.Println("==============================================================")
}

// ============================================================================
// TUI-РЕЖИМ: ГЕНЕРАЦИЯ СТРОК ДЛЯ LIVE_BOX
// ============================================================================

// GetTarpitLiveRows собирает историю завершённых сессий из лога и возвращает
// готовый массив строк для компонента `termui v3` (экран LIVE_BOX).
//
// АРХИТЕКТУРНОЕ РЕШЕНИЕ (v8.0.1):
//
//	В новой архитектуре единого демона воркеры живут в отдельном процессе.
//	Глобальная переменная `tarpit.ActiveSessions` в процессе TUI ВСЕГДА ПУСТА.
//	Поэтому мы отображаем ТОЛЬКО историю из `tarpit_report.log`.
//
//	Количество ботов в настоящий момент отображается через счётчик
//	в шапке экрана (из `/dev/shm/` через `tarpit.GetLiveBotsShmCount()`),
//	а не через `ActiveSessions`.
//
// ФОРМАТ КАЖДОЙ СТРОКИ:
//
//	🟢 [CLOSED] │ Время │ Ловушка │ IP-адрес │ План/Факт │ Поведение │ Страна
//
// ВОЗВРАЩАЕТ:
//   - Срез строк для отображения в `widgets.List` (TUI)
//   - Если лог пуст, возвращает единственную строку-заглушку
//
// ОБНОВЛЕНО В v8.0.1: Удалён блок активных сессий из `tarpit.ActiveSessions`.
// Отображается только история завершённых сессий из лога.
func GetTarpitLiveRows() []string {
	var rows []string

	// Определяем путь к криминалистическому логу.
	// Берём из глобального конфига, с фолбэком на стандартный путь.
	// ОБНОВЛЕНО В v8.0.1: Путь соответствует изолированному хабу бренда.
	rptLog := config.C.TarpitRptLog
	if rptLog == "" {
		rptLog = config.FileTarpitRptLog
	}

	// =============================================================
	// ЕДИНСТВЕННЫЙ СЛЕД: История завершённых сессий из файла отчётов
	//
	// В старой архитектуре здесь был ещё один блок — «След 1»,
	// который читал активные сессии из `tarpit.ActiveSessions` (sync.Map).
	// В новой архитектуре этот блок УДАЛЁН, потому что:
	//   1. Воркеры живут в процессе демона, а не в процессе TUI
	//   2. `sync.Map` не разделяется между процессами
	//   3. Активные боты отображаются через счётчик в шапке экрана
	//
	// ОБНОВЛЕНО В v8.0.1.
	// =============================================================

	// Открываем лог-файл для чтения
	file, err := os.Open(rptLog)
	if err != nil {
		// Если файл не существует или недоступен — возвращаем заглушку.
		// Это нормальная ситуация для первого старта, когда ловушка
		// ещё не поймала ни одного бота.
		rows = append(rows, fmt.Sprintf(" [%s]%s", i18n.T.ForensicsTrapEmpty, colorYellow))
		return rows
	}
	defer file.Close()

	// Собираем все непустые строки лога в память.
	// Это необходимо для отображения в обратном порядке (новые сверху).
	var historyLines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if text := scanner.Text(); strings.TrimSpace(text) != "" {
			historyLines = append(historyLines, text)
		}
	}

	// Если лог пуст — возвращаем заглушку
	if len(historyLines) == 0 {
		rows = append(rows, fmt.Sprintf(" [%s]%s", i18n.T.ForensicsTrapEmpty, colorYellow))
		return rows
	}

	// =============================================================
	// ПАРСИНГ СТРОК ЛОГА В ОБРАТНОМ ПОРЯДКЕ (новые сверху)
	//
	// Каждая строка лога содержит 13 колонок через табуляцию.
	// Для LIVE_BOX нам нужны только 6 ключевых полей:
	//   [0] Время, [2] IP, [3] План, [4] Факт, [5] Поведение, [6] Страна
	//
	// Ограничение: выводим не более 50 строк, чтобы не перегружать
	// терминальный буфер при массовых атаках.
	// =============================================================
	outputCount := 0
	maxRows := 50

	for idx := len(historyLines) - 1; idx >= 0; idx-- {
		if outputCount >= maxRows {
			break // Достигли лимита отображения
		}

		lineText := historyLines[idx]
		parts := strings.Split(lineText, "\t")

		// Минимальный набор колонок для корректного парсинга.
		// Нужно хотя бы 7 колонок: Время, TarpitID, IP, План, Факт, Поведение, Страна.
		// ОБНОВЛЕНО В v8.0.1: Проверка на 7 колонок вместо 13,
		// так как для отображения нужны только первые 7.
		if len(parts) < 7 {
			continue
		}

		// Извлекаем переменные по строго утверждённым индексам
		tsFull := parts[0]      // [0] Дата/Время начала атаки
		tarpitID := parts[1]    // [1] Идентификатор ловушки (TARPIT:22 / TARPIT:2222)
		ip := parts[2]          // [2] IP-адрес атакующего
		planRaw := parts[3]     // [3] ПЛАНИРУЕМОЕ удержание (джиттер)
		factRaw := parts[4]     // [4] ФАКТИЧЕСКОЕ удержание
		behaviorRaw := parts[5] // [5] Поведение сканера
		country := parts[6]     // [6] Страна источника (GeoIP)
		// v8.2.3: идентификатор в фиксированную колонку, чтобы таблица не ехала.
		idShort := shortTarpitID(tarpitID)

		// Извлекаем только время из полной даты.
		// Формат даты: "2006-01-02 15:04:05" → берём "15:04:05"
		timePart := tsFull
		if len(tsFull) > 11 {
			timePart = tsFull[11:]
		}

		// Форматируем план и факт для компактного отображения.
		// Заменяем " sec" на "с" для экономии ширины колонки.
		// ОБНОВЛЕНО В v8.0.1: В новой архитектуре `planRaw` = запланированный
		// джиттер, `factRaw` = фактическое удержание. Оба значения корректны.
		planShort := strings.ReplaceAll(planRaw, " sec", i18n.T.ForensicsSecShort)
		factShort := strings.ReplaceAll(factRaw, " sec", i18n.T.ForensicsSecShort)
		formattedTimeWindow := planShort + "/" + factShort

		// Очищаем страну от пробелов (для корректного отображения в таблице)
		countryClean := strings.ReplaceAll(country, " ", "_")
		if countryClean == "" {
			countryClean = "UNKNOWN"
		}

		// Сокращаем метку поведения для компактности.
		// "PASSIVE_SCANNER" → "PASSIVE"
		// "AGGRESSIVE_SCANNER" → "AGRESS"
		behaviorShort := behaviorRaw
		if strings.Contains(behaviorRaw, "PASSIVE") {
			behaviorShort = "PASSIVE"
		} else if strings.Contains(behaviorRaw, "AGGRESSIVE") {
			behaviorShort = "AGRESS"
		}
		// Обрезаем до 7 символов для выравнивания колонки
		if len(behaviorShort) > 7 {
			behaviorShort = behaviorShort[:7]
		}

		// Собираем итоговую строку для отображения в TUI.
		// Формат: 🟢 [CLOSED] │ Время │ Ловушка │ IP │ План/Факт │ Поведение │ Страна
		// Цвет накладывается строго на текст [CLOSED], эмодзи идёт отдельно.
		// ОБНОВЛЕНО В v8.0.1. v8.2.3: добавлена колонка идентификатора ловушки.
		line := fmt.Sprintf(" 🟢 "+i18n.T.ForensicsStatusClosed+"(fg:green,mod:bold) │ %s │ %-12s │ %-15s │ %-7s │ %-7s │ %s",
			timePart, idShort, ip, formattedTimeWindow, behaviorShort, countryClean)

		rows = append(rows, line)
		outputCount++
	}

	// Если после парсинга не осталось ни одной строки (все строки повреждены)
	// — возвращаем заглушку пустого капкана.
	// ОБНОВЛЕНО В v8.0.1.
	if len(rows) == 0 {
		rows = append(rows, fmt.Sprintf(" [%s]%s", i18n.T.ForensicsTrapEmpty, colorYellow))
	}

	return rows
}
