// tui/view_trap_editor.go
// ============================================================================
// КОМПОНЕНТ: Редактор параметров существующей ловушки (TRAP_EDITOR)
// ВЕРСИЯ: v8.0.3
// РОЛЬ: Экран редактирования параметров существующей мульти-ловушки.
//
//	Открывается из карточки ловушки (TRAP_CARD) по действию EDIT.
//	Порт доступен только для чтения. Изменяемые параметры:
//	DelayStep, DelayVariance, MaxConn, SSHVersion (баннер).
//
// ============================================================================
//
// АРХИТЕКТУРНАЯ ПОЗИЦИЯ:
//
//	Экран открывается из TRAP_CARD по действию EDIT (только для остановленной
//	ловушки). Загружает существующую конфигурацию в черновик `TrapEditDraft`,
//	позволяет изменить параметры и сохранить обратно в инвентарь через
//	атомарную запись `sys.SaveTarpitInventory` (POSIX).
//
// МАППИНГ СТРОК:
//
//	0 — Порт (только чтение)
//	1 — DelayStep
//	2 — DelayVariance
//	3 — SSHVersion (переключатель пресетов)
//	4 — MaxConn
//	5 — SAVE
//	6 — CANCEL
//
// ============================================================================
package tui

import (
	"fmt"
	"strconv"
	"strings"

	"agl/i18n"
	"agl/internal/service"
	"agl/internal/sys"

	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
)

// TrapEditDraft хранит черновик редактируемой ловушки.
// Порт фиксируется при загрузке и не подлежит изменению.
var TrapEditDraft = TrapDraft{}

// LoadTrapIntoEditor загружает конфигурацию существующей ловушки в черновик редактора.
// Вызывается из view_router.go при переходе в экран TRAP_EDITOR.
func LoadTrapIntoEditor(port int) error {
	inv, err := sys.LoadTarpitInventory(sys.TarpitInventoryPath)
	if err != nil {
		return fmt.Errorf(i18n.T.ViewTrapEditorErrLoadInventory, err)
	}
	trapID := sys.GenerateTarpitID(port)
	trap, exists := inv.Traps[trapID]
	if !exists {
		return fmt.Errorf("%s", fmt.Sprintf(i18n.T.ViewTrapEditorErrNotFound, trapID))
	}
	// Заполняем черновик из существующей конфигурации.
	// Порт фиксируется (только чтение).
	TrapEditDraft.Port = trap.Port
	TrapEditDraft.DelayStep = trap.ConfigConfig.DelayStep
	TrapEditDraft.DelayVariance = trap.ConfigConfig.DelayVariance
	TrapEditDraft.MaxConn = trap.ConfigConfig.MaxConn
	// Определяем индекс баннера по совпадению строки.
	TrapEditDraft.BannerIndex = 0
	for i, preset := range SSHVersionPresets {
		if preset == trap.ConfigConfig.SSHVersion {
			TrapEditDraft.BannerIndex = i
			break
		}
	}
	return nil
}

// Геометрия колонок редактора (v9.0.0): паддинг в коде, словари чистые.
const (
	trapEditorLabelW = 23
	trapEditorValueW = 22
	trapEditorTagW   = 23 // Операционные кнопки
)

