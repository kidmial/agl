// internal/service/ssh_service.go
package service

import (
	"agl/internal/armor"
	"agl/internal/sys"
	"fmt"
)

// ============================================================================
// СЕРВИСНЫЙ СЛОЙ: РОКИРОВКА SSH (CASTLING)
// ============================================================================
// PURPOSE: Единая точка входа для TUI к функции рокировки SSH.
// CONTRACT: Инкапсулирует вызов armor.SetMovePortProtocol.
//           Возвращает явные error, логирует события через sys.LogAction.
// SIDE-EFFECTS: Модифицирует /etc/ssh/sshd_config, перезапускает sshd.
// ============================================================================

// ToggleSshCastling включает или выключает рокировку SSH.
// action: "ON", "OFF".
func ToggleSshCastling(action string) error {
	err := armor.SetMovePortProtocol(action)
	if err != nil {
		sys.LogAction("SERVICE_SSH", "CASTLING_FAIL", "ERROR",
			fmt.Sprintf("event=ssh.castling_failed action=%s err=%v", action, err))
		return fmt.Errorf("сбой рокировки SSH: %w", err)
	}
	sys.LogAction("SERVICE_SSH", "CASTLING_OK", "SUCCESS",
		fmt.Sprintf("event=ssh.castling_toggled action=%s", action))
	return nil
}
