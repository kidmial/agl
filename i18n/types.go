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
	ConfigErrSaveAfterPrefixGen string

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

	// --- internal/tarpit/tarpit_manager.go ---
	TarpitManagerTrapRemovedFromInventory string
	TarpitManagerTrapMissingFromInventory string
	TarpitManagerMarkerSerializeFail      string
	TarpitManagerMarkerWriteDiskFail      string

	// --- internal/tarpitctl/tarpit_controller.go ---
	TarpitCtlInventoryWriteCriticalFail string
	TarpitCtlDaemonStartFail            string
	TarpitCtlTrapNotFound               string
	TarpitCtlTrapActiveStop             string
	TarpitCtlDaemonStillStopping        string
	TarpitCtlTrapActiveEdit             string
	TarpitCtlTrapNotFoundByPort         string
	TarpitCtlUnitGenerateFail           string
	TarpitCtlDaemonStartCriticalFail    string
	TarpitCtlUnitWriteDiskFail          string
	TarpitCtlPortOccupiedByProcess      string
	TarpitCtlPortOccupiedByKernel       string
	TarpitCtlCastlingNotDone            string
	TarpitCtlCastlingDesync             string

	// --- tui/view_armor_controller.go ---
	ViewArmorCtlProgressTitleOn       string
	ViewArmorCtlProgSysctlTune        string
	ViewArmorCtlProgArmPort           string
	ViewArmorCtlProgArmService        string
	ViewArmorCtlProgNetfilterReload   string
	ViewArmorCtlHintArmorOnDone       string
	ViewArmorCtlProgressTitleOff      string
	ViewArmorCtlProgSysctlReset       string
	ViewArmorCtlProgDisarmPort        string
	ViewArmorCtlProgDeactivateService string
	ViewArmorCtlProgNetfilterCleanup  string
	ViewArmorCtlHintArmorOffDone      string
	ViewArmorCtlHintTarpitReject      string
	ViewArmorCtlErrorPrefix           string
	ViewArmorCtlHintPerimeterOn       string
	ViewArmorCtlHintPerimeterOff      string
	ViewArmorCtlCoreSaveFailPrefix    string
	ViewArmorCtlCoreResetSuccess      string

	// --- tui/view_armor_progress.go ---
	ViewArmorProgressTransactionLabel  string
	ViewArmorProgressInitKernel        string
	ViewArmorProgressLabelSystemStatus string
	ViewArmorProgressNetfilterRebuilt  string
	ViewArmorProgressLabelSuccess      string
	ViewArmorProgressArmorTransferred  string
	ViewArmorProgressHintExit          string
	ViewArmorProgressHintOr            string
	ViewArmorProgressHintReturn        string

	// --- tui/view_armor.go ---
	ViewArmorHintSysctl           string
	ViewArmorHintScanPerimeter    string
	ViewArmorHintCoreSettings     string
	ViewArmorHintArmorOn          string
	ViewArmorHintArmorOff         string
	ViewArmorHintBack             string
	ViewArmorHintDynamicPerimeter string
	ViewArmorListSysctl           string
	ViewArmorListScanPerimeter    string
	ViewArmorListCoreSettings     string
	ViewArmorListManualPort       string
	ViewArmorPrefixService        string
	ViewArmorListWebKnockPort     string
	ViewArmorListArmorOn          string
	ViewArmorListArmorOff         string
	ViewArmorListBack             string
	ViewArmorHeaderTitle          string
	ViewArmorListTitle            string
	ViewArmorInfoBoxTitle         string
	ViewArmorConsoleTitle         string
	ViewArmorHeaderDesc           string
	ViewArmorLabelNav             string
	ViewArmorLabelNavArrows       string
	ViewArmorLabelActivation      string
	ViewArmorLabelActivationHint  string
	ViewArmorLabelInfo            string
	ViewArmorLabelScrollInfo      string
	ViewArmorLabelExit            string
	ViewArmorLabelExitHint        string

	// --- tui/view_backup.go ---
	ViewBackupGaugeTitle          string
	ViewBackupGaugeObjectTitleTpl string
	ViewBackupConsoleTitle        string
	ViewBackupConfirmTitle        string
	ViewBackupLabelSuccess        string
	ViewBackupConfirmSuccessText  string
	ViewBackupConfirmPressKey     string
	ViewBackupConfirmOr           string
	ViewBackupConfirmReturnMenu   string

	// --- tui/view_core_settings.go ---
	ViewCoreSettingsRowSyncookies        string
	ViewCoreSettingsRowFinTimeout        string
	ViewCoreSettingsRowSynRetries        string
	ViewCoreSettingsRowSynackRetries     string
	ViewCoreSettingsRowSomaxconn         string
	ViewCoreSettingsRowMaxSynBacklog     string
	ViewCoreSettingsRowRpFilter          string
	ViewCoreSettingsRowResetToFactory    string
	ViewCoreSettingsRowBackToArmor       string
	ViewCoreSettingsEditModeTitle        string
	ViewCoreSettingsLabelParam           string
	ViewCoreSettingsLabelEnterNew        string
	ViewCoreSettingsErrEmptyValue        string
	ViewCoreSettingsErrOutOfBounds       string
	ViewCoreSettingsErrSaveFailPrefix    string
	ViewCoreSettingsSuccessSaved         string
	ViewCoreSettingsErrWriteRejectPrefix string

	// --- tui/view_forensics.go ---
	ViewForensicsHeaderTitle       string
	ViewForensicsLabelCollectMode  string
	ViewForensicsLabelAutoParallel string
	ViewForensicsLabelSiemRegistry string
	ViewForensicsListTitle         string
	ViewForensicsRowThreatMap      string
	ViewForensicsRowSshLog         string
	ViewForensicsRowWebLog         string
	ViewForensicsRowBotnet         string
	ViewForensicsRowIpForensics    string
	ViewForensicsRowBack           string
	ViewForensicsInfoBoxTitle      string

	// --- tui/view_main.go ---
	ViewMainLabelMainMenu      string
	ViewMainLabelDate          string
	ViewMainLabelTime          string
	ViewMainLabelCpu           string
	ViewMainLabelRam           string
	ViewMainLabelSwap          string
	ViewMainLabelLoad          string
	ViewMainLabelUsage         string
	ViewMainLabelWebWatcher    string
	ViewMainLabelTarpit        string
	ViewMainLabelCastling      string
	ViewMainLabelTlsGate       string
	ViewMainLabelBotsTrapped   string
	ViewMainPassportTitle      string
	ViewMainServicesTitle      string
	ViewMainMenuTitle          string
	ViewMainRowSettings        string
	ViewMainRowAntiddos        string
	ViewMainRowTarpit          string
	ViewMainRowMultiTarpit     string
	ViewMainRowPortMap         string
	ViewMainRowBackup          string
	ViewMainRowExit            string
	ViewMainFallbackUnknownCpu string
	ViewMainFallbackError      string
	ViewMainFallbackZeroMb     string

	// --- tui/view_main.go (stats block) ---
	StatsBlockTitle    string
	StatsTrapsActive   string
	StatsOpenPorts     string
	StatsLogProgram    string
	StatsLogConnection string
	StatsLogReport     string
	StatsLastSnapshot  string
	StatsBackupCount   string
	StatsNever         string
	StatsMinutesAgo    string
	StatsHoursAgo      string
	StatsDaysAgo       string
	StatsLogBlockTitle string

	// --- tui/view_main.go (additional) ---
	StatsLabelLogs      string
	StatsLabelSnapshot  string
	StatsLabelBackups   string
	StatsLabelTraps     string
	StatsLabelPorts     string
	StatusOn            string
	StatusOff           string
	UnitMB              string
	UnitKB              string
	EmptyLog            string
	DateFormat          string
	TimeFormat          string
	ViewMainHeaderTpl   string
	ViewMainPassportTpl string
	ViewMainServicesTpl string
	ViewMainStatsTpl1   string
	ViewMainStatsTpl2   string

	// --- tui/view_multi_tarpit.go ---
	ViewMultiTarpitHeaderTitle            string
	ViewMultiTarpitHeaderDesc             string
	ViewMultiTarpitLabelDeclaredState     string
	ViewMultiTarpitLabelAdminWants        string
	ViewMultiTarpitLabelDaemonConfirmed   string
	ViewMultiTarpitLabelMarkerOnDisk      string
	ViewMultiTarpitErrLoadInventoryPrefix string
	ViewMultiTarpitInventoryEmpty         string
	ViewMultiTarpitColPort                string
	ViewMultiTarpitRowAddNewTrap          string
	ViewMultiTarpitRowBack                string
	ViewMultiTarpitListTitle              string
	ViewMultiTarpitLabelTotal             string
	ViewMultiTarpitInfoBoxTitle           string
	ViewMultiTarpitLabelTotalIntegrated   string
	ViewMultiTarpitLabelDeclInventory     string
	ViewMultiTarpitLabelThreeFactor       string
	ViewMultiTarpitLabelFileOnDisk        string
	ViewMultiTarpitHelpEnterLine          string
	ViewMultiTarpitHelpEscLine            string
	ViewMultiTarpitLabelWorker            string
	ViewMultiTarpitWorkerIdleText         string
	ViewMultiTarpitLabelStarted           string
	ViewMultiTarpitLabelTrap              string
	ViewMultiTarpitLabelCore              string
	ViewMultiTarpitLabelSshBanner         string
	ViewMultiTarpitLabelRetention         string
	ViewMultiTarpitLabelSec               string
	ViewMultiTarpitLabelSessionLimit      string
	ViewMultiTarpitLabelMarker            string
	ViewMultiTarpitPassportFooter         string

	// --- tui/view_port_map.go ---
	ViewPortMapHeaderTitle     string
	ViewPortMapHeaderDesc      string
	ViewPortMapWebKnockLabel   string
	ViewPortMapColSocket       string
	ViewPortMapColDefense      string
	ViewPortMapRowBack         string
	ViewPortMapListTitle       string
	ViewPortMapInfoBoxTitle    string
	ViewPortMapInfoBoxText     string
	ViewPortMapConsoleTitle    string
	ViewPortMapLabelNav        string
	ViewPortMapLabelNavArrows  string
	ViewPortMapLabelExit       string
	ViewPortMapLabelExitHint   string
	ViewPortMapLabelInfo       string
	ViewPortMapLabelScrollInfo string
	ViewPortMapLabelStatus     string
	ViewPortMapLabelStatusSync string

	// --- tui/view_router.go ---
	ViewRouterAnalyticsStatus            string
	ViewRouterTrapPortAlreadyInInventory string
	ViewRouterErrActivatePrefix          string
	ViewRouterTrapActivatedSuccess       string
	ViewRouterTrapPortReadOnly           string
	ViewRouterErrSavePrefix              string
	ViewRouterScanLoadingText            string

	// --- tui/view_settings.go ---
	ViewSettingsErrPasswordTooShort          string
	ViewSettingsErrPasswordTooLong           string
	ViewSettingsErrHashGenerationFail        string
	ViewSettingsErrPortsConflictStandardReal string
	ViewSettingsErrPortsConflictRealStandard string
	ViewSettingsErrPortsChangeTarpitActive   string
	ViewSettingsErrPortsChangeCastlingActive string
	ViewSettingsErrSavePrefix                string
	ViewSettingsPasswordSet                  string
	ViewSettingsPasswordNotSet               string
	SettingsLabelLang                        string
	SettingsLabelStandardSshPort             string
	SettingsLabelRealSshPort                 string
	SettingsLabelDelayStep                   string
	SettingsLabelDelayVariance               string
	SettingsLabelTarpitMaxConn               string
	SettingsLabelAdminIdleTimeout            string
	SettingsLabelWebRescueEnabled            string
	SettingsLabelWebRescuePort               string
	SettingsLabelWebRescuePassword           string
	SettingsLabelBack                        string
	SettingsUiTitleBox                       string
	SettingsUiHeaderDesc                     string
	SettingsUiFormTitle                      string
	SettingsUiEditingMode                    string
	SettingsUiEnterNew                       string
	SettingsUiInputPrompt                    string
	SettingsUiHintNav1                       string
	SettingsUiHintNav2                       string
	SettingsUiHintEsc                        string
	SettingsUiStatusConsoleTitle             string
	SettingsUiEmptyVal                       string
	SettingsUiOutOfBounds                    string
	SettingsUiLangSupport                    string
	SettingsGroupLang                        string
	SettingsGroupTarpit                      string
	SettingsGroupLimits                      string
	SettingsGroupSystem                      string
	SettingsGroupDefault                     string

	// --- M5 (v8.4.0): Автообновления сервера ---
	SettingsLabelAutoUpdateEnabled string // Тумблер автообновлений (0=выкл / 1=вкл)
	SettingsLabelAutoUpdateReboot  string // Тумблер авто-ребута после обновлений (0=выкл / 1=вкл)

	// --- tui/view_core_settings.go (TD-2: подписи справки) ---
	CoreDocDesc    string
	CoreDocAttack  string
	CoreDocRisks   string
	CoreDocCurVal  string
	CoreDocLimit   string
	CoreDocFactory string

	// --- tui/view_tarpit_controller.go ---
	ViewTarpitCtlLockCritical           string
	ViewTarpitCtlLockReason             string
	ViewTarpitCtlPressEnterToReturn     string
	ViewTarpitCtlReverseCastlingStart   string
	ViewTarpitCtlDirectCastlingStart    string
	ViewTarpitCtlDemonsRestarted        string
	ViewTarpitCtlAlreadyActiveWarning   string
	ViewTarpitCtlAlreadyActiveCollision string
	ViewTarpitCtlStartInit              string
	ViewTarpitCtlStartCriticalReject    string
	ViewTarpitCtlStartSuccess           string
	ViewTarpitCtlStopDismantle          string
	ViewTarpitCtlStopSuccess            string
	ViewTarpitCtlProtocolHeader         string
	ViewTarpitCtlProtocolPressEnter     string

	// --- tui/view_tarpit_live_box.go ---
	ViewTarpitLiveBoxHeaderText string
	ViewTarpitLiveBoxListTitle  string
	ViewTarpitLiveBoxHintExit   string

	// --- tui/view_tarpit.go ---
	ViewTarpitHeaderTitle            string
	ViewTarpitLabelCurrentSshPort    string
	ViewTarpitLabelCastling          string
	ViewTarpitLabelJitterSec         string
	ViewTarpitLabelEngineStatus      string
	ViewTarpitLabelRetentionRange    string
	ViewTarpitLabelSec               string
	ViewTarpitLabelBotsInTrap        string
	ViewTarpitLabelAdminIdleCounter  string
	ViewTarpitLabelMin               string
	ViewTarpitListTitle              string
	ViewTarpitRowLiveBoxDesc         string
	ViewTarpitRowSettingsDesc        string
	ViewTarpitRowMovePortDesc        string
	ViewTarpitRowOnDesc              string
	ViewTarpitRowOffDesc             string
	ViewTarpitRowHelpDesc            string
	ViewTarpitRowBackDesc            string
	ViewTarpitInfoBoxTitle           string
	ViewTarpitLiveTerminalTitle      string
	ViewTarpitReportTerminalTitleTpl string
	ViewTarpitLogUnavailable         string

	// --- tui/view_trap_card.go ---
	ViewTrapCardHeaderTitle            string
	ViewTrapCardLabelPort              string
	ViewTrapCardErrNotFound            string
	ViewTrapCardLabelIdentifier        string
	ViewTrapCardLabelSshBanner         string
	ViewTrapCardLabelRetention         string
	ViewTrapCardLabelSec               string
	ViewTrapCardLabelSessionLimit      string
	ViewTrapCardLabelStatus            string
	ViewTrapCardLabelCore              string
	ViewTrapCardRowBackToList          string
	ViewTrapCardRowStartActivate       string
	ViewTrapCardRowStartAlreadyActive  string
	ViewTrapCardRowStopDeactivate      string
	ViewTrapCardRowStopAlreadyStopped  string
	ViewTrapCardRowEditParams          string
	ViewTrapCardRowEditRequiresStop    string
	ViewTrapCardRowDeleteFull          string
	ViewTrapCardRowDeleteWaiting       string
	ViewTrapCardRowDeleteRequiresStop  string
	ViewTrapCardRowBackToPool          string
	ViewTrapCardListTitle              string
	ViewTrapCardHintTitle              string
	ViewTrapCardHintNotFound           string
	ViewTrapCardHintStartAlreadyActive string
	ViewTrapCardHintStartInfo          string
	ViewTrapCardHintStopAlreadyStopped string
	ViewTrapCardHintStopInfo           string
	ViewTrapCardHintEditBlocked        string
	ViewTrapCardHintEditInfo           string
	ViewTrapCardHintDeleteBlocked      string
	ViewTrapCardHintDeleteWaiting      string
	ViewTrapCardHintDeleteInfo         string
	ViewTrapCardHintBack               string

	// --- tui/view_trap_creator.go ---
	ViewTrapCreatorHeaderTitle            string
	ViewTrapCreatorLabelDate              string
	ViewTrapCreatorLabelTime              string
	ViewTrapCreatorHeaderDesc1            string
	ViewTrapCreatorHeaderDesc2            string
	ViewTrapCreatorListTitle              string
	ViewTrapCreatorInfoBoxTitle           string
	ViewTrapCreatorEditTitle              string
	ViewTrapCreatorNavTitle               string
	ViewTrapCreatorRowPortLabel           string
	ViewTrapCreatorRowDelayStepLabel      string
	ViewTrapCreatorRowVarianceLabel       string
	ViewTrapCreatorRowBannerLabel         string
	ViewTrapCreatorRowMaxConnLabel        string
	ViewTrapCreatorRowPortDesc            string
	ViewTrapCreatorRowDelayStepDesc       string
	ViewTrapCreatorRowVarianceDesc        string
	ViewTrapCreatorRowBannerDesc          string
	ViewTrapCreatorRowMaxConnDesc         string
	ViewTrapCreatorRowGenerate            string
	ViewTrapCreatorRowCancel              string
	ViewTrapCreatorHelpPort               string
	ViewTrapCreatorHelpDelayStep          string
	ViewTrapCreatorHelpVariance           string
	ViewTrapCreatorHelpBanner             string
	ViewTrapCreatorHelpMaxConn            string
	ViewTrapCreatorHelpGenerate           string
	ViewTrapCreatorHelpAbort              string
	ViewTrapCreatorParamPort              string
	ViewTrapCreatorParamDelayStep         string
	ViewTrapCreatorParamVariance          string
	ViewTrapCreatorParamMaxConn           string
	ViewTrapCreatorLabelEditNode          string
	ViewTrapCreatorLabelEnterNewValue     string
	ViewTrapCreatorValidationRejectPrefix string
	ViewTrapCreatorNavBody                string
	ViewTrapCreatorLabelSec               string
	ViewTrapCreatorErrEmpty               string
	ViewTrapCreatorErrNotDigit            string
	ViewTrapCreatorErrPortRange           string
	ViewTrapCreatorErrPort22Reserved      string
	ViewTrapCreatorErrRescuePortTpl       string
	ViewTrapCreatorErrHiddenSshPortTpl    string
	ViewTrapCreatorErrDelayRange          string
	ViewTrapCreatorErrVarianceRangeTpl    string
	ViewTrapCreatorErrMaxConnRange        string

	// --- tui/view_trap_editor.go ---
	ViewTrapEditorErrLoadInventory       string
	ViewTrapEditorErrNotFound            string
	ViewTrapEditorRowPortLabel           string
	ViewTrapEditorRowPortReadOnlyDesc    string
	ViewTrapEditorRowDelayStepLabel      string
	ViewTrapEditorLabelSec               string
	ViewTrapEditorRowDelayStepDesc       string
	ViewTrapEditorRowVarianceLabel       string
	ViewTrapEditorRowVarianceDesc        string
	ViewTrapEditorRowBannerLabel         string
	ViewTrapEditorRowBannerDesc          string
	ViewTrapEditorRowMaxConnLabel        string
	ViewTrapEditorRowMaxConnDesc         string
	ViewTrapEditorRowSave                string
	ViewTrapEditorRowCancel              string
	ViewTrapEditorHeaderTitle            string
	ViewTrapEditorLabelPort              string
	ViewTrapEditorLabelDate              string
	ViewTrapEditorLabelTime              string
	ViewTrapEditorHeaderDesc1            string
	ViewTrapEditorHeaderDesc2            string
	ViewTrapEditorListTitle              string
	ViewTrapEditorHelpPort               string
	ViewTrapEditorHelpDelayStep          string
	ViewTrapEditorHelpVariance           string
	ViewTrapEditorHelpBanner             string
	ViewTrapEditorHelpMaxConn            string
	ViewTrapEditorHelpSave               string
	ViewTrapEditorHelpCancel             string
	ViewTrapEditorInfoBoxTitle           string
	ViewTrapEditorEditTitle              string
	ViewTrapEditorParamDelayStep         string
	ViewTrapEditorParamVariance          string
	ViewTrapEditorParamMaxConn           string
	ViewTrapEditorLabelEditNode          string
	ViewTrapEditorLabelEnterNewValue     string
	ViewTrapEditorNavTitle               string
	ViewTrapEditorValidationRejectPrefix string
	ViewTrapEditorLabelNav               string
	ViewTrapEditorLabelNavArrows         string
	ViewTrapEditorLabelEnterHint         string
	ViewTrapEditorLabelAction            string
	ViewTrapEditorLabelActionHint        string
	ViewTrapEditorErrEmpty               string
	ViewTrapEditorErrNotDigit            string
	ViewTrapEditorErrDelayRange          string
	ViewTrapEditorErrVarianceRangeTpl    string
	ViewTrapEditorErrMaxConnRange        string
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

