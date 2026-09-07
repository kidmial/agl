// tui/view_multi_tarpit.go
// ============================================================================
// КОМПОНЕНТ: Графический хаб пула распределённых мульти-ловушек (MULTI_TARPIT)
// ВЕРСИЯ: v8.0.1
// РОЛЬ: Визуализация всех ловушек из `tarpit_inventory.json` с трёхфакторной
//
//	индикацией состояния: декларация (state) / подтверждение (status) /
//	факт на диске (маркер).
//
// ============================================================================
//
// АРХИТЕКТУРА:
//
//	Этот файл — ЧИСТЫЙ РЕНДЕРЕР. Он только ОТРИСОВЫВАЕТ список ловушек.
//	Все действия (активация/деактивация/переходы) обрабатываются в
//	`view_router.go` через `tarpitctl`.
//
// ТРЁХФАКТОРНАЯ ИНДИКАЦИЯ СОСТОЯНИЯ:
//
//	Для каждой ловушки показываются три статуса:
//
//	1. DECLARED (state) — что админ ХОЧЕТ
//	   Источник: поле `state` в `tarpit_inventory.json`
//	   Значения: [RUNNING] (зелёный) / [STOPPED] (красный)
//
//	2. DAEMON (status) — что демон ПОДТВЕРДИЛ
//	   Источник: поле `status` в `tarpit_inventory.json`
//	   Значения: [ACTIVE] (зелёный) / [DISABLED] (серый)
//
//	3. MARKER (факт) — существует ли маркер на диске
//	   Источник: проверка `os.Stat()` на `marker_file_path`
//	   Значения: [●] (зелёная точка) / [○] (серая точка)
//
//	Админ может сразу увидеть расхождения:
//	- DECLARED=RUNNING, DAEMON=DISABLED, MARKER=○ → демон не поднял слушатель
//	- DECLARED=STOPPED, DAEMON=ACTIVE, MARKER=● → демон не снял слушатель
//
// ### [2026-08-22] — Мажорный рефакторинг хаба (v8.0.1)
// - Добавлена трёхфакторная индикация состояния (state/status/marker).
// - Добавлено визуальное выделение базовой ловушки (is_core) префиксом `[CORE]`.
// - Добавлено отображение `activated_at` и `worker_pid` в строке ловушки.
// - Обновлена терминология: `Jitter` → `DelayVariance`.
// - Добавлена проверка существования маркера для индикации факта.
// - Обновлены комментарии и история изменений.
// ============================================================================
package tui

import (
	"fmt"
	"os"
	"sort"

	// "time"

	"agl/internal/sys"

	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
)

// ============================================================================
// ОСНОВНАЯ ФУНКЦИЯ ОТРИСОВКИ
// ============================================================================