// DrawTrapEditorGrid отрисовывает экран редактирования ловушки.
// Возвращает срез виджетов и общее количество строк.
func DrawTrapEditorGrid(selectedRow int) ([]ui.Drawable, int) {
	var menuRows []string

	// v9.0.0: стандарт меток/описаний: метка | значение | описание, паддинг в коде.
	menuRows = append(menuRows, fmt.Sprintf(" %s │ %s │ %s",
		padVisible(i18n.T.ViewTrapCreatorRowPortLabel, trapEditorLabelW),
		padVisible(fmt.Sprintf("[ %d ]", TrapEditDraft.Port), trapEditorValueW),
		i18n.T.ViewTrapEditorRowPortReadOnlyDesc))
	menuRows = append(menuRows, fmt.Sprintf(" %s │ %s │ %s",
		padVisible(i18n.T.ViewTrapCreatorRowDelayStepLabel, trapEditorLabelW),
		padVisible(fmt.Sprintf("[ %d %s ]", TrapEditDraft.DelayStep, i18n.T.ViewTrapCreatorLabelSec), trapEditorValueW),
		i18n.T.ViewTrapCreatorRowDelayStepDesc))
	menuRows = append(menuRows, fmt.Sprintf(" %s │ %s │ %s",
		padVisible(i18n.T.ViewTrapCreatorRowVarianceLabel, trapEditorLabelW),
		padVisible(fmt.Sprintf("[ %d %s ]", TrapEditDraft.DelayVariance, i18n.T.ViewTrapCreatorLabelSec), trapEditorValueW),
		i18n.T.ViewTrapCreatorRowVarianceDesc))
	// Короткое имя пресета для колонки значения: полный баннер не влезает в колонку.
	presetName := "Ubuntu Standard"
	switch TrapEditDraft.BannerIndex {
	case 1:
		presetName = "Legacy Enterprise"
	case 2:
		presetName = "Embedded IoT"
	case 3:
		presetName = "Stealth Void"
	}
	// v9.0.0: клавиша ENTER — явный markup-токен (жёлтый bold), описание из словаря.
	enterHint := "[" + i18n.T.ViewButtonEnter + "](fg:yellow,mod:bold) - " + i18n.T.ViewTrapCreatorRowBannerDesc
	menuRows = append(menuRows, fmt.Sprintf(" %s │ %s │ %s",
		padVisible(i18n.T.ViewTrapCreatorRowBannerLabel, trapEditorLabelW),
		padVisible(fmt.Sprintf("[ %s ]", presetName), trapEditorValueW),
		enterHint))
	menuRows = append(menuRows, fmt.Sprintf(" %s │ %s │ %s",
		padVisible(i18n.T.ViewTrapCreatorRowMaxConnLabel, trapEditorLabelW),
		padVisible(fmt.Sprintf("[ %d ]", TrapEditDraft.MaxConn), trapEditorValueW),
		i18n.T.ViewTrapCreatorRowMaxConnDesc))
	// Кнопки: тег растянут на сумму первых двух колонок — колонка описаний монолитна.
	// v9.0.0: кнопки — секция из двух ячеек (тег | описание): параметра у кнопок нет.
	menuRows = append(menuRows, fmt.Sprintf(" %s │ %s",
		padTagCell(i18n.T.ViewTrapEditorButtonSave, trapEditorTagW),
		i18n.T.ViewTrapEditorRowSave))
	menuRows = append(menuRows, fmt.Sprintf(" %s │ %s",
		padTagCell(i18n.T.ViewButtonCancel, trapEditorTagW),
		i18n.T.ViewTrapEditorRowCancel))

	// -------------------------------------------------------------
	// ШАПКА
	// -------------------------------------------------------------
	// v9.0.0: единая шапка; порт остаётся частью левого текста титула.
	header := newScreenHeader(fmt.Sprintf("%s - %s │ %s %d", HeaderBrandShort, i18n.T.ViewTrapEditorHeaderTitle, i18n.T.ViewTrapEditorLabelPort, TrapEditDraft.Port), 2, 1, 102, 5)
	header.Text = "  " + i18n.T.ViewTrapEditorHeaderDesc1 + "\n  " + i18n.T.ViewTrapEditorHeaderDesc2

	// -------------------------------------------------------------
	// СПИСОК ПАРАМЕТРОВ
	// -------------------------------------------------------------
	paramList := widgets.NewList()
	paramList.Title = " " + i18n.T.ViewTrapEditorListTitle + " "
	paramList.Rows = menuRows
	paramList.SetRect(2, 5, 102, 14)

	// v9.0.0: полоса выделения — единая точка геометрии (helpers.go).
	padListRowsToWidth(paramList)

	paramList.BorderStyle.Fg = ui.ColorCyan
	paramList.SelectedRow = selectedRow
	paramList.SelectedRowStyle = ui.NewStyle(ui.ColorBlack, ui.ColorGreen, ui.ModifierBold)

	// -------------------------------------------------------------
	// КОНТЕКСТНАЯ СПРАВКА
	// -------------------------------------------------------------
	var helpText string
	switch selectedRow {
	case 0:
		helpText = " " + i18n.T.ViewTrapEditorHelpPort
	case 1:
		helpText = " " + i18n.T.ViewTrapCreatorHelpDelayStep
	case 2:
		helpText = " " + i18n.T.ViewTrapCreatorHelpVariance
	case 3:
		helpText = fmt.Sprintf(
			" "+i18n.T.ViewTrapCreatorHelpBanner+"[ %s ](fg:yellow,mod:bold)",
			SSHVersionPresets[TrapEditDraft.BannerIndex])
	case 4:
		helpText = " " + i18n.T.ViewTrapCreatorHelpMaxConn
	case 5:
		helpText = " " + i18n.T.ViewTrapEditorHelpSave
	case 6:
		helpText = " " + i18n.T.ViewTrapEditorHelpCancel
	}
	infoBox := widgets.NewParagraph()
	infoBox.Title = " " + i18n.T.ViewTrapCreatorInfoBoxTitle + " "
	infoBox.Text = helpText
	infoBox.SetRect(2, 14, 102, 24)
	infoBox.BorderStyle.Fg = ui.ColorYellow
	infoBox.TextStyle.Fg = ui.ColorCyan
	infoBox.WrapText = false

	// -------------------------------------------------------------
	// КОНСОЛЬ ВВОДА / НАВИГАЦИИ
	// -------------------------------------------------------------
	sysConsole := widgets.NewParagraph()
	sysConsole.SetRect(2, 24, 102, 30)
	if IsTrapEditMode {
		sysConsole.Title = " " + i18n.T.ViewTrapCreatorEditTitle + " "
		var pName string
		switch selectedRow {
		case 1:
			pName = i18n.T.ViewTrapCreatorRowDelayStepLabel
		case 2:
			pName = i18n.T.ViewTrapCreatorRowVarianceLabel
		case 4:
			pName = i18n.T.ViewTrapCreatorRowMaxConnLabel
		}
		sysConsole.Text = fmt.Sprintf(
			"  %s: %s\n  %s: [ %s_ ](fg:white,mod:bold)",
			i18n.T.ViewTrapCreatorLabelEditNode, pName,
			i18n.T.ViewTrapCreatorLabelEnterNewValue, CurrentTrapInputBuf)
		sysConsole.BorderStyle.Fg = ui.ColorGreen
		sysConsole.TextStyle.Fg = ui.ColorWhite
		sysConsole.WrapText = false
	} else {
		sysConsole.Title = " " + i18n.T.ViewTrapCreatorNavTitle + " "
		if TrapAlertMessage != "" {
			if strings.HasPrefix(TrapAlertMessage, "[УСПЕХ]") {
				sysConsole.Text = "\n " + TrapAlertMessage
				sysConsole.BorderStyle.Fg = ui.ColorGreen
				sysConsole.TextStyle.Fg = ui.ColorWhite
				sysConsole.WrapText = false
			} else {
				sysConsole.Text = "\n  ⚠️ [" + i18n.T.ViewTrapCreatorValidationRejectPrefix + "](fg:red,mod:bold): " + TrapAlertMessage
				sysConsole.BorderStyle.Fg = ui.ColorRed
				sysConsole.TextStyle.Fg = ui.ColorYellow
				sysConsole.WrapText = false
			}
		} else {
			sysConsole.Text = fmt.Sprintf("  [%s]: %s [Up/Down] │ [Enter] — %s.\n  [%s]: %s",
				i18n.T.ViewTrapLabelNav, i18n.T.ViewTrapLabelNavArrows,
				i18n.T.ViewTrapLabelEnterHint, i18n.T.ViewTrapLabelAction, i18n.T.ViewTrapEditorLabelActionHint)
			sysConsole.BorderStyle.Fg = ui.ColorGreen
			sysConsole.TextStyle.Fg = ui.ColorCyan
			sysConsole.WrapText = false
		}
	}

	return []ui.Drawable{header, paramList, infoBox, sysConsole}, len(paramList.Rows)
}

