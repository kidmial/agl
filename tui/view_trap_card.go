// tui/view_trap_card.go
// ============================================================================
// КОМПОНЕНТ: Карточка управления конкретной ловушкой (TRAP_CARD)
// ВЕРСИЯ: v8.0.3
// РОЛЬ: Экран-карточка, открываемый при проваливании (ENTER) в ловушку
//
//	из списка MULTI_TARPIT. Содержит действия:
//	START / STOP / EDIT / DELETE / BACK.
//
// ============================================================================
package tui

import (
	"fmt"
	"os"

	"agl/internal/sys"

	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
)

// SelectedTrapPort хранит порт ловушки, для которой открыта карточка.
// Устанавливается в view_router.go при проваливании в карточку.
var SelectedTrapPort int

// Индексы строк действий в карточке ловушки.
const (
	TrapCardStart = iota
	TrapCardStop
	TrapCardEdit
	TrapCardDelete
	TrapCardBack
)

// DrawTrapCardGrid отрисовывает карточку управления ловушкой.
// Вызывается из view_router.go при навигации на экран TRAP_CARD.
func DrawTrapCardGrid(selectedRow int) ([]ui.Drawable, int) {
	// Загружаем инвентарь
	inv, err := sys.LoadTarpitInventory(sys.TarpitInventoryPath)
	trapExists := false
	var trap sys.TarpitDefinition
	if err == nil {
		trapID := sys.GenerateTarpitID(SelectedTrapPort)
		trap, trapExists = inv.Traps[trapID]
	}

	// Вычисляем состояние маркера
	markerExists := false
	if trapExists && trap.MarkerFilePath != "" {
		if _, errStat := os.Stat(trap.MarkerFilePath); errStat == nil {
			markerExists = true
		}
	}

	isRunning := trapExists && trap.State == sys.StateRunning

	// -------------------------------------------------------------
	// ШАПКА: детали ловушки и трёхфакторный статус
	// -------------------------------------------------------------
	header := widgets.NewParagraph()
	header.Title = fmt.Sprintf(" [ Карточка Ловушки │ Порт %d ] ", SelectedTrapPort)

	var headerText string
	if !trapExists {
		headerText = "  [ОШИБКА]: Ловушка не найдена в инвентаре.\n" +
			"  Возможно, она была удалена. Вернитесь назад."
	} else {
		declaredStr := "[ STOPPED ](fg:red)"
		if isRunning {
			declaredStr = "[ RUNNING ](fg:green,mod:bold)"
		}
		daemonStr := "[ DISABLED ](fg:white)"
		if trap.Status == sys.StatusActive {
			daemonStr = "[ ACTIVE ](fg:yellow,mod:bold)"
		}
		markerStr := "[○](fg:white)"
		if markerExists {
			markerStr = "[●](fg:green,mod:bold)"
		}

		headerText = fmt.Sprintf(
			"  Идентификатор: %s\n"+
				"  SSH-баннер   : %s\n"+
				"  Удержание    : %d сек ± %d сек │ Лимит сессий: %d\n"+
				"  Статус  %s    : DAEMON: %s │ MARKER: %s | Ядро: %v",
			trap.TarpitID,
			trap.ConfigConfig.SSHVersion,
			trap.ConfigConfig.DelayStep, trap.ConfigConfig.DelayVariance,
			trap.ConfigConfig.MaxConn,
			markerStr, declaredStr, daemonStr, trap.IsCore)
	}
	header.Text = headerText
	header.SetRect(2, 1, 102, 8)
	header.BorderStyle.Fg = ui.ColorCyan
	header.TitleStyle.Fg = ui.ColorYellow
	header.TitleStyle.Modifier = ui.ModifierBold
	header.WrapText = false

	// -------------------------------------------------------------
	// СПИСОК ДЕЙСТВИЙ
	// -------------------------------------------------------------
	var menuRows []string
	if !trapExists {
		menuRows = append(menuRows, "  [ BACK ]   │ Вернуться в список")
	} else {
		// START
		if !isRunning {
			menuRows = append(menuRows, "  [ START ]  │ Активация ловушки")
		} else {
			menuRows = append(menuRows, "  [ START ]  │ Ловушка уже активна")
		}
		// STOP
		if isRunning {
			menuRows = append(menuRows, "  [ STOP ]   │ Остановка ловушки")
		} else {
			menuRows = append(menuRows, "  [ STOP ]   │ Ловушка уже остановлена")
		}
		// EDIT
		if !isRunning {
			menuRows = append(menuRows, "  [ EDIT ]   │ Редактировать параметры ловушки")
		} else {
			menuRows = append(menuRows, "  [ EDIT ]   │ Требуется STOP для редактирования")
		}
		// DELETE
		if !isRunning && !markerExists {
			menuRows = append(menuRows, "  [ DELETE ] │ Полностью удалить из инвентаря")
		} else if !isRunning && markerExists {
			menuRows = append(menuRows, "  [ DELETE ] │ Ожидание завершения демона...")
		} else {
			menuRows = append(menuRows, "  [ DELETE ] │ Требуется STOP для удаления")
		}
		// BACK
		menuRows = append(menuRows, "  [ BACK ]   │ Вернуться к списку")
	}

	actionList := widgets.NewList()
	actionList.Title = " Действия с ловушкой: "
	actionList.Rows = menuRows
	actionList.SetRect(2, 8, 102, 16)
	actionList.BorderStyle.Fg = ui.ColorCyan
	actionList.SelectedRow = selectedRow
	actionList.SelectedRowStyle = ui.NewStyle(ui.ColorBlack, ui.ColorGreen, ui.ModifierBold)

	// -------------------------------------------------------------
	// КОНТЕСТНАЯ ПОДСКАЗКА
	// -------------------------------------------------------------
	hintBox := widgets.NewParagraph()
	hintBox.Title = " Контекстная справка "
	hintBox.Text = getTrapCardHint(selectedRow, trapExists, isRunning, markerExists)
	hintBox.SetRect(2, 16, 102, 22)
	hintBox.BorderStyle.Fg = ui.ColorYellow
	hintBox.TextStyle.Fg = ui.ColorCyan
	hintBox.WrapText = false

	return []ui.Drawable{header, actionList, hintBox}, len(actionList.Rows)
}