// DrawMultiTarpitGrid динамически формирует сетку управления распределёнными
// капканами на основе декларативного инвентаря.
//
// ПАРАМЕТРЫ:
//   - selectedRow: индекс строки, на которой сейчас стоит курсор
//
// ВОЗВРАЩАЕТ:
//   - Слайс виджетов для рендера
//   - Общее количество строк (для роутера — ограничение навигации)
//
// ОБНОВЛЕНО В v8.0.1: Добавлена трёхфакторная индикация и расширенная информация.
func DrawMultiTarpitGrid(selectedRow int) ([]ui.Drawable, int) {
	// -------------------------------------------------------------
	// 1. ФИКСИРОВАННАЯ ШАПКА (Высота 4 строки)
	// -------------------------------------------------------------
	header := widgets.NewParagraph()
	header.Title = " MULTI-TARPIT - Пул Распределенных Мульти-Ловушек "
	header.Text = " Управление активными горутинами-воркерами эмуляции служб.\n" +
		" Декларативный стейт: [DECLARED](fg:cyan) = Что хочет админ │\n" +
		" [DAEMON](fg:yellow) = Что подтвердил демон │ [MARKER](fg:green) = Присутствие маркера на диске."
	header.SetRect(2, 1, 102, 6)
	header.BorderStyle.Fg = ui.ColorCyan
	header.TitleStyle.Fg = ui.ColorYellow
	header.TitleStyle.Modifier = ui.ModifierBold
	header.WrapText = false

	// Устанавливаем ширину текста для более компактного отображения
	// header.TextStyle.Width = 102 - len(header.Text)

	// -------------------------------------------------------------
	// 2. ЗАГРУЗКА ДЕКЛАРАТИВНОГО ИНВЕНТАРЯ
	// -------------------------------------------------------------
	var menuRows []string

	inv, err := sys.LoadTarpitInventory(sys.TarpitInventoryPath)
	if err != nil {
		// Если инвентарь недоступен — показываем сообщение об ошибке
		menuRows = append(menuRows,
			" [ОШИБКА]: Не удалось загрузить инвентарь ловушек из "+sys.TarpitInventoryPath)
	} else if len(inv.Traps) == 0 {
		// Инвентарь пуст — показываем подсказку
		menuRows = append(menuRows,
			" [ПУСТО]: В инвентаре нет ни одной ловушки. Нажмите [ ADD NEW TRAP ] для создания.")
	} else {
		// ---------------------------------------------------------
		// СОРТИРОВКА КЛЮЧЕЙ ДЛЯ СТАБИЛЬНОГО ПОРЯДКА
		// Без сортировки карта Go возвращает ключи в случайном порядке.
		// Это критично для маппинга индексов строк в `view_router.go`.
		// ---------------------------------------------------------
		var trapIDs []string
		for id := range inv.Traps {
			trapIDs = append(trapIDs, id)
		}
		sort.Strings(trapIDs)

		// ---------------------------------------------------------
		// ФОРМИРОВАНИЕ СТРОК ДЛЯ КАЖДОЙ ЛОВУШКИ
		// ---------------------------------------------------------
		for _, id := range trapIDs {
			trap := inv.Traps[id]

			// === ФАКТОР 1: DECLARED (state) ===
			// Что админ ХОЧЕТ (источник: поле `state` в JSON).
			declaredStr := "[ STOPPED ](fg:red)"
			if trap.State == sys.StateRunning {
				declaredStr = "[ RUNNING ](fg:green,mod:bold)"
			}

			// === ФАКТОР 2: DAEMON (status) ===
			// Что демон ПОДТВЕРДИЛ (источник: поле `status` в JSON).
			daemonStr := "[ DISABLED ](fg:white)"
			if trap.Status == sys.StatusActive {
				daemonStr = "[ ACTIVE ](fg:yellow,mod:bold)"
			}

			// === ФАКТОР 3: MARKER (факт на диске) ===
			// Существует ли маркер (источник: `os.Stat`).
			markerStr := "[○](fg:white)" // Маркер отсутствует
			if trap.MarkerFilePath != "" {
				if _, errStat := os.Stat(trap.MarkerFilePath); errStat == nil {
					markerStr = "[●](fg:green,mod:bold)" // Маркер существует
				}
			}

			// === ПРЕФИКС CORE (для базовой ловушки порта 22) ===
			corePrefix := ""
			if trap.IsCore {
				corePrefix = "[CORE](fg:magenta,mod:bold) "
			}

			// === РАНТАЙМ-МЕТАДАННЫЕ ===
			// `activated_at` и `worker_pid` для диагностики.
			// Собираем финальную строку ловушки.
			// ФОРМАТ v8.0.3: компактная строка без PID/activated_at —
			// рантайм-метаданные перенесены в «Паспорт ловушки» (нижний блок),
			// чтобы исключить обрезание строк в списке.
			menuRows = append(menuRows, fmt.Sprintf(
				" %s%-29s │ Порт: %-5d │ D: %s │ M: %s │ %s",
				corePrefix, id, trap.Port,
				declaredStr, daemonStr, markerStr))
		}
	}

	// -------------------------------------------------------------
	// 3. ДОБАВЛЕНИЕ АДМИНИСТРАТИВНЫХ КНОПОК В ХВОСТ СПИСКА
	// -------------------------------------------------------------
	totalTrapsCount := len(menuRows)

	// Кнопка добавления новой ловушки
	menuRows = append(menuRows,
		" [ ADD NEW TRAP ]              │ Развернуть новый рубеж капкана через мастер TRAP_CREATOR")

	// Кнопка возврата в главное меню
	menuRows = append(menuRows,
		" [ BACK ]                      │ Закрыть диспетчер капканов и вернуться в Главное меню")

	// -------------------------------------------------------------
	// 4. БЛОК СПИСКА ЛОВУШЕК (Динамическая высота)
	// -------------------------------------------------------------
	trapList := widgets.NewList()
	trapList.Title = fmt.Sprintf(" Список активных рубежей (всего: %d) ", totalTrapsCount)
	trapList.Rows = menuRows
	// ГЕОМЕТРИЯ v8.0.3: фиксированная высота 12 строк (y: 6-18).
	// Нижняя половина терминала отдана под «Паспорт ловушки».
	trapList.SetRect(2, 6, 102, 18)
	trapList.BorderStyle.Fg = ui.ColorCyan
	trapList.SelectedRow = selectedRow
	trapList.SelectedRowStyle = ui.NewStyle(ui.ColorBlack, ui.ColorGreen, ui.ModifierBold)

	// -------------------------------------------------------------
	// 5. ИНФОРМАЦИОННЫЙ БЛОК (Справка)
	// -------------------------------------------------------------
	infoBox := widgets.NewParagraph()
	infoBox.Title = " Паспорт ловушки / Системная справка "
	// Базовая справка по пулу
	helpText := fmt.Sprintf(
		" Всего интегрировано рубежей: %d\n"+
			" Декларативный инвентарь: %s\n"+
			" Трёхфакторная индикация: DECLARED (state) │ DAEMON (status) │ MARKER (файл на диске)\n"+
			" [ENTER] на ловушке — проваливание в карточку управления (START/STOP/EDIT/DELETE).\n"+
			" [ESC] — возврат в Главное меню.",
		totalTrapsCount, sys.TarpitInventoryPath)
	// Если выбрана реальная ловушка (не кнопка) — показываем полный паспорт
	if inv != nil && selectedRow < totalTrapsCount {
		var trapIDs []string
		for id := range inv.Traps {
			trapIDs = append(trapIDs, id)
		}
		sort.Strings(trapIDs)
		if selectedRow < len(trapIDs) {
			selectedTrap := inv.Traps[trapIDs[selectedRow]]
			// Рантайм-метаданные демона (перенесены сюда из строки списка)
			runtimeLine := " Воркер: не поднят (демон ещё не подтвердил активацию)"
			if selectedTrap.ActivatedAt != "" {
				runtimeLine = fmt.Sprintf(" Воркер: PID %d │ Поднят: %s",
					selectedTrap.WorkerPID, selectedTrap.ActivatedAt)
			}
			helpText = fmt.Sprintf(
				" Ловушка: %s │ Порт: %d │ Ядро: %v\n"+
					" SSH-баннер: %s\n"+
					" Удержание: %d сек ± %d сек\n"+
					" Лимит сессий: %d\n"+
					"%s\n"+
					" Маркер: %s\n\n"+
					" [ENTER] — карточка управления (START/STOP/EDIT/DELETE). [ESC] — выход.",
				selectedTrap.TarpitID, selectedTrap.Port, selectedTrap.IsCore,
				selectedTrap.ConfigConfig.SSHVersion,
				selectedTrap.ConfigConfig.DelayStep,
				selectedTrap.ConfigConfig.DelayVariance,
				selectedTrap.ConfigConfig.MaxConn,
				runtimeLine,
				selectedTrap.MarkerFilePath)
		}
	}
	infoBox.Text = helpText
	// ГЕОМЕТРИЯ v8.0.3: паспорт растянут до нижней границы терминала (y: 18-30)
	infoBox.SetRect(2, 18, 102, 30)
	infoBox.BorderStyle.Fg = ui.ColorYellow
	infoBox.TextStyle.Fg = ui.ColorCyan
	infoBox.WrapText = false

	return []ui.Drawable{header, trapList, infoBox}, len(trapList.Rows)
}

