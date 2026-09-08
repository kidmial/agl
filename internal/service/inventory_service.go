// internal/service/inventory_service.go
package service

import (
	"agl/config"
	"agl/i18n"
	"agl/internal/sys"
	"context"
	"fmt"
)

// ============================================================================
// СЕРВИСНЫЙ СЛОЙ: ИНВЕНТАРИЗАЦИЯ ПЕРИМЕТРА (INVENTORY)
// ============================================================================
// PURPOSE: Единая точка входа для TUI к функциям сканирования периметра.
// CONTRACT: Инкапсулирует вызовы config.RunPerimeterInventoryScan, LoadInventory.
//           Возвращает явные error, логирует события через sys.LogAction.
// SIDE-EFFECTS: Запускает сканирование процессов и сокетов (ss, /proc).
// ============================================================================

// RunPerimeterInventoryScan запускает полное сканирование периметра.
func RunPerimeterInventoryScan(ctx context.Context) error {
	err := config.RunPerimeterInventoryScan()
	if err != nil {
		sys.LogAction("SERVICE_INVENTORY", "SCAN_FAIL", "ERROR",
			fmt.Sprintf("event=inventory.scan_failed err=%v", err))
		return fmt.Errorf(i18n.T.InventoryScanFail, err)
	}
	sys.LogAction("SERVICE_INVENTORY", "SCAN_OK", "SUCCESS",
		"event=inventory.scan_completed")
	return nil
}

// LoadInventory возвращает список защищаемых портов из инвентаря.
func LoadInventory() ([]config.PortTarget, error) {
	ports, err := config.LoadInventory()
	if err != nil {
		return nil, fmt.Errorf(i18n.T.InventoryLoadPortsFail, err)
	}
	return ports, nil
}

// LoadDetectedApps возвращает список авто-обнаруженных приложений.
func LoadDetectedApps() ([]config.AppInventoryItem, error) {
	apps, err := config.LoadDetectedApps()
	if err != nil {
		return nil, fmt.Errorf(i18n.T.InventoryLoadAppsFail, err)
	}
	return apps, nil
}