// getTrapCardHint возвращает контекстную подсказку для выбранного действия.
func getTrapCardHint(row int, trapExists, isRunning, markerExists bool) string {
	if !trapExists {
		// Базовая справка, максимальная краткость
		return " Ловушка не найдена в инвентаре. Нажмите BACK для возврата."
	}
	switch row {
	case TrapCardStart:
		if isRunning {
			return " Ловушка уже активна.\n Подробнее: Проверьте статус в списке."
		}
		// Подробная информация о процессе запуска
		return " Запуск ловушки.\n 1. Изменяется статус в инвентаре (RUNNING).\n 2. Открывается порт через Firewall.\n 3. Демон поднимает слушатель в течение нескольких секунд."

	case TrapCardStop:
		if !isRunning {
			return " Ловушка уже остановлена."
		}
		// Подробная информация о процессе остановки
		return " Остановка ловушки.\n 1. Изменяется статус в инвентаре (STOPPED).\n 2. Порт закрывается (Firewall).\n 3. Демон удаляет слушатель и квитанцию ловушки."

	case TrapCardEdit:
		if isRunning {
			// Причина блокировки действий
			return " [[ИНФО]](fg:yellow,mod:bold) Редактирование недоступно.\n Редактирование параметров ловушки возможно только после её остановки.\n (STOP -> EDIT)"
		}
		// Список изменяемых параметров
		return " Редактирование параметров:\n - SSH Version (Баннер), DelayStep / DelayVariance (Удержание), Max Connections (Сессии)\n [Важно] Порт ловушки не меняется после создания."

	case TrapCardDelete:
		if isRunning {
			return " [[ИНФО]](fg:yellow,mod:bold) Удаление недоступно.\n Удалить ловушку возможно только после её остановки.\n (STOP -> DELETE)"
		}
		if markerExists {
			return " Ожидание завершения.\n Демон ещё очищает служебные файлы.\n Повторите через несколько секунд."
		}
		return " Полное удаление ловушки из инвентаря.\n ДЕЙСТВИЕ НЕОБРАТИМО.\n Если это была последняя ловушка, демон остановится автоматически."

	case TrapCardBack:
		// Здесь можно убрать вообще, если это стандартный элемент TUI,
		// но если оставить, пусть будет кратко:
		return " Возврат в главный диспетчер пула MULTI TARPIT."
	}
	return ""
}
