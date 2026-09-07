// tui/view_tarpit_live_box.go
package tui

import (
	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"

	"agl/internal/forensics"
)

// Global Shared Instance для управления табличной матрицей инцидентов.
// Перенос указателя в данный пакет исключает загрязнение глобального пространства роутера.
var LiveTable *widgets.List

// DrawTarpitLiveBoxGrid вызывается первично при интерактивном входе администратора в окно.
// Нативно инициализирует List-компонент termui и осуществляет жесткий сброс курсора на нулевую строку.
func DrawTarpitLiveBoxGrid() []ui.Drawable {
	header := widgets.NewParagraph()
	header.Text = " Хроника Истощения и Анализа Сканеров (TARPIT_LIVE_BOX)"
	header.SetRect(2, 1, 102, 4)
	header.BorderStyle.Fg = ui.ColorCyan
	header.TextStyle.Fg = ui.ColorYellow

	// ПАТТЕРН СИНГЛТОН: Инициализируем List-виджет строго один раз в памяти ОЗУ.
	// Это гарантирует стабильность ссылки для фонового секундного тикера рендерера.
	if LiveTable == nil {
		LiveTable = widgets.NewList()
		LiveTable.SetRect(2, 4, 102, 26)
		LiveTable.BorderStyle.Fg = ui.ColorCyan
		LiveTable.SelectedRowStyle = ui.NewStyle(ui.ColorBlack, ui.ColorGreen, ui.ModifierBold)
	}
	
	// Синхронизируем строки с массивом сырых SIEM-логов Судьи.
	LiveTable.Title = " Хроника инцидентов (Свежие атаки сверху) "
	LiveTable.Rows = forensics.GetTarpitLiveRows()
	
	// СБРОС КУРСОРА СТРОГО ОДИН РАЗ — ПРИ РУЧНОМ ВХОДЕ НА ЭКРАН!
	// Предотвращает залипание каретки выделения в хвосте списка при повторных сессиях.
	LiveTable.SelectedRow = 0 

	hint := widgets.NewParagraph()
	hint.Text = " [ Нажмите ESC или Q для закрытия LIVE_BOX и возврата ] "
	hint.SetRect(2, 26, 102, 29)
	hint.BorderStyle.Fg = ui.ColorYellow
	hint.TextStyle.Fg = ui.ColorCyan

	return []ui.Drawable{header, LiveTable, hint}
}

// UpdateTarpitLiveBoxData производит фоновое обновление контента без деструктивного сброса скролла.
// Метод непрерывно вызывается независимым секундным тикером центрального роутера.
func UpdateTarpitLiveBoxData() {
	if LiveTable != nil {
		// Подменяем исключительно массив строк, текущая позиция SelectedRow остается нетронутой в ОЗУ.
		LiveTable.Rows = forensics.GetTarpitLiveRows()
	}
}
