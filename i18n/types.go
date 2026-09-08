// i18n/types.go
package i18n

import "fmt"

// SysctlHelpDoc представляет локализованные данные для одного параметра ядра
type SysctlHelpDoc struct {
	ParamName   string
	DefaultVal  string
	MinVal      int
	MaxVal      int
	Description string
	OnAttack    string
	Risks       string
}

// UITranslation описывает жесткий каркас всех текстовых строк TUI-интерфейса.
type UITranslation struct {
	MenuTitle               string
	BtnSettings             string
	BtnAntiDdos             string
	BtnTarpit               string
	BtnExit                 string
	StatusActive            string
	StatusDead              string
	BackupStart             string
	BackupSuccess           string
	BackupErrorNoDirs       string
	BackupErrorTar          string
	BackupProgressItem      string
	MapNotFoundLabel        string
	ArmorSshLimitOn         string
	ArmorSshLimitOff        string
	ArmorSshAdminOk         string
	ArmorSshPortFound       string
	ProtoMovePortStart      string
	ProtoMovePortSuccess    string
	ProtoRestorePortStart   string
	ProtoRestorePortSuccess string
	ProtoSysctlOn           string
	ProtoSysctlOff          string
	ProtoSysctlCheckActive  string
	ProtoSysctlCheckDead    string
	ProtoTarpitOn           string
	ProtoTarpitOff          string
	ProtoTarpitErrPort22    string
	ProtoTarpitWorkerIn     string
	ArmorHintSysctl         string
	ArmorHintLimitSsh       string
	ArmorHintLimitXui       string
	ArmorHintOn             string
	ArmorHintOff            string
	ArmorHintBack           string
	SettingsSaved           string
	SettingsErrDigits       string
	SettingsErrJitter       string
	TarpitHintOn            string
	TarpitHintOff           string
	TarpitHintLive          string
	TarpitHintReport        string
	TarpitHintBack          string
	TarpitHintSettings      string
	TarpitHintProtocol      string
	TarpitHintMove          string
	CoreTitleText           string
	CoreHeaderDesc          string
	CoreHeaderWarning       string
	CoreListTitle           string
	CoreResetWarning        string
	CoreBackText            string
	CoreInfoBoxTitle        string
	CoreSysConsoleTitle     string
	CoreSysConsoleBody      string
	CoreHelpSelectParam     string
	SysctlDocs              []SysctlHelpDoc

	// --- config/config.go (ошибки конфигурации и диска) ---
	ConfigErrOpenCenter         string
	ConfigErrParseStruct        string
	ConfigErrSaveAfterMigration string
	ConfigErrDiskGeometry       string
	ConfigErrInvalidSshPort     string
	ConfigErrSerialization      string
	ConfigErrCreateTempFile     string
	ConfigErrWriteConfig        string
	ConfigErrSyncConfig         string
	ConfigErrCloseTempFile      string
	ConfigErrRenameConfig       string

	// --- config/config_inventory.go (ошибки и системные сообщения) ---
	ConfigErrInitScan              string // Ошибка инициализации при сканировании
	ConfigErrC1Discovery           string // Ошибка Контура 1 (процессы)
	ConfigErrC2SocketAudit         string // Ошибка Контура 2 (сокеты)
	ConfigErrC3Correlation         string // Ошибка Контура 3 (корреляция)
	ConfigErrReadProc              string // Ошибка чтения /proc
	ConfigErrReadStatus            string // Ошибка чтения статуса процесса
	ConfigErrSyncDisk              string // Ошибка синхронизации с диском
	ConfigErrPortExists            string // Порт уже защищен
	ConfigErrUUIDNotFound          string // UUID не найден в инвентаре
	ConfigUnknownProcess           string // Фолбэк имени процесса
	ConfigUnknownProcessSuspicious string // Маркер подозрительной активности

	// --- internal/armor/armorrouter.go ---
	ArmorRouterAdminStateChange      string
	ArmorRouterUnknownPerimeter      string
	ArmorRouterArchFail              string
	ArmorRouterSysctlFail            string
	ArmorRouterSysctlSuccess         string
	ArmorRouterTarpitFail            string
	ArmorRouterTarpitSuccess         string
	ArmorRouterCastlingFail          string
	ArmorRouterCastlingSuccess       string
	ArmorRouterRateLimitFail         string
	ArmorRouterRateLimitCriticalFail string
	ArmorRouterRateLimitSuccess      string
	ArmorRouterInventoryUpdateFail   string
	ArmorRouterServiceInvalidEnable  string
	ArmorRouterServiceInvalidErr     string
	ArmorRouterAutoShieldActivate    string
	ArmorRouterServiceInvalidDisable string
	ArmorRouterAutoShieldDeactivate  string
	ArmorRouterCheckNoTrajectory     string
	ArmorRouterCheckReceiptMissing   string
	ArmorRouterCheckUfwDesync        string
	ArmorRouterCheckServiceDesync    string

	// --- internal/armor/custom_limit_protocol.go ---
	ArmorLimitTarpitReject  string
	ArmorLimitActivateErr   string
	ArmorLimitDeactivateErr string
	ArmorLimitCheckDisabled string
	ArmorLimitUnknownAction string
	ArmorLimitUfwRestored   string
	ArmorLimitPurgeSuccess  string

	// --- internal/armor/multitarpit_service_protocol.go ---
	ArmorMultiTarpitOpenErr       string
	ArmorMultiTarpitGateOpen      string
	ArmorMultiTarpitGateClosed    string
	ArmorMultiTarpitUnknownAction string

	// --- internal/armor/port_move_protocol.go ---
	PortMoveCastlingInactive         string
	PortMoveUnknownAction            string
	PortMovePortBusy                 string
	PortMoveBackupCreateFailed       string
	PortMoveGeometryWriteFailed      string
	PortMoveValidationFailedRollback string
	PortMoveSshRestartFailed         string
	PortMoveValidationFailedOff      string
	PortMoveSshdConfigReadFailed     string
	PortMoveSshdConfigWriteFailed    string
	PortMoveWatcherUnitWriteFailed   string

	// --- internal/armor/ssh_limit_protocol.go ---
	SshLimitCollisionLockError string
	SshLimitDisabled           string
	SshLimitUnknownAction      string

	// --- internal/armor/sysctl_tune_protocol.go ---
	SysctlOptimizationDisabled    string
	SysctlKernelIntervalsMismatch string
	SysctlUnknownAction           string

	// --- internal/armor/tarpit_service_protocol.go ---
	TarpitPortCongestionLockError string
	TarpitUfwGateInitFailed       string
	TarpitActivateCritical        string
	TarpitDaemonDeactivated       string
	TarpitInventoryUnavailable    string
	TarpitCoreTrapNotFound        string
	TarpitUnknownAction           string

	// --- internal/armor/web_rescue_server.go --- WEB Page ---
	WebRescuePageInvalidIP     string
	WebRescuePageFormTitle     string
	WebRescuePageAccessDenied  string
	WebRescuePageAuthRequired  string
	WebRescuePageEnterButton   string
	WebRescuePageSuccessTitle  string
	WebRescuePageAccessGranted string
	WebRescuePageIPAddress     string
	WebRescuePageRecorded      string
	WebRescuePageWaiting       string

	// --- internal/armor/web_rescue_tls.go ---
	TlsCertDirCreateFailed string
	TlsEcdsaKeyGenFailed   string
	TlsSerialGenFailed     string
	TlsCertCreateFailed    string
	TlsKeyFileFailed       string
	TlsCertFileFailed      string

	// --- internal/backup/backup.go ---
	BackupControlDiskCheck      string
	BackupWarningPrefix         string
	BackupFailSessionDirCreate  string
	BackupDpkgExport            string
	BackupSuccessPackageList    string
	BackupErrorDpkgExport       string
	BackupTarSysRecovery        string
	BackupCriticalSysTarFail    string
	BackupLogTarForensics       string
	BackupCriticalLogTarFail    string
	BackupSuccessArchivesDone   string
	BackupFsStatFailed          string
	BackupDiskDeficitFifo       string
	BackupBackupRootReadErr     string
	BackupFifoDeleteOld         string
	BackupFifoSafeThreshold     string
	BackupFifoFailThreshold     string
	BackupArchiveFileCreateFail string
	BackupTarCompressObject     string
	BackupSkipObjectMissing     string
	BackupRecursivePackFail     string
	BackupTarFlushFail          string
	BackupGzipFlushFail         string

	// --- internal/forensics/tarpit_report.go ---
	ForensicsReportTitle           string
	ForensicsGenDate               string
	ForensicsTargetLog             string
	ForensicsColTime               string
	ForensicsColTrap               string
	ForensicsColIP                 string
	ForensicsColPlanFact           string
	ForensicsColBehavior           string
	ForensicsColCountry            string
	ForensicsLogEmptyOrUnavailable string
	ForensicsLogEmpty              string
	ForensicsTotalRecords          string
	ForensicsTrapEmpty             string
	ForensicsStatusClosed          string
	ForensicsSecShort              string

	// --- internal/service/armor_service.go ---
	ArmorRouteFail             string
	ArmorSysctlToggleFail      string
	ArmorRateLimitFail         string
	ArmorUfwReloadFail         string
	ArmorMultiTarpitToggleFail string
	// --- internal/service/inventory_service.go ---
	InventoryScanFail      string
	InventoryLoadPortsFail string
	InventoryLoadAppsFail  string
	// --- internal/service/ssh_service.go ---
	SshCastlingFail string
	// --- internal/service/tarpit_service.go ---
	TarpitServiceActivateFail     string
	TarpitServiceDeactivateFail   string
	TarpitServiceDeleteFail       string
	TarpitServiceDaemonEnsureFail string
	TarpitServiceSyncConfigFail   string
	TarpitServiceUpdateConfigFail string
	// --- internal/sys/runner.go ---
	SysRunnerTimeout string
	// --- internal/sys/tarpit_inventory.go ---
	TarpitInvReadFail        string
	TarpitInvParseFail       string
	TarpitInvSerializeFail   string
	TarpitInvDirCreateFail   string
	TarpitInvTmpCreateFail   string
	TarpitInvDataWriteFail   string
	TarpitInvFsyncFail       string
	TarpitInvRenameFail      string
	TarpitInvLockOpenFail    string
	TarpitInvLockAcquireFail string
	TarpitInvTxCurrupted     string
}

