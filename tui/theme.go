// tui/theme.go
package tui

// Единый суверенный реестр стилей и раскраски интерфейса Amber Guard Lab
const (
	ColorGreen  = "(fg:green,mod:bold)"
	ColorRed    = "(fg:red,mod:bold)"
	ColorYellow = "(fg:yellow,mod:bold)"
	ColorCyan   = "(fg:cyan)"
	ColorWhite  = "(fg:white,mod:bold)" // Добавлен сочный белый цвет для выделения важных данных
	ColorGray   = "(fg:gray)"           // Скрытый приглушенный серый для второстепенных текстов
	ColorReset  = "[-]"
)
