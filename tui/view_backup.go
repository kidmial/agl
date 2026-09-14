// tui/view_backup.go
package tui

import (
	"agl/i18n"
	"agl/internal/service"
	"context"
	"fmt"
	"time"

	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
)

// DrawBackupProgressGrid координирует асинхронную транзакцию бэкапа по сетке 120x30.
// Реализует двухслойный рендеринг прогресс-бара для обеспечения воздушного зазора в 1 символ.
func DrawBackupProgressGrid() {
	ui.Clear()

	// 1а. Внешняя рамка-контейнер прогресса (задает монолитные границы на экране)
	gaugeBorder := widgets.NewParagraph()
	gaugeBorder.Title = i18n.T.ViewBackupGaugeTitle
	gaugeBorder.SetRect(2, 1, 102, 4)
	gaugeBorder.Border = true

	// 1б. Внутренняя полоса прогресса (pGauge) с отступами в 1 символ по бокам
	pGauge := widgets.NewGauge()
	// Сдвигаем левую границу X1 на +1 вправо (3), а правую X2 уменьшаем на -1 влево (101)
	// По вертикали Y ложится ровно внутрь рамки контейнера на 2-й строке терминала
	pGauge.SetRect(3, 2, 101, 3)
	pGauge.Border = false // Отключаем собственную рамку, чтобы не двоилась
	pGauge.BarColor = ui.ColorGreen
	pGauge.LabelStyle = ui.NewStyle(ui.ColorBlack, ui.ColorGreen, ui.ModifierBold)

	// 2. Модернизированный List для логов (Автоперенос отключен нативно)
	consoleList := widgets.NewList()
	consoleList.Title = i18n.T.ViewBackupConsoleTitle
	consoleList.SetRect(2, 4, 102, 23)
	consoleList.BorderStyle.Fg = ui.ColorCyan
	consoleList.TextStyle.Fg = ui.ColorWhite

	// 3. Выделенное нижнее окно подтверждения высотой в 6 символов под 30-ю границу футера
	confirmBox := widgets.NewParagraph()
	confirmBox.Title = i18n.T.ViewBackupConfirmTitle
	confirmBox.Text = fmt.Sprintf("\n  [[ %s ]](fg:green,mod:bold) %s\n  %s [ ESC ](fg:yellow,mod:bold) %s [ Q ](fg:yellow,mod:bold) %s",
		i18n.T.ViewBackupLabelSuccess,
		i18n.T.ViewBackupConfirmSuccessText,
		i18n.T.ViewBackupConfirmPressKey,
		i18n.T.ViewBackupConfirmOr,
		i18n.T.ViewBackupConfirmReturnMenu)
	confirmBox.SetRect(2, 23, 102, 29)
	confirmBox.BorderStyle.Fg = ui.ColorYellow
	confirmBox.TextStyle.Fg = ui.ColorCyan

	// Первичный вывод каркаса на холст
	ui.Render(gaugeBorder, pGauge, consoleList)

	// Слайс для накопления всех строк лога в ОЗУ
	var consoleLogRows []string
	maxVisibleLines := 17

	// Запускаем асинхронную горутину упаковки двух контуров через ядро
	progressChan, _ := service.RunSystemBackup(context.Background())

	// Читаем живой поток событий из RAM-канала до полного его закрытия бэкендом
	for stat := range progressChan {
		pGauge.Percent = stat.Percent
		if stat.CurrentFile != "" {
			// Изменяем заголовок у внешнего контейнера, так как рамка теперь на нем
			gaugeBorder.Title = fmt.Sprintf(i18n.T.ViewBackupGaugeObjectTitleTpl, stat.CurrentFile)
		}

		if stat.LogLine != "" {
			timeTag := time.Now().Format("15:04:05")
			formattedLine := fmt.Sprintf(" [%s] %s", timeTag, stat.LogLine)
			consoleLogRows = append(consoleLogRows, formattedLine)

			startIdx := 0
			if len(consoleLogRows) > maxVisibleLines {
				startIdx = len(consoleLogRows) - maxVisibleLines
			}
			consoleList.Rows = consoleLogRows[startIdx:]
		}

		ui.Render(gaugeBorder, pGauge, consoleList)

	}

	// Бэкап успешно завершен — подсвечиваем контур полезного действия зеленым
	ui.Clear()
	ui.Render(gaugeBorder, pGauge, consoleList, confirmBox)

	// Указатель текущего виртуального скролла в памяти
	scrollOffset := 0
	// -----------------------------------------------------------------
	// ИНТЕРФЕЙСНАЯ ОПЦИЯ: Сверхбыстрый скроллинг массива List по стрелкам Up/Down
	// -----------------------------------------------------------------
	// ШАГ 3: ЗАХВАТ КЛАВИШ И СКРОЛЛ. Выходим строго по ESCAPE/Q
	// -----------------------------------------------------------------
	for confirmEvent := range ui.PollEvents() {
		if confirmEvent.ID == "<Up>" {
			if len(consoleLogRows) > maxVisibleLines && scrollOffset < len(consoleLogRows)-maxVisibleLines {
				scrollOffset++
				// Точечно пересчитываем строки и рендерим сдвиг логов
				startIdx := len(consoleLogRows) - maxVisibleLines - scrollOffset
				consoleList.Rows = consoleLogRows[startIdx : startIdx+maxVisibleLines]
				ui.Render(consoleList)
			}
		} else if confirmEvent.ID == "<Down>" {
			if scrollOffset > 0 {
				scrollOffset--
				// Точечно пересчитываем строки и рендерим сдвиг логов
				startIdx := len(consoleLogRows) - maxVisibleLines - scrollOffset
				consoleList.Rows = consoleLogRows[startIdx : startIdx+maxVisibleLines]
				ui.Render(consoleList)
			}
		} else if confirmEvent.ID == "<Escape>" || confirmEvent.ID == "q" {
			// ФИКС v6.0.0: Полный отказ от ENTER для исключения прострелов клавиши.
			// Выходим строго по ESC или Q. Даем рантайму 50 мс на стабилизацию дескрипторов.
			time.Sleep(50 * time.Millisecond)
			break // Безопасный возврат во внешний роутер!
		}
	}
}
