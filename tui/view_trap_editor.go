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
//	5 — SAVE_AND_APPLY_CHANGES
//	6 — CANCEL_AND_DISCARD
//
// ============================================================================
package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

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
		return fmt.Errorf("не удалось загрузить инвентарь: %w", err)
	}
	trapID := sys.GenerateTarpitID(port)
	trap, exists := inv.Traps[trapID]
	if !exists {
		return fmt.Errorf("ловушка %s не найдена в инвентаре", trapID)
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

// DrawTrapEditorGrid отрисовывает экран редактирования ловушки.
// Возвращает срез виджетов и общее количество строк.
func DrawTrapEditorGrid(selectedRow int) ([]ui.Drawable, int) {
	var menuRows []string

	// Порт — только чтение
	menuRows = append(menuRows, fmt.Sprintf(
		" Сетевой порт ловушки    (Port)       │ [ %d ] │ ТОЛЬКО ЧТЕНИЕ (изменение запрещено)",
		TrapEditDraft.Port))
	menuRows = append(menuRows, fmt.Sprintf(
		" Базовое время удержания (DelayStep)  │ [ %d сек ] │ Время задержки одного шага цикла",
		TrapEditDraft.DelayStep))
	menuRows = append(menuRows, fmt.Sprintf(
		" Вариативность разброса  (Variance)   │ [ %d сек ] │ Случайный разброс сна (Delay ± Variance)",
		TrapEditDraft.DelayVariance))

	// Короткое имя пресета баннера
	presetName := "Ubuntu Standard"
	switch TrapEditDraft.BannerIndex {
	case 1:
		presetName = "Legacy Enterprise"
	case 2:
		presetName = "Embedded IoT"
	case 3:
		presetName = "Stealth Void"
	}
	menuRows = append(menuRows, fmt.Sprintf(
		" Имитируемый профиль     (SSHVersion) │ [ %s ] │ Нажми ENTER для смены шаблона службы",
		presetName))
	menuRows = append(menuRows, fmt.Sprintf(
		" Лимит сессий в ОЗУ      (MaxConn)    │ [ %d ] │ Защита RAM от перегрузки ботнетом",
		TrapEditDraft.MaxConn))
	menuRows = append(menuRows,
		" [ GENERATE AND ACTIVATE ]            │ Сохранить изменения и вернуться к карточке")
	menuRows = append(menuRows,
		" [ CANCEL ]                           │ Отменить изменения и вернуться к карточке")

	// -------------------------------------------------------------
	// ШАПКА
	// -------------------------------------------------------------
	header := widgets.NewParagraph()
	header.Title = fmt.Sprintf(" [ Редактор Ловушки │ Порт %d ] ", TrapEditDraft.Port)
	timeStr := time.Now().Format("15:04:05")
	dateStr := time.Now().Format("2006-01-02")
	header.Text = fmt.Sprintf(
		"  Дата: [%s](fg:yellow,mod:bold)  │  Время: [%s](fg:yellow,mod:bold)\n"+
			"  Редактирование параметров существующей ловушки. Порт зафиксирован.\n"+
			"  Изменения применяются через атомарную запись в инвентарь (POSIX).",
		dateStr, timeStr)
	header.SetRect(2, 1, 102, 5)
	header.BorderStyle.Fg = ui.ColorCyan
	header.TitleStyle.Fg = ui.ColorYellow
	header.TitleStyle.Modifier = ui.ModifierBold
	header.WrapText = false

	// -------------------------------------------------------------
	// СПИСОК ПАРАМЕТРОВ
	// -------------------------------------------------------------
	paramList := widgets.NewList()
	paramList.Title = " Параметры ловушки (порт — только чтение): "
	paramList.Rows = menuRows
	paramList.SetRect(2, 5, 102, 14)
	paramList.BorderStyle.Fg = ui.ColorCyan
	paramList.SelectedRow = selectedRow
	paramList.SelectedRowStyle = ui.NewStyle(ui.ColorBlack, ui.ColorGreen, ui.ModifierBold)

	// -------------------------------------------------------------
	// КОНТЕКСТНАЯ СПРАВКА
	// -------------------------------------------------------------
	var helpText string
	switch selectedRow {
	case 0:
		helpText = " Порт ловушки зафиксирован и не подлежит изменению.\n" +
			" Изменение порта потребовало бы пересоздания идентификатора, маркера и UFW-правила.\n" +
			" Для смены порта удалите ловушку (DELETE) и создайте новую."
	case 1:
		helpText = " Базовый интервал удержания одного TCP-соединения бота.\n" +
			" Рекомендуемый диапазон: от 5 до 60 секунд."
	case 2:
		helpText = " Вариативность разброса джиттера.\n" +
			" Итоговый сон: DelayStep ± DelayVariance.\n" +
			" Значение не может превышать базовый сон DelayStep."
	case 3:
		helpText = fmt.Sprintf(
			" Текстовый баннер, отдаваемый боту после Handshake.\n"+
				" Текущая строка: [ %s ](fg:cyan,mod:bold)",
			SSHVersionPresets[TrapEditDraft.BannerIndex])
	case 4:
		helpText = " Максимальное количество одновременных горутин-потоков.\n" +
			" Допустимый диапазон: от 1 до 1000."
	case 5:
		helpText = " Сохранить изменения в инвентарь и вернуться к карточке ловушки.\n" +
			" Если ловушка запущена, изменения применятся после перезапуска (STOP → START)."
	case 6:
		helpText = " Отменить все изменения и вернуться к карточке ловушки без записи."
	}
	infoBox := widgets.NewParagraph()
	infoBox.Title = " Контекстная справка параметра "
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
		sysConsole.Title = " [ РЕЖИМ ИЗМЕНЕНИЯ ПАРАМЕТРА ЛОВУШКИ ] "
		var pName string
		switch selectedRow {
		case 1:
			pName = "DelayStep (Базовое время сна)"
		case 2:
			pName = "DelayVariance (Разброс вариативности)"
		case 4:
			pName = "MaxConn (Лимит соединений)"
		}
		sysConsole.Text = fmt.Sprintf(
			"  Редактируемый узел: %s\n  Введите новое числовое значение: [ %s_ ](fg:white,mod:bold)",
			pName, CurrentTrapInputBuf)
		sysConsole.BorderStyle.Fg = ui.ColorGreen
		sysConsole.TextStyle.Fg = ui.ColorWhite
		sysConsole.WrapText = false
	} else {
		sysConsole.Title = " Консоль редактирования ловушки "
		if TrapAlertMessage != "" {
			if strings.HasPrefix(TrapAlertMessage, "[УСПЕХ]") {
				sysConsole.Text = "\n " + TrapAlertMessage
				sysConsole.BorderStyle.Fg = ui.ColorGreen
				sysConsole.TextStyle.Fg = ui.ColorWhite
			} else {
				sysConsole.Text = "\n  ⚠️ [ОТКАЗ ВАЛИДАЦИИ](fg:red,mod:bold): " + TrapAlertMessage
				sysConsole.BorderStyle.Fg = ui.ColorRed
				sysConsole.TextStyle.Fg = ui.ColorYellow
			}
		} else {
			sysConsole.Text = "  [Навигация]: Стрелки [Up/Down] │ [Enter] — редактировать / переключить.\n" +
				"  [Действие]: Выберите SAVE для сохранения или CANCEL для отмены."
			sysConsole.BorderStyle.Fg = ui.ColorGreen
			sysConsole.TextStyle.Fg = ui.ColorCyan
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
		return "Вводимое значение не может быть пустым!"
	}
	if !trapDigitOnlyRegex.MatchString(value) {
		return "Ошибка валидации типа: Допускаются только целые положительные числа!"
	}
	valInt, _ := strconv.Atoi(value)
	switch index {
	case 1:
		if valInt < 1 || valInt > 300 {
			return "Критическое значение задержки! Допускается диапазон от 1 до 300 секунд."
		}
		TrapEditDraft.DelayStep = valInt
		if TrapEditDraft.DelayVariance > valInt {
			TrapEditDraft.DelayVariance = valInt
		}
	case 2:
		if valInt < 0 || valInt > TrapEditDraft.DelayStep {
			return fmt.Sprintf(
				"DelayVariance не может быть отрицательным или превышать базовый сон DelayStep (%d сек)!",
				TrapEditDraft.DelayStep)
		}
		TrapEditDraft.DelayVariance = valInt
	case 4:
		if valInt < 1 || valInt > 1000 {
			return "Недопустимый лимит сессий! Установите границы от 1 до 1000 горутин."
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