// ============================================================================
// ВСПОМОГАТЕЛЬНАЯ ФУНКЦИЯ: ОБНОВЛЕНИЕ СЧЁТЧИКОВ (для будущего использования)
// ============================================================================

// GetMultiTarpitSummary возвращает краткую сводку по пулу ловушек.
// Используется для отображения в шапке главного меню.
//
// ВОЗВРАЩАЕТ:
//   - total: общее количество ловушек
//   - running: количество ловушек со state=RUNNING
//   - active: количество ловушек со status=ACTIVE
//   - с маркером: количество ловушек с существующим маркером
//
// НОВОЕ В v8.0.1.
func GetMultiTarpitSummary() (total, running, active, withMarker int) {
	inv, err := sys.LoadTarpitInventory(sys.TarpitInventoryPath)
	if err != nil {
		return 0, 0, 0, 0
	}

	for _, trap := range inv.Traps {
		total++
		if trap.State == sys.StateRunning {
			running++
		}
		if trap.Status == sys.StatusActive {
			active++
		}
		if trap.MarkerFilePath != "" {
			if _, errStat := os.Stat(trap.MarkerFilePath); errStat == nil {
				withMarker++
			}
		}
	}
	return
}

// ============================================================================
// ИСТОРИЯ ИЗМЕНЕНИЙ
// ============================================================================
//
// ### [2026-08-22] — Мажорный рефакторинг хаба (v8.0.1)
// - Добавлена трёхфакторная индикация состояния (state/status/marker).
// - Добавлено визуальное выделение базовой ловушки (is_core) префиксом [CORE].
// - Добавлено отображение `activated_at` и `worker_pid` в строке ловушки.
// - Обновлена терминология: `Jitter` → `DelayVariance`.
// - Добавлена проверка существования маркера для индикации факта.
// - Добавлена функция `GetMultiTarpitSummary()` для сводки.
// - Динамическая справка показывает параметры выбранной ловушки.
//
// ### [2024-XX-XX] — Переход на динамическое чтение стейта (v7.1.2)
// - Полный переход со статических заглушек на чтение реального JSON-инвентаря.
//
// ### [2024-XX-XX] — Создание файла
// - Базовая реализация экрана MULTI_TARPIT.