// InitLanguage инициализирует активный язык панели при старте системы
func InitLanguage(currentLang string) {
	if val, ok := UILanguages[currentLang]; ok {
		T = val
	} else {
		T = UILanguages["en"]
	}

	// M5 (v8.4.0): фолбэк для новых полей автообновлений, если словарь неполный
	if T.SettingsLabelAutoUpdateEnabled == "" {
		T.SettingsLabelAutoUpdateEnabled = "Auto-update server (0=off / 1=on)"
	}
	if T.SettingsLabelAutoUpdateReboot == "" {
		T.SettingsLabelAutoUpdateReboot = "Auto-reboot after kernel updates (0=off / 1=on)"
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

// Если код не найден или шаблон пуст — форматируется сам машинный код.
// Строка журнала НИКОГДА не пуста.
//
// Используется для событий с параметрами:
//   - "Рокировка портов завершена: порт %d"
//   - "Сканер из %s захвачен в капкан"
func TrEventf(eventCode string, args ...interface{}) string {
	tpl, ok := L.Events[eventCode]
	if !ok || tpl == "" {
		// Спека А.2: фолбэк в машинный код дословно, без форматирования
		return eventCode
	}
	if len(args) == 0 {
		return tpl
	}
	return fmt.Sprintf(tpl, args...)
}
