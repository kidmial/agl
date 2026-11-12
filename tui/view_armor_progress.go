package tui

import "agl/i18n"

// buildArmorFinalReport — финальные строки броневой транзакции.
// Механика окна переехала в helpers.go (ShowTransactionWindow).
func buildArmorFinalReport() []string {
	return []string{
		"",
		"",
		"  [[" + i18n.T.ViewArmorProgressLabelSystemStatus + "]](fg:yellow,mod:bold): " + i18n.T.ViewArmorProgressNetfilterRebuilt,
		"  --------------------------------------------------------------------------",
		"  [[" + i18n.T.ViewArmorProgressLabelSuccess + "]](fg:green,mod:bold) " + i18n.T.ViewArmorProgressArmorTransferred,
		"",
	}
}