// LogTranslation описывает человекочитаемые описания событий для журнала.
type LogTranslation struct {
	Events map[string]string
}

var (
	UILanguages  = map[string]UITranslation{}
	LogLanguages = map[string]LogTranslation{}
	T            UITranslation
	L            LogTranslation
)

// Временные алиасы для совместимости с существующим кодом (tui/, main.go).
// Будут удалены после завершения задачи 6.7.
type Translation = UITranslation

var Languages = UILanguages

// InitLanguage инициализирует активный язык панели при старте системы
func InitLanguage(currentLang string) {
	if val, ok := UILanguages[currentLang]; ok {
		T = val
	} else {
		T = UILanguages["en"]
	}

	if val, ok := LogLanguages[currentLang]; ok {
		L = val
	} else {
		L = LogLanguages["en"]
	}
}

// TrEvent возвращает локализованное описание события по машинному коду.
// БЕЗ форматирования — описание возвращается как есть.
//
// Используется в auditor.go для записи в журнал, где
// динамические данные передаются отдельным полем `details`.
//
// Если код не найден в L.Events — возвращается сам машинный код (А.2, фолбэк).
// Строка журнала НИКОГДА не пуста.
func TrEvent(eventCode string) string {
	if val, ok := L.Events[eventCode]; ok && val != "" {
		return val
	}
	return eventCode
}

