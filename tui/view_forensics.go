// tui/view_forensics.go
package tui

import (
	"agl/i18n"
	"fmt"

	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
)

// DrawForensicsMenuGrid отрисовывает Суб-панель Диспетчера Форензики (Раздел FORENSIC)
func DrawForensicsMenuGrid(selectedRow int, hintText string) ([]ui.Drawable, int) {
	header := widgets.NewParagraph()
	header.Title = i18n.T.ViewForensicsHeaderTitle
	header.Text = fmt.Sprintf(i18n.T.ViewForensicsHeaderTextTpl, ColorYellow, ColorYellow)
	header.SetRect(2, 1, 102, 5)
	header.BorderStyle.Fg = ui.ColorCyan
	header.TitleStyle.Fg = ui.ColorYellow

	menuList := widgets.NewList()
	menuList.Title = i18n.T.ViewForensicsListTitle
	menuList.Rows = []string{
		i18n.T.ViewForensicsRowThreatMap,
		i18n.T.ViewForensicsRowSshLog,
		i18n.T.ViewForensicsRowWebLog,
		i18n.T.ViewForensicsRowBotnet,
		i18n.T.ViewForensicsRowIpForensics,
		i18n.T.ViewForensicsRowBack,
	}
	menuList.SetRect(2, 5, 102, 15)
	menuList.BorderStyle.Fg = ui.ColorCyan
	menuList.TitleStyle.Fg = ui.ColorYellow
	menuList.SelectedRow = selectedRow
	menuList.SelectedRowStyle = ui.NewStyle(ui.ColorBlack, ui.ColorGreen, ui.ModifierBold)

	infoBox := widgets.NewParagraph()
	infoBox.Title = i18n.T.ViewForensicsInfoBoxTitle
	infoBox.Text = hintText
	infoBox.SetRect(2, 15, 102, 20)
	infoBox.BorderStyle.Fg = ui.ColorYellow
	infoBox.TextStyle.Fg = ui.ColorCyan

	return []ui.Drawable{header, menuList, infoBox}, len(menuList.Rows)
}
