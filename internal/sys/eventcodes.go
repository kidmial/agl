// internal/sys/eventcodes.go
package sys

const (
	// Кастлинг (рокировка портов)
	EventCastlingComplete = "castling.complete"
	EventCastlingFailed   = "castling.failed"
	EventCastlingRollback = "castling.rollback"

	// Ловушки (Дегтярная яма)
	EventTarpitWorkerStart = "tarpit.worker_start"
	EventTarpitWorkerStop  = "tarpit.worker_stop"
	EventTarpitBindFail    = "tarpit.bind_fail"

	// Веб-стук (канал спасения)
	EventWebKnockProcessed  = "web_knock.processed"
	EventWebKnockInvalidIP  = "web_knock.invalid_ip"
	EventWebKnockBruteforce = "web_knock.bruteforce"

	// Броня периметра
	EventArmorRouteTriggered = "armor.route_triggered"
	EventArmorRouteFailed    = "armor.route_failed"

	// Тюнинг ядра (sysctl)
	EventSysctlApplied  = "sysctl.applied"
	EventSysctlRestored = "sysctl.restored"

	// UFW (брандмауэр)
	EventUfwGateOpen    = "ufw.gate_open"
	EventUfwGateClosed  = "ufw.gate_closed"
	EventUfwRulePresent = "ufw.rule_present"
	EventUfwRuleMissing = "ufw.rule_missing"

	// Конфигурация
	EventConfigLoadFailed = "config.load_failed"
	EventConfigSaved      = "config.saved"

	// Резервное копирование
	EventBackupStarted   = "backup.started"
	EventBackupCompleted = "backup.completed"
	EventBackupFailed    = "backup.failed"

	// Наблюдатель (watcher)
	EventWatcherIdleTimeout  = "watcher.idle_timeout"
	EventWatcherIncineration = "watcher.incineration"
	EventWatcherSessionSeen  = "watcher.session_seen"
)
