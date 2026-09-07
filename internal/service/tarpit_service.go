// internal/service/tarpit_service.go
package service

import (
	"agl/internal/sys"
	"agl/internal/tarpitctl"
	"context"
	"fmt"
)

// ============================================================================
// СЕРВИСНЫЙ СЛОЙ: УПРАВЛЕНИЕ ДЕГТЯРНОЙ ЯМОЙ (TARPIT)
// ============================================================================
// PURPOSE: Единая точка входа для TUI к функциям управления ловушками.
// CONTRACT: Инкапсулирует вызовы tarpitctl.ActivateTrap, DeactivateTrap, DeleteTrap.
//           Возвращает явные error, логирует события через sys.LogAction.
// SIDE-EFFECTS: Взаимодействует с демоном agld_tarpit.service через IPC.
// ============================================================================

// ActivateTrap активирует ловушку на заданном порту.
func ActivateTrap(ctx context.Context, port int, cfg sys.TarpitConfig) error {
	err := tarpitctl.ActivateTrap(port, cfg)
	if err != nil {
		sys.LogAction("SERVICE_TARPIT", "ACTIVATE_FAIL", "ERROR",
			fmt.Sprintf("event=tarpit.activate_failed port=%d err=%v", port, err))
		return fmt.Errorf("сбой активации ловушки на порту %d: %w", port, err)
	}
	sys.LogAction("SERVICE_TARPIT", "ACTIVATE_OK", "SUCCESS",
		fmt.Sprintf("event=tarpit.activated port=%d", port))
	return nil
}

// DeactivateTrap деактивирует ловушку на заданном порту.
func DeactivateTrap(ctx context.Context, port int) error {
	err := tarpitctl.DeactivateTrap(port)
	if err != nil {
		sys.LogAction("SERVICE_TARPIT", "DEACTIVATE_FAIL", "ERROR",
			fmt.Sprintf("event=tarpit.deactivate_failed port=%d err=%v", port, err))
		return fmt.Errorf("сбой деактивации ловушки на порту %d: %w", port, err)
	}
	sys.LogAction("SERVICE_TARPIT", "DEACTIVATE_OK", "SUCCESS",
		fmt.Sprintf("event=tarpit.deactivated port=%d", port))
	return nil
}

// DeleteTrap удаляет ловушку из инвентаря и останавливает её.
func DeleteTrap(ctx context.Context, port int) error {
	err := tarpitctl.DeleteTrap(port)
	if err != nil {
		sys.LogAction("SERVICE_TARPIT", "DELETE_FAIL", "ERROR",
			fmt.Sprintf("event=tarpit.delete_failed port=%d err=%v", port, err))
		return fmt.Errorf("сбой удаления ловушки на порту %d: %w", port, err)
	}
	sys.LogAction("SERVICE_TARPIT", "DELETE_OK", "SUCCESS",
		fmt.Sprintf("event=tarpit.deleted port=%d", port))
	return nil
}

// EnsureDaemonRunning проверяет и запускает демон ловушек, если он не жив.
func EnsureDaemonRunning() error {
	err := tarpitctl.EnsureDaemonRunning()
	if err != nil {
		sys.LogAction("SERVICE_TARPIT", "DAEMON_ENSURE_FAIL", "ERROR",
			fmt.Sprintf("event=tarpit.daemon_ensure_failed err=%v", err))
		return fmt.Errorf("сбой обеспечения работы демона: %w", err)
	}
	return nil
}

// SyncGlobalConfigToCoreTrap синхронизирует глобальный конфиг с базовой ловушкой порта 22.
// PURPOSE: Командная функция для применения изменений конфига к демону.
// CONTRACT: Делегирует в tarpitctl.SyncGlobalConfigToCoreTrap.
// SIDE-EFFECTS: Обновляет конфигурацию базовой ловушки через IPC.
func SyncGlobalConfigToCoreTrap() error {
	err := tarpitctl.SyncGlobalConfigToCoreTrap()
	if err != nil {
		sys.LogAction("SERVICE_TARPIT", "SYNC_CONFIG_FAIL", "ERROR",
			fmt.Sprintf("event=tarpit.sync_config_failed err=%v", err))
		return fmt.Errorf("сбой синхронизации конфига с базовой ловушкой: %w", err)
	}
	sys.LogAction("SERVICE_TARPIT", "SYNC_CONFIG_OK", "SUCCESS",
		"event=tarpit.config_synced")
	return nil
}

// UpdateTrapConfig обновляет конфигурацию существующей ловушки.
// PURPOSE: Командная функция для редактирования ловушки.
// CONTRACT: Делегирует в tarpitctl.UpdateTrapConfig.
// SIDE-EFFECTS: Обновляет tarpit_inventory.json и IPC.
func UpdateTrapConfig(port int, cfg sys.TarpitConfig) error {
	err := tarpitctl.UpdateTrapConfig(port, cfg)
	if err != nil {
		sys.LogAction("SERVICE_TARPIT", "UPDATE_CONFIG_FAIL", "ERROR",
			fmt.Sprintf("event=tarpit.update_config_failed port=%d err=%v", port, err))
		return fmt.Errorf("сбой обновления конфига ловушки на порту %d: %w", port, err)
	}
	sys.LogAction("SERVICE_TARPIT", "UPDATE_CONFIG_OK", "SUCCESS",
		fmt.Sprintf("event=tarpit.config_updated port=%d", port))
	return nil
}