// ValidateAndStoreEditParam проверяет и сохраняет параметр в черновик редактора.
// Порт (индекс 0) не редактируется. Возвращает пустую строку при успехе.
// Переиспользует пакетный валидатор `trapDigitOnlyRegex` из view_trap_creator.go.
func ValidateAndStoreEditParam(index int, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return i18n.T.ViewTrapCreatorErrEmpty
	}
	if !trapDigitOnlyRegex.MatchString(value) {
		return i18n.T.ViewTrapCreatorErrNotDigit
	}
	valInt, _ := strconv.Atoi(value)
	switch index {
	case 1:
		if valInt < 1 || valInt > 300 {
			return i18n.T.ViewTrapCreatorErrDelayRange
		}
		TrapEditDraft.DelayStep = valInt
		if TrapEditDraft.DelayVariance > valInt {
			TrapEditDraft.DelayVariance = valInt
		}
	case 2:
		if valInt < 0 || valInt > TrapEditDraft.DelayStep {
			return fmt.Sprintf(
				i18n.T.ViewTrapCreatorErrVarianceRangeTpl,
				TrapEditDraft.DelayStep)
		}
		TrapEditDraft.DelayVariance = valInt
	case 4:
		if valInt < 1 || valInt > 1000 {
			return i18n.T.ViewTrapCreatorErrMaxConnRange
		}
		TrapEditDraft.MaxConn = valInt
	}
	return ""
}

// SaveTrapEditToInventory записывает отредактированные параметры обратно в инвентарь.
// API-forward v8.0.3 (Аудит №1): TUI больше НЕ пишет в инвентарь напрямую.
// Единственный контур — service.UpdateTrapConfig под транзакцией flock.
// Порт не изменяется. Возвращает ошибку при сбое.
func SaveTrapEditToInventory() error {
	cfg := sys.TarpitConfig{
		SSHVersion:    SSHVersionPresets[TrapEditDraft.BannerIndex],
		DelayStep:     TrapEditDraft.DelayStep,
		DelayVariance: TrapEditDraft.DelayVariance,
		MaxConn:       TrapEditDraft.MaxConn,
	}
	return service.UpdateTrapConfig(TrapEditDraft.Port, cfg)
}