// TrEventf возвращает локализованное описание события по машинному коду
// С форматированием аргументов (аналог fmt.Sprintf).
//
// Используется для событий с параметрами:
//   - "Рокировка портов завершена: порт %d"
//   - "Сканер из %s захвачен в капкан"
//
// Если код не найден или шаблон пуст — форматируется сам машинный код.
// Строка журнала НИКОГДА не пуста.
func TrEventf(eventCode string, args ...interface{}) string {
	tpl := eventCode
	if val, ok := L.Events[eventCode]; ok && val != "" {
		tpl = val
	}
	if len(args) == 0 {
		return tpl
	}
	return fmt.Sprintf(tpl, args...)
}

func GetSettingsGroupHeader(group string, isEn bool) string {
	switch group {
	case "LANG":
		if isEn {
			return "─── LOCALIZATION  & INTERFACE MANAGEMENT "
		}
		return "─── РЕГЛАМЕНТ ЛОКАЛИЗАЦИИ И ЯЗЫК ИНТЕРФЕЙСА "
	case "TARPIT":
		if isEn {
			return "─── SSH DECEPTION TARPIT INTERVALS (PORT 22) "
		}
		return "─── ПАРАМЕТРЫ ЯДРА ДЕГТЯРНОЙ ЯМЫ SSH (ПОРТ 22)  "
	case "LIMITS":
		if isEn {
			return "─── ANTI-DDOS BOUNDARIES  & FIREWALL LIMITS "
		}
		return "─── СИСТЕМНЫЕ ЛИМИТЫ И АНТИ-DDOS-ОБОРOНА "
	case "SYSTEM":
		if isEn {
			return "─── CORE MANAGEMENT SERVICES "
		}
		return "─── СЕРВИСНЫЕ И УПРАВЛЯЮЩИЕ КОМАНДЫ "
	default:
		return "────────────────────────────────────────────── "
	}
}

