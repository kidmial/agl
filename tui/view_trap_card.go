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

	"agl/i18n"
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
	header.Title = fmt.Sprintf(" [ %s │ %s %d ] ", i18n.T.ViewTrapCardHeaderTitle, i18n.T.ViewTrapCardLabelPort, SelectedTrapPort)

	var headerText string
	if !trapExists {
		headerText = i18n.T.ViewTrapCardErrNotFound
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
			"  %s: %s\n"+
				"  %s   : %s\n"+
				"  %s    : %d %s ± %d %s │ %s: %d\n"+
				"  %s  %s    : DAEMON: %s │ MARKER: %s | %s: %v",
			i18n.T.ViewTrapCardLabelIdentifier, trap.TarpitID,
			i18n.T.ViewTrapCardLabelSshBanner, trap.ConfigConfig.SSHVersion,
			i18n.T.ViewTrapCardLabelRetention, trap.ConfigConfig.DelayStep, i18n.T.ViewTrapCardLabelSec, trap.ConfigConfig.DelayVariance, i18n.T.ViewTrapCardLabelSec, i18n.T.ViewTrapCardLabelSessionLimit, trap.ConfigConfig.MaxConn,
			i18n.T.ViewTrapCardLabelStatus, markerStr, declaredStr, daemonStr, i18n.T.ViewTrapCardLabelCore, trap.IsCore)
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
		menuRows = append(menuRows, fmt.Sprintf("  [ BACK ]   │ %s", i18n.T.ViewTrapCardRowBackToList))
	} else {
		// START
		if !isRunning {
			menuRows = append(menuRows, fmt.Sprintf("  [ START ]  │ %s", i18n.T.ViewTrapCardRowStartActivate))
		} else {
			menuRows = append(menuRows, fmt.Sprintf("  [ START ]  │ %s", i18n.T.ViewTrapCardRowStartAlreadyActive))
		}
		// STOP
		if isRunning {
			menuRows = append(menuRows, fmt.Sprintf("  [ STOP ]   │ %s", i18n.T.ViewTrapCardRowStopDeactivate))
		} else {
			menuRows = append(menuRows, fmt.Sprintf("  [ STOP ]   │ %s", i18n.T.ViewTrapCardRowStopAlreadyStopped))
		}
		// EDIT
		if !isRunning {
			menuRows = append(menuRows, fmt.Sprintf("  [ EDIT ]   │ %s", i18n.T.ViewTrapCardRowEditParams))
		} else {
			menuRows = append(menuRows, fmt.Sprintf("  [ EDIT ]   │ %s", i18n.T.ViewTrapCardRowEditRequiresStop))
		}
		// DELETE
		if !isRunning && !markerExists {
			menuRows = append(menuRows, fmt.Sprintf("  [ DELETE ] │ %s", i18n.T.ViewTrapCardRowDeleteFull))
		} else if !isRunning && markerExists {
			menuRows = append(menuRows, fmt.Sprintf("  [ DELETE ] │ %s", i18n.T.ViewTrapCardRowDeleteWaiting))
		} else {
			menuRows = append(menuRows, fmt.Sprintf("  [ DELETE ] │ %s", i18n.T.ViewTrapCardRowDeleteRequiresStop))
		}
		// BACK
		menuRows = append(menuRows, fmt.Sprintf("  [ BACK ]   │ %s", i18n.T.ViewTrapCardRowBackToPool))
	}

	actionList := widgets.NewList()
	actionList.Title = i18n.T.ViewTrapCardListTitle
	actionList.Rows = menuRows
	actionList.SetRect(2, 8, 102, 16)
	actionList.BorderStyle.Fg = ui.ColorCyan
	actionList.SelectedRow = selectedRow
	actionList.SelectedRowStyle = ui.NewStyle(ui.ColorBlack, ui.ColorGreen, ui.ModifierBold)

	// -------------------------------------------------------------
	// КОНТЕСТНАЯ ПОДСКАЗКА
	// -------------------------------------------------------------
	hintBox := widgets.NewParagraph()
	hintBox.Title = i18n.T.ViewTrapCardHintTitle
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
		return i18n.T.ViewTrapCardHintNotFound
	}
	switch row {
	case TrapCardStart:
		if isRunning {
			return i18n.T.ViewTrapCardHintStartAlreadyActive
		}
		// Подробная информация о процессе запуска
		return i18n.T.ViewTrapCardHintStartInfo

	case TrapCardStop:
		if !isRunning {
			return i18n.T.ViewTrapCardHintStopAlreadyStopped
		}
		// Подробная информация о процессе остановки
		return i18n.T.ViewTrapCardHintStopInfo

	case TrapCardEdit:
		if isRunning {
			// Причина блокировки действий
			return " [[ИНФО]](fg:yellow,mod:bold) " + i18n.T.ViewTrapCardHintEditBlocked
		}
		// Список изменяемых параметров
		return i18n.T.ViewTrapCardHintEditInfo

	case TrapCardDelete:
		if isRunning {
			return " [[ИНФО]](fg:yellow,mod:bold) " + i18n.T.ViewTrapCardHintDeleteBlocked
		}
		if markerExists {
			return i18n.T.ViewTrapCardHintDeleteWaiting
		}
		return i18n.T.ViewTrapCardHintDeleteInfo

	case TrapCardBack:
		// Здесь можно убрать вообще, если это стандартный элемент TUI,
		// но если оставить, пусть будет кратко:
		return i18n.T.ViewTrapCardHintBack
	}
	return ""
}
