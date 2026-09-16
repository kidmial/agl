// tui/view_armor_progress.go
package tui

import (
	"agl/i18n"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
)

// ShowArmorProgressWindow выводит расширенную историю выполнения операций Брони v6.2.7
func ShowArmorProgressWindow(actionName string, progressChan chan string) {
	ui.Clear()

	var operationHistory []string
	maxVisibleHistoryLines := 7 // Увеличили до 7 строк для лучшей информативности хроники

	// 1. БРОНЕБОЙНАЯ ГЕОМЕТРИЯ v6.2.7 (Ширина 80 символов, Высота 11 строк)
	// Идеально центрировано на холсте 102х30, дает огромный внутренний запас для длинных имен служб
	progressBox := widgets.NewParagraph()
	progressBox.Title = fmt.Sprintf(" [ %s: %s ] ", i18n.T.ViewArmorProgressTransactionLabel, strings.ToUpper(actionName))
	progressBox.SetRect(12, 9, 92, 20)
	progressBox.BorderStyle.Fg = ui.ColorRed
	progressBox.TextStyle.Fg = ui.ColorWhite
	progressBox.WrapText = false // Отключает автоперенос

	progressBox.Text = "\n  " + i18n.T.ViewArmorProgressInitKernel
	ui.Render(progressBox)

	// 2. ЧИТАЕМ ЖИВОЙ ПОТОК ОПЕРАЦИЙ
	for stepLog := range progressChan {
		// Максимальное количество символов (рун) для лога
		const maxLogRunes = 64
		if utf8.RuneCountInString(stepLog) > maxLogRunes {
			runes := []rune(stepLog)
			if len(runes) > maxLogRunes {
				stepLog = string(runes[:maxLogRunes-3]) + "..."
			}
		}

		timeTag := time.Now().Format("15:04:05")
		formattedLine := fmt.Sprintf(" [%s] %s", timeTag, stepLog)
		operationHistory = append(operationHistory, formattedLine)

		if len(operationHistory) > maxVisibleHistoryLines {
			operationHistory = operationHistory[len(operationHistory)-maxVisibleHistoryLines:]
		}

		progressBox.Text = "\n" + strings.Join(operationHistory, "\n")
		ui.Render(progressBox)
	}

	// =================================================================
	// ФАЗА ЗАВЕРШЕНИЯ С АНИМАЦИЕЙ ПОЯВЛЕНИЯ СТРОК
	// =================================================================
	// Формируем финальный рапорт строго в виде независимых элементов массива
	// Очищаем историю, чтобы начать с пустого виджета
	operationHistory = nil

	// Готовим строки финального отчёта (без префикса времени)
	finalLines := []string{
		"",
		"  [[" + i18n.T.ViewArmorProgressLabelSystemStatus + "]](fg:yellow,mod:bold): " + i18n.T.ViewArmorProgressNetfilterRebuilt,
		"  --------------------------------------------------------------------------",
		"  [[" + i18n.T.ViewArmorProgressLabelSuccess + "]](fg:green,mod:bold) " + i18n.T.ViewArmorProgressArmorTransferred,
		"",
		"",
		"  " + i18n.T.ViewArmorProgressHintExit + " [[ ESC ]](fg:green,mod:bold) " + i18n.T.ViewArmorProgressHintOr + " [[ Q ]](fg:green,mod:bold) " + i18n.T.ViewArmorProgressHintReturn,
	}

	// Меняем цвет рамки на зелёный сразу
	progressBox.BorderStyle.Fg = ui.ColorGreen
	progressBox.WrapText = false // Отключает автоперенос

	// Анимируем появление строк
	for _, line := range finalLines {
		operationHistory = append(operationHistory, line)
		progressBox.Text = "\n" + strings.Join(operationHistory, "\n")
		ui.Render(progressBox)
		time.Sleep(150 * time.Millisecond) // скорость появления текста
	}

	// Ожидание нажатия клавиш
	for confirmEvent := range ui.PollEvents() {
		if confirmEvent.ID == "<Escape>" || confirmEvent.ID == "q" {
			time.Sleep(50 * time.Millisecond)
			break
		}
	}
}
