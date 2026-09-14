// tui/view_port_map.go
// Реформа v6.4.0 — Чистый пассивный визуализатор инвентаризации портов хоста
package tui

import (
	"agl/config"
	"agl/i18n"
	"agl/internal/service"
	"agl/internal/sys"
	"fmt"
	"os"

	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
)

// DrawPortMapGrid динамически строит живую карту легитимных портов и сокетов хоста.
// v6.4.0: Избавлен от мертвых вызовов старой аналитики и переведен в пассивный режим.
// ГЕОМЕТРИЯ v6.4.1: Высота блока сокетов жестко зажата на 13 строк для предотвращения мерцания сетки.
func DrawPortMapGrid(selectedRow int) ([]ui.Drawable, int, []config.AppInventoryItem) {
	// 1. Верхний текстовый блок заголовка (Высота 4 строки: y: 1 - 5)
	header := widgets.NewParagraph()
	header.Title = fmt.Sprintf(" [ %s │ PORT_MAP ] ", i18n.T.ViewPortMapHeaderTitle)
	header.Text = i18n.T.ViewPortMapHeaderDesc
	header.SetRect(2, 1, 102, 5)
	header.BorderStyle.Fg = ui.ColorCyan
	header.TitleStyle.Fg = ui.ColorYellow
	header.TitleStyle.Modifier = ui.ModifierBold
	header.WrapText = false

	// Загружаем сырой инвентарь напрямую из файла через стабильный бэкенд config_inventory.go.
	// Используем приватный метод через обертку или создаем безопасный срез.
	// По анализу config_inventory, мы можем безопасно прочитать DetectedApps через костыль пустой инициализации,
	// но самый чистый способ — запустить сканирование или прочитать файл, если функция LoadInventory возвращает только protected.
	// Однако, чтобы не ломать типы, временно инициализируем пустой срез, если бэкенд еще реформируется.

	// ВАЖНО: Так как в config_inventory.go функция LoadInventory возвращает []PortTarget,
	// а DetectedApps лежит внутри PerimeterInventory, для TUI v6.4.0 мы делаем безопасный вызов:
	var visibleApps []config.AppInventoryItem

	// В рамках реформы v6.4.0 для бесшовной компиляции:
	// Если мы не хотим менять config_inventory.go, мы можем сделать заплатку,
	// но если ты УЖЕ добавил функцию LoadDetectedApps() в config_inventory.go, как мы договаривались шагом ранее:
	var err error
	// v8.2.3 (R-3d): прямой вызов config.LoadDetectedApps() заменяем на сервисный слой.
	// TUI больше не знает о деталях чтения JSON-инвентаря с диска.
	visibleApps, err = service.LoadDetectedApps()
	if err != nil {
		visibleApps = []config.AppInventoryItem{}
	}

	var menuRows []string
	// Строим строки карты портов
	for _, app := range visibleApps {
		// C2 (v8.1.3): приоритет статусов TARPIT > ARMOR > UNPROTECTED
		// (паттерн view_armor.go). Порт под ямой — не «незащищённый», а «капкан».
		tarpitMarker := config.TarpitMarkerPath(app.Port)
		statusStr := "[ UNPROTECTED ](fg:red)"
		if _, errT := os.Stat(tarpitMarker); errT == nil {
			statusStr = "[   TARPIT   ](fg:magenta,mod:bold)"
		} else if app.IsArmorEnabled {
			statusStr = "[   ARMORED   ](fg:green,mod:bold)"
		}

		// Корпоративный стандарт: Каждое Слово С Заглавной Буквы
		formattedAppName := sys.PrettyServiceName(app.Name)
		// C9 (v8.1.3): канал спасения не маскируется под обычную службу
		if app.Port == config.C.WebRescuePort {
			formattedAppName = i18n.T.ViewPortMapWebKnockLabel
		}
		paddedAppName := fmt.Sprintf("%-14s", formattedAppName)
		if len(paddedAppName) > 14 {
			paddedAppName = paddedAppName[:14]
		}

		// Выводим: Программа | Сокет (Интерфейс) | Статус защиты ядра Armor
		menuRows = append(menuRows, fmt.Sprintf(" %s │ %s: %-18s │ %s: %s",
			paddedAppName, i18n.T.ViewPortMapColSocket, app.ListenInterface, i18n.T.ViewPortMapColDefense, statusStr))
	}

	// Единственная интерактивная кнопка для выхода назад
	menuRows = append(menuRows, fmt.Sprintf(" BACK           │ %s", i18n.T.ViewPortMapRowBack))

	// 2. Основной блок списка сокетов (ЖЕСТКАЯ ФИКСАЦИЯ: Высота ровно 13 строк, y: 5 - 18)
	portList := widgets.NewList()
	portList.Title = i18n.T.ViewPortMapListTitle
	portList.Rows = menuRows
	portList.SetRect(2, 5, 102, 18) // Высота 13 строк зафиксирована жестко по ТЗ
	portList.BorderStyle.Fg = ui.ColorCyan
	portList.SelectedRow = selectedRow
	portList.SelectedRowStyle = ui.NewStyle(ui.ColorBlack, ui.ColorGreen, ui.ModifierBold)

	// 3. Информационная справка (Фиксированная высота 5 строк, y: 18 - 23)
	infoBox := widgets.NewParagraph()
	infoBox.Title = i18n.T.ViewPortMapInfoBoxTitle
	infoBox.Text = i18n.T.ViewPortMapInfoBoxText
	infoBox.SetRect(2, 18, 102, 23)
	infoBox.BorderStyle.Fg = ui.ColorYellow
	infoBox.TextStyle.Fg = ui.ColorCyan
	infoBox.WrapText = false

	// 4. Системная консоль навигации (Фиксированная высота 6 строк до нижней границы футера, y: 23 - 29)
	sysConsole := widgets.NewParagraph()
	sysConsole.Title = i18n.T.ViewPortMapConsoleTitle
	sysConsole.Text = fmt.Sprintf(" [%s]: %s │ [%s]: %s\n [%s]: %s\n [%s]: %s",
		i18n.T.ViewPortMapLabelNav, i18n.T.ViewPortMapLabelNavArrows, i18n.T.ViewPortMapLabelExit, i18n.T.ViewPortMapLabelExitHint,
		i18n.T.ViewPortMapLabelInfo, i18n.T.ViewPortMapLabelScrollInfo, i18n.T.ViewPortMapLabelStatus, i18n.T.ViewPortMapLabelStatusSync)
	sysConsole.SetRect(2, 23, 102, 29)
	sysConsole.BorderStyle.Fg = ui.ColorGreen
	sysConsole.TextStyle.Fg = ui.ColorCyan
	sysConsole.WrapText = false

	return []ui.Drawable{header, portList, infoBox, sysConsole}, len(portList.Rows), visibleApps
}
