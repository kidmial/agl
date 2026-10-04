package service

import (
	"agl/config"
	"agl/internal/armor"
)

// SyncRescueChannelState приводит факт канала спасения к декларации.
// PURPOSE: Единственная точка входа TUI в активацию/деактивацию канала.
// CONTRACT: Активация — только при совпадении 3 переменных и отсутствии маркера.
//
//	Деактивация — при нарушении любого условия и живом маркере.
//
// SIDE-EFFECTS: UFW-правила и маркеры меняются внутри пакета armor.
func SyncRescueChannelState() error {
	// v8.4.7: жизненный цикл юнита приводим к декларации ДО сверки маркеров.
	armor.SyncWatcherUnit()
	castling := armor.MarkerExists(config.MarkerSshMoved)
	declared := config.C.WebRescueEnabled && config.C.WebServerEnabled
	marker := armor.MarkerExists(config.MarkerRescueActive)
	switch {
	case castling && declared && !marker:
		armor.ActivateRescueChannel()
	case (!declared || !castling) && marker:
		armor.DeactivateRescueChannel()
	}
	return nil
}
