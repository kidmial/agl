// tui/view_backup.go
package tui

import (
	"agl/i18n"
	"agl/internal/service"
	"context"
	"fmt"
	"regexp"
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
	// v9.0.0: стиль контейнера транзакции — cyan, как у шапок экранов.
	gaugeBorder.BorderStyle.Fg = ui.ColorCyan
	gaugeBorder.TitleStyle.Fg = ui.ColorCyan
	gaugeBorder.TitleStyle.Modifier = ui.ModifierBold

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

	// 3а. Нижнее окно ПРОГРЕССА (показывается во время архивации)
	progressBox := widgets.NewParagraph()
	progressBox.Title = i18n.T.ViewBackupProgressTitle
	progressBox.Text = fmt.Sprintf("\n  %s", i18n.T.ViewBackupProgressBody)
	progressBox.SetRect(2, 23, 102, 29)
	progressBox.BorderStyle.Fg = ui.ColorCyan
	progressBox.TextStyle.Fg = ui.ColorWhite
	progressBox.WrapText = false

	// 3б. Выделенное нижнее окно подтверждения высотой в 6 символов под 30-ю границу футера
	confirmBox := widgets.NewParagraph()
	confirmBox.Title = i18n.T.ViewBackupConfirmTitle
	confirmBox.Text = fmt.Sprintf("\n  [[ %s ]](fg:green,mod:bold) %s\n  %s [ ESC ](fg:yellow,mod:bold) %s",
		i18n.T.ViewBackupLabelSuccess,
		i18n.T.ViewBackupConfirmSuccessText,
		i18n.T.ViewBackupConfirmPressKey,
		i18n.T.ViewBackupConfirmReturnMenu)
	confirmBox.SetRect(2, 23, 102, 29)
	confirmBox.BorderStyle.Fg = ui.ColorYellow
	confirmBox.TextStyle.Fg = ui.ColorCyan

	// Первичный вывод каркаса на холст (включая progressBox)
	ui.Render(gaugeBorder, pGauge, consoleList, progressBox)

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
			// v9.0.0: КОСМЕТИКА — раскраска тегов операций для glanceable-чтения.
			colorizedLog := colorizeBackupLog(stat.LogLine)
			formattedLine := fmt.Sprintf(" [%s] %s", timeTag, colorizedLog)
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

	// v9.0.0: гасим клавиши, накопленные во время архивации: подтверждение
	// читается свежими глазами, устаревший Escape не проскакивает экран.
	drained := false
	for !drained {
		select {
		case <-ui.PollEvents():
		default:
			drained = true
		}
	}
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
		} else if confirmEvent.ID == "<Escape>" {
			// ФИКС v6.0.0: Только ESC для выхода. ENTER/Q отклонены:
			// ENTER вызывает повторный запуск архивации, Q — конфликтует с командой
			// полного выхода из комплекса в роутере (семантический clash).
			// Даем рантайму 50 мс на стабилизацию дескрипторов.
			time.Sleep(50 * time.Millisecond)
			break // Безопасный возврат во внешний роутер!
		}
	}
}

// ============================================================================
// КОСМЕТИКА: РАСКРАСКА ТЕГОВ ОПЕРАЦИЙ В ЛОГАХ БЭКАПА
// ============================================================================

// backupTagRe матчит квадратные теги операций в логах бэкапа.
// PURPOSE: визуальная грамматика процесса — каждый тип операции свой цвет.
// Контракт termui: двойные скобки `[[ TEXT ]](fg:color,mod:bold)` для markup.
var backupTagRe = regexp.MustCompile(`\[(DPKG|КОНТРОЛЬ|CONTROL|ПРОПУСК|SKIP|LOG-TAR|TAR|CRITICAL|ERROR|ОШИБКА|КРИТИЧНО|FAIL|WARNING|ВНИМАНИЕ|УСПЕХ|SUCCESS|INFO)\]`)

// colorizeBackupLog оборачивает теги операций в markup-модификаторы termui.
// ЦВЕТОВАЯ ГРАММАТИКА (единый визуальный язык с инсталлятором):
//   - ЖЁЛТЫЙ (fg:yellow): подготовительные/пропускные операции — DPKG, ПРОПУСК, SKIP, WARNING, ВНИМАНИЕ
//   - ГОЛУБОЙ (fg:cyan): контроли и логи — КОНТРОЛЬ, CONTROL, LOG-TAR, INFO
//   - ЗЕЛЁНЫЙ (fg:green): успешные действия — TAR, УСПЕХ, SUCCESS
//   - КРАСНЫЙ (fg:red): сбои — CRITICAL, КРИТИЧНО, ERROR, ОШИБКА, FAIL
//
// CONTRACT: строки без тегов возвращаются без изменений. Теги с модификатором
// `mod:bold` для glanceable-чтения потока.
func colorizeBackupLog(line string) string {
	return backupTagRe.ReplaceAllStringFunc(line, func(tag string) string {
		// Обнажаем имя тега (снимаем внешние скобки) для матчинга
		name := tag[1 : len(tag)-1]
		switch name {
		case "DPKG", "ПРОПУСК", "SKIP", "WARNING", "ВНИМАНИЕ":
			return fmt.Sprintf("[[ %s ]](fg:yellow,mod:bold)", name)
		case "КОНТРОЛЬ", "CONTROL", "LOG-TAR", "INFO":
			return fmt.Sprintf("[[ %s ]](fg:cyan,mod:bold)", name)
		case "TAR", "УСПЕХ", "SUCCESS":
			return fmt.Sprintf("[[ %s ]](fg:green,mod:bold)", name)
		case "CRITICAL", "КРИТИЧНО", "ERROR", "ОШИБКА", "FAIL":
			return fmt.Sprintf("[[ %s ]](fg:red,mod:bold)", name)
		default:
			return tag // неизвестный тег — оставляем как есть
		}
	})
}
