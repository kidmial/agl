// internal/service/maintenance_service.go
package service

import (
	"agl/internal/maintenance"
)

// SyncAutoUpdateUnits reconciles systemd units of server auto-update
// (agld_autoupdate.service / agld_autoupdate.timer) with the config declaration.
//
// PURPOSE: Bridge between TUI and the maintenance protocol per layering doctrine:
// TUI writes only the declaration (config.Update); unit reconciliation
// happens here, never in the presentation layer.
//
// CONTRACT: enabled=true  → write units, daemon-reload, enable+start timer;
//
//	enabled=false → disable+stop timer, remove units, daemon-reload.
//	Idempotent: identical unit files are not rewritten.
//
// SIDE-EFFECTS: Writes/removes /etc/systemd/system/agld_autoupdate.{service,timer};
// invokes systemctl via sys.RunTimeout.
func SyncAutoUpdateUnits(enabled bool) error {
	return maintenance.EnsureAutoUpdateUnits(enabled)
}
