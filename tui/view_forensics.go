// tui/view_forensics.go
package tui

import (
	"fmt"

	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
)

// DrawForensicsMenuGrid отрисовывает Суб-панель Диспетчера Форензики (Раздел FORENSIC)
func DrawForensicsMenuGrid(selectedRow int, hintText string) ([]ui.Drawable, int) {
	header := widgets.NewParagraph()
	header.Title = " Подсистема Криминалистического Анализа и Форензики "
	header.Text = fmt.Sprintf("  [РЕЖИМ СБОРА МЕТРИК]: [ АВТОМАТИЧЕСКИЙ (RAM-Параллельный) ]%s\n  [ЦЕНТРАЛЬНЫЙ SIEM-РЕЕСТР]: [ /var/log/amberguard/ ]%s", ColorYellow, ColorYellow) // РЕБРЕНДИНГ
	header.SetRect(2, 1, 102, 5)
	header.BorderStyle.Fg = ui.ColorCyan
	header.TitleStyle.Fg = ui.ColorYellow

	menuList := widgets.NewList()
	menuList.Title = " Выберите криминалистический модуль для проведения расследования: "
	menuList.Rows = []string{
		"  THREAT_MAP   │ Сводная интерактивная Карта Угроз экосистемы сервера",
		"  SSH_LOG      │ Журнал распределенных брутфорс-атак на OpenSSH (auth.log)",
		"  WEB_LOG      │ Журнал сигнатурного сканирования веб-панелей (access.log)",
		"  BOTNET       │ Хроника истощения и поведенческого анализа ботнет-сканеров",
		"  IP_FORENSICS │ Картотека и персональное криминалистическое досье нарушителя",
		"  BACK         │ Назад в главное меню сервера",
	}
	menuList.SetRect(2, 5, 102, 15)
	menuList.BorderStyle.Fg = ui.ColorCyan
	menuList.TitleStyle.Fg = ui.ColorYellow
	menuList.SelectedRow = selectedRow
	menuList.SelectedRowStyle = ui.NewStyle(ui.ColorBlack, ui.ColorGreen, ui.ModifierBold)

	infoBox := widgets.NewParagraph()
	infoBox.Title = " Регламент проведения криминалистического расследования инцидентов "
	infoBox.Text = hintText
	infoBox.SetRect(2, 15, 102, 20)
	infoBox.BorderStyle.Fg = ui.ColorYellow
	infoBox.TextStyle.Fg = ui.ColorCyan

	return []ui.Drawable{header, menuList, infoBox}, len(menuList.Rows)
}