func GetSettingsStatusText(key string, isEn bool) string {
	translations := map[string][]string{
		"title_box":            {" Раздел конфигурации системы  ", " System configuration sector  "},
		"header_desc":          {"Глобальная Конфигурация Периметра (SETTINGS) ", "Global Perimeter Configuration (SETTINGS) "},
		"form_title":           {" Модификация портов, интервалов ядра и локализации системы  ", " Modification of ports, kernel intervals  & localization  "},
		"editing_mode":         {" РЕЖИМ РЕДАКТИРОВАНИЯ ПАРАМЕТРА  ", " PARAMETER EDITING MODE  "},
		"enter_new":            {"Введите новое значение ", "Enter new value "},
		"input_prompt":         {"ЗНАЧЕНИЕ ", "VALUE "},
		"hint_nav_1":           {"[ НАВИГАЦИЯ ]: Стрелки ↑/↓ │ [ ИЗМЕНЕНИЕ ]: Нажмите ENTER, чтобы открыть параметр ", "[ NAVIGATION ]: Arrows ↑/↓ │ [ MODIFY ]: Press ENTER to open parameter "},
		"hint_nav_2":           {"[ ВЫХОД ]: Выберите строку НАЗАД или нажмите ESC для закрытия конфигуратора ", "[ EXIT ]: Choose BACK row or press ESC to close config "},
		"hint_esc":             {"Нажмите [ESC](fg:yellow,mod:bold) в любой момент для отмены изменений.", "Press [ESC](fg:yellow,mod:bold) at any time to cancel changes."},
		"empty_val":            {"Значение не может быть пустым! ", "Value cannot be empty! "},
		"out_bounds":           {"Выход за допустимые системные границы (1-65535)! ", "Value is out of valid bounds (1-65535)! "},
		"status_console_title": {" СИСТЕМНАЯ КОНСОЛЬ  ", " SYSTEM CONSOLE  "},
		"lang_support":         {"Допустимые значения локали: RU или EN ", "Supported interface locales: RU or EN "},
		"tarpit_lock":          {"⚠️ ОТКАЗ: Дегтярная Яма АКТИВНА! Лимиты UFW заблокированы во избежание сброса логов. ", "⚠️ LOCK: Tarpit is ACTIVE! UFW rate limiting disabled to preserve deception analytics. "},
	}
	if vec, ok := translations[key]; ok {
		if isEn {
			return vec[1]
		}
		return vec[0]
	}
	return key
}

func FormatTuiHelpText(doc SysctlHelpDoc, curVal int, lang string) string {
	if doc.ParamName == "" {
		return UILanguages[lang].CoreHelpSelectParam
	}
	descLabel := " [ОПИСАНИЕ]:"
	attackLabel := " [ПРИ DDoS АТАКЕ]:"
	risksLabel := " [КРИТИЧЕСКИЕ РИСКИ]:"
	curValLabel := "Текущее боевое значение в config.json:"
	limitLabel := "Безопасные границы регулирования: от"
	factoryLabel := "Заводской эталон:"

	if lang == "en" {
		descLabel = " [DESCRIPTION]:"
		attackLabel = " [UPON DDoS ATTACK]:"
		risksLabel = " [CRITICAL RISKS]:"
		curValLabel = "Current operational value in config.json:"
		limitLabel = "Safe regulation boundaries: from"
		factoryLabel = "Factory template:"
	}
	return fmt.Sprintf(
		" Параметр: %s \n"+
			" %s %d (%s %s)\n"+
			" %s [ %d ] до [ %d ]\n\n"+
			"%s\n %s\n\n"+
			"%s\n %s\n\n"+
			"%s\n %s ",
		doc.ParamName, curValLabel, curVal, factoryLabel, doc.DefaultVal, limitLabel, doc.MinVal, doc.MaxVal, descLabel, doc.Description, attackLabel, doc.OnAttack, risksLabel, doc.Risks,
	)
}
