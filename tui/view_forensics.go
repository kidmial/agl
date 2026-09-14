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
	header.Text = fmt.Sprintf("  [%s]: [ %s ]%s\n  [%s]: [ /var/log/amberguard/ ]%s",
		i18n.T.ViewForensicsLabelCollectMode, i18n.T.ViewForensicsLabelAutoParallel, ColorYellow,
		i18n.T.ViewForensicsLabelSiemRegistry, ColorYellow)
	header.SetRect(2, 1, 102, 5)
	header.BorderStyle.Fg = ui.ColorCyan
	header.TitleStyle.Fg = ui.ColorYellow

	menuList := widgets.NewList()
	menuList.Title = i18n.T.ViewForensicsListTitle
	menuList.Rows = []string{
		fmt.Sprintf("  THREAT_MAP   │ %s", i18n.T.ViewForensicsRowThreatMap),
		fmt.Sprintf("  SSH_LOG      │ %s", i18n.T.ViewForensicsRowSshLog),
		fmt.Sprintf("  WEB_LOG      │ %s", i18n.T.ViewForensicsRowWebLog),
		fmt.Sprintf("  BOTNET       │ %s", i18n.T.ViewForensicsRowBotnet),
		fmt.Sprintf("  IP_FORENSICS │ %s", i18n.T.ViewForensicsRowIpForensics),
		fmt.Sprintf("  BACK         │ %s", i18n.T.ViewForensicsRowBack),
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
