// i18n/types_web.go
package i18n

// PanelTranslation описывает жесткий каркас всех текстовых строк веб-панели аналитики.
// Отделена от UITranslation (TUI), чтобы не смешивать консольные и веб-домены.
type PanelTranslation struct {
	// -- internal/webpanel/ui_panel.go --
	// Временные зоны и метки времени
	TimezoneLocal string
	TimezoneUTC   string
	TimeNoData    string // Заглушка, когда время не определено (например, "—")

	// Классификация поведения (Behavior)
	BehaviorUNK   string // Неизвестно
	BehaviorMIXED string // Смешанное
	BehaviorAGGR  string // Агрессивное
	BehaviorPASS  string // Пассивное

	// Системные ошибки
	ErrorTemplateExecution string

	// -- Shared (переиспользуемые переменные для всех шаблонов) --
	SharedUnitSec              string // "сек"
	SharedUnitMs               string // "мс"
	SharedNoData               string // "Нет данных"
	SharedNoDataPeriod         string // "Нет данных за выбранный период."
	SharedNoData24h            string // "Нет данных за последние 24 часа"
	SharedNoActiveProfiles     string // "Нет активных профилей"
	SharedNoEvidence           string // "Нет зафиксированных доказательств."
	SharedNoReasons            string // "Нет зафиксированных причин."
	SharedNoPatternsLastHour   string // "За последний час ботнет-паттернов не обнаружено."
	SharedModeFullHistory      string // "Полная история"
	SharedModeLiveSlice        string // "Живой срез"
	SharedShow                 string // "Показать"
	SharedOf                   string // "из"
	SharedTableIP              string // "IP"
	SharedTableEvents          string // "Событий"
	SharedTableShare           string // "Доля"
	SharedTableTrap            string // "Ловушка"
	SharedTableUniqueIPs       string // "Уник. IP"
	SharedTableUniqueCountries string // "Уник. стран"

	// -- internal/webpanel/templates/base.html (HTML) --
	HTMLLang        string // Значение атрибута lang (ru/en)
	NavOverview     string // "Обзор" / "Overview"
	NavVitals       string // "Динамика" / "Vitals"
	NavFingerprints string // "Отпечатки" / "Fingerprints"
	NavAlgorithms   string // "Алгоритмы" / "Algorithms"
	NavSIEM         string // "SIEM" / "SIEM"
	NavReports      string // "Отчёты" / "Reports"
	NavLogout       string // "Выход" / "Logout"

	AriaAutorefresh string // aria-label для чекбокса автообновления
	AriaPrivacy     string // aria-label для чекбокса приватности
	AriaTheme       string // aria-label для чекбокса темы

	TitleAutorefresh string // tooltip автообновления
	TitlePrivacy     string // tooltip приватности
	TitleTheme       string // tooltip темы

	// -- internal/webpanel/templates/base.html (JavaScript) --
	JSStatusActive10s string // "активно (10 сек)" / "active (10 sec)"
	JSStatusPaused    string // "пауза" / "paused"
	JSLoading         string // "Загрузка..." / "Loading..."
	JSHistoryLoaded   string // "История загружена" / "History loaded"
	JSLoadHistory     string // "Загрузить историю из лога" / "Load history from log"
	JSReadingLog      string // "Чтение файла лога..." / "Reading log file..."
	JSLoadError       string // "Ошибка загрузки" / "Load error"
	JSNetworkError    string // "Ошибка сети: " / "Network error: "
	JSRestartService  string // "Перезапустите сервис." / "Please restart service."

	// -- internal/webpanel/templates/dashboard_fingerprints.html (HTML) --
	FingerprintsPageTitle     string // "Отчёт о цифровом отпечатке и почерке"
	FingerprintsPageDesc      string // "Декомпозиция следа цифрового p0f идентификатора..."
	FingerprintsReadDistTitle string // "Распределение прикладных чтений"
	FingerprintsReadDistDesc  string // "Категории: tiny (0-64 байт), small (65-256)..."
	FingerprintsTopTCPTitle   string // "Топ TCP-отпечатков (p0f-логика)"
	FingerprintsTopTCPDesc    string // "Уникальные комбинации MSS|RcvSpace|SndWscale|RcvWscale..."
	FingerprintsTableCol1     string // "Отпечаток (MSS|RcvSpace|SndW|RcvW)"
	FingerprintsTableCol2     string // "Количество событий"
	FingerprintsTableCol3     string // "Доля"
	FingerprintsIATDispTitle  string // "Дисперсия ритма IAT"
	FingerprintsIATDispDesc   string // "Средний дисперсионный след по часам..."
	FingerprintsRTTTitle      string // "Средний RTT по часам (мс)"
	FingerprintsRTTDesc       string // "Динамика сетевой задержки (Round Trip Time)."

	// -- internal/webpanel/templates/dashboard_fingerprints.html (JavaScript) --
	JSReadsCount       string // "Количество чтений"
	JSAvgIATDispersion string // "Средняя дисперсия IAT"
	JSAvgRTT           string // "Средний RTT (мс)"

	// -- internal/webpanel/templates/dashboard_overview.html (HTML) --
	OverviewPageTitle       string // "Панель мониторинга киберугроз"
	OverviewPageDesc        string // "Поведенческий анализ недавней активности акторов..."
	OverviewAnalyticsStatus string // "Статус аналитики"
	OverviewTotalEvents     string // "Всего событий в буфере"
	OverviewValid           string // "Валидных:"
	OverviewBroken          string // "Битых:"
	OverviewMode            string // "Режим:"
	OverviewActiveProfiles  string // "Активных кластеров профилей акторов"
	OverviewLowConfidence   string // "Низкая уверенность:"
	OverviewExpired         string // "Просрочено:"
	OverviewTimeRange       string // "Временной охват"
	OverviewUpdated         string // "Обновлено:"
	OverviewRealtimeUpdate  string // "Данные обновляются в реальном времени. Автообновление страницы:"
	OverviewStolenHour      string // "Утилизировано времени ботнет за текущий час"
	OverviewStolenDay       string // "Утилизировано времени ботнет за 24 часа"
	OverviewStolenWeek      string // "Утилизировано времени ботнет за 7 дней"
	OverviewStolenMonth     string // "Утилизировано времени ботнет за 30 дней"
	OverviewEvents24h       string // "События за 24 часа"
	OverviewTableHour       string // "Час"
	OverviewTableAggressive string // "Агрессивных"
	OverviewTableAvgRTT     string // "Средний RTT"
	OverviewFreshProfiles   string // "Свежие профили акторов"
	OverviewTableEndTime    string // "Время завершения"
	OverviewTableEventCount string // "Cобытий"
	OverviewTableType       string // "Тип"
	OverviewTablePlan       string // "План (сек)"
	OverviewTableFact       string // "Факт (сек)"
	OverviewTablePhase      string // "Фаза (Алг.3)"
	OverviewTablePrediction string // "Прогноз (Алг.4)"
	OverviewTableDrift      string // "Дрейф (Алг.2)"
	OverviewTableSubject    string // "Субъект"
	OverviewDossier         string // "ДОСЬЕ"

	// -- internal/webpanel/templates/dashboard_profile_detail.html (HTML) --
	ProfileDetailDossier              string // "Досье актора:"
	ProfileDetailReportUpdated        string // "Отчёт обновлён:"
	ProfileDetailBackToOverview       string // "Назад к обзору"
	ProfileDetailTimeCoverage         string // "Временной охват (время = лог)"
	ProfileDetailLastActivity         string // "Последняя активность:"
	ProfileDetailHoldDuration         string // "Длительность удержания:"
	ProfileDetailSessionRhythm        string // "Ритм сессий:"
	ProfileDetailStateConfidence      string // "Состояние и уверенность"
	ProfileDetailTotal                string // "Всего:"
	ProfileDetailMergeRisk            string // "Риск склейки:"
	ProfileDetailBehaviorDisconnects  string // "Поведение и обрывы"
	ProfileDetailBehavior             string // "Поведение:"
	ProfileDetailWhoDisconnects       string // "Кто обрывает:"
	ProfileDetailTimeoutExplanation   string // "TIMEOUT = бот досидел до нашего таймера; FIN/RST = воля бота к сбросу."
	ProfileDetailCurrentPhase         string // "Текущая фаза (Алг.3)"
	ProfileDetailThreatLevel          string // "Уровень угрозы:"
	ProfileDetailPrediction           string // "Прогноз (Алг.4)"
	ProfileDetailProbability          string // "Вероятность:"
	ProfileDetailPhaseDistribution    string // "Распределение фаз (всё время)"
	ProfileDetailStackFingerprint     string // "Сетевой отпечаток (Stack Fingerprint)"
	ProfileDetailStackIncomplete      string // "Сетевой отпечаток неполный или отсутствует."
	ProfileDetailDriftLatency         string // "Дрейф и задержка (Алг.2 + RTT)"
	ProfileDetailPortFromToken        string // "Порт из токена:"
	ProfileDetailRTTAvg               string // "RTT средний:"
	ProfileDetailRTTMin               string // "RTT мин:"
	ProfileDetailRTTMax               string // "RTT макс:"
	ProfileDetailReadProfile          string // "Профиль чтения (L7 Payload)"
	ProfileDetailReadTiny             string // "Tiny (0–64 б):"
	ProfileDetailReadSmall            string // "Small (65–256 б):"
	ProfileDetailReadMedium           string // "Medium (257–1024 б):"
	ProfileDetailReadLarge            string // "Large (>1024 б):"
	ProfileDetailIPRotation           string // "Ротация IP"
	ProfileDetailConcentration        string // "Концентрация:"
	ProfileDetailSubjPerIP            string // "соб/IP"
	ProfileDetailResident             string // "резидент"
	ProfileDetailRotator              string // "ротатор"
	ProfileDetailCountryDominance     string // "Доминирование в странах"
	ProfileDetailTop1Dominance        string // "Доминирование топ-1:"
	ProfileDetailGeoRotation          string // "Гео-ротация:"
	ProfileDetailCountriesProxyMarker string // "стран — маркер прокси/VPN при одном стеке"
	ProfileDetailNoGeoRotation        string // "нет гео-ротации"
	ProfileDetailHoldMetrics          string // "Метрики удержания"
	ProfileDetailAvgPlan              string // "Средний план:"
	ProfileDetailAvgFact              string // "Средний факт:"
	ProfileDetailTotalSessions        string // "Всего сессий:"
	ProfileDetailTotalUtilized        string // "Всего утилизировано:"
	ProfileDetailPlanFactDelta        string // "Дельта план−факт:"
	ProfileDetailIntervalsAnomalies   string // "Интервалы и Сетевые аномалии"
	ProfileDetailAvgIATRhythm         string // "Средний ритм IAT:"
	ProfileDetailIATDispersion        string // "Дисперсия IAT:"
	ProfileDetailRetransPerSession    string // "Retrans / сессию:"
	ProfileDetailProbesPerSession     string // "Probes / сессию:"
	ProfileDetailRetransAccumulated   string // "Retrans (накопл.):"
	ProfileDetailProbesSum            string // "Probes (зонды):"
	ProfileDetailSessionLog           string // "Журнал сессий (последние"
	ProfileDetailTableDateTime        string // "Дата/время (лог)"
	ProfileDetailTableBehavior        string // "Поведение"
	ProfileDetailTablePhase           string // "Фаза"
	ProfileDetailTableClose           string // "Закрытие"
	ProfileDetailTableHold            string // "Удержание"
	ProfileDetailTablePayload         string // "Payload"
	ProfileDetailDailyActivity        string // "Суточная активность (доказательная база, даты = лог)"
	ProfileDetailTableDate            string // "Дата"
	ProfileDetailTableIPTop5Days      string // "IP (топ-5 дней)"
	ProfileDetailEvidence             string // "Доказательства (Evidence)"
	ProfileDetailReasons              string // "Причины и оговорки (Reasons)"
	ProfileDetailNotFound             string // "Профиль не найден"
	ProfileDetailNotFoundDesc         string // "Профиль с указанным идентификатором не существует, был удалён или ещё не создан."

	// -- internal/webpanel/templates/dashboard_profile_detail.html (JavaScript) --
	JSProfileDetailReportUpdated     string // "Отчёт обновлён: "
	JSProfileDetailSubjPerIPResident string // "соб/IP — резидент"
	JSProfileDetailSubjPerIPRotator  string // "соб/IP — ротатор"
	JSProfileDetailCountriesProxy    string // "стран — маркер прокси/VPN при одном стеке"
	JSProfileDetailNoGeoRotation     string // "нет гео-ротации"
	JSProfileDetailFmtDurDays        string // "д"
	JSProfileDetailFmtDurHours       string // "ч"
	JSProfileDetailFmtDurMinutes     string // "м"
	JSProfileDetailFmtDurSeconds     string // "с"
	JSProfileDetailFmtDurZero        string // "0с"

	// -- internal/webpanel/templates/dashboard_reports.html (HTML) --
	ReportsPageTitle             string // "Криминалистические отчёты"
	ReportsPageDesc              string // "Инструмент для принятия обоснованных решений..."
	ReportsRange24h              string // "Сутки (24 ч)"
	ReportsRange7d               string // "Неделя (7 д)"
	ReportsRange30d              string // "Месяц (30 д)"
	ReportsRangeDesc24h          string // "24 часа"
	ReportsRangeDesc7d           string // "7 дней"
	ReportsRangeDesc30d          string // "30 дней"
	ReportsMacroStats            string // "Макро-статистика за период"
	ReportsTotalEvents           string // "Всего событий"
	ReportsValidEvents           string // "Валидных событий:"
	ReportsUtilizedTime          string // "Утилизировано времени"
	ReportsUtilizedTimeSum       string // "Сумма фактического утилизированного времени ботнет в интервале за"
	ReportsUniqueCountriesIPs    string // "Уникальных стран / IP"
	ReportsIPsPerCountry         string // "IP-адресов на страну (прибл.):"
	ReportsAvgDropSecond         string // "Средняя секунда сброса атакующим (FIN/RST). Avg Drop Second"
	ReportsDropSessions          string // "Дроп-сессий:"
	ReportsTacticsDistribution   string // "Распределение тактик"
	ReportsTrapEfficiency        string // "Анализ эффективности ловушек"
	ReportsTrapEfficiencyDesc    string // "Анализ трафика и среднего утилизированного времени..."
	ReportsTableAttacks          string // "Атак"
	ReportsTableUtilized         string // "Утилизировано"
	ReportsTableDrops            string // "Дропов (FIN/RST)"
	ReportsTableAvgHoldPerAttack string // "Ср. удержание за атаку"
	ReportsBlock1Title           string // "Блок 1. Геопрофилирование агрессивной сетевой активности — Топ-"
	ReportsBlock1Suffix          string // "стран"
	ReportsBlock1Desc            string // "Детализация глобальных векторов ботнета..."
	ReportsTableCountry          string // "Страна"
	ReportsTableTopTrap          string // "Топ-ловушка"
	ReportsTableConcentration    string // "Концентрация"
	ReportsBlock2Title           string // "Блок 2. Профилирование агрессивной сетевой активности — Топ-"
	ReportsBlock2Suffix          string // "атакующих"
	ReportsBlock2Desc            string // "Профилирование агрессивного трафика..."
	ReportsTableVisits           string // "Заходов"
	ReportsTableAvgRTT           string // "Ср. RTT"
	ReportsTableDominantClose    string // "Домин. CloseReason"
	ReportsTableIATDispersion    string // "Дисперсия IAT"
	ReportsTableNetworkAnomalies string // "Сетевые аномалии"
	ReportsTableLastActivity     string // "Последняя активность"
	ReportsTop100Note            string // "Топ-100 гарантирует учёт всех активных адресов..."

	// -- internal/webpanel/templates/dashboard_reports.html (JavaScript) --
	JSReportsTacticsDistribution string // "Распределение тактик"

	// -- internal/webpanel/templates/dashboard_siem_core.html (HTML) --
	SIEMPageTitle          string // "Инспектор Анализа Корреляций (SIEM Core)"
	SIEMPageDesc           string // "На основе непрерывного мониторинга поведенческих аномалий..."
	SIEMDetectedPatterns   string // "Обнаруженные ботнет-паттерны (интервал - час)"
	SIEMTablePattern       string // "Паттерн"
	SIEMTableConfidence    string // "Уверенность"
	SIEMTableIPsInvolved   string // "IP вовлечено"
	SIEMTableDetectionTime string // "Время обнаружения"
	SIEMTableEvidence      string // "Доказательства"
	SIEMActiveAlerts       string // "Активные предупреждения SIEM"
	SIEMProxyTunnelTitle   string // "⚠️ Detected Proxy/Tunnel"
	SIEMProxyTunnelDesc    string // "Несоответствие физического пинга (RTT) и прикладной задержки (WriteLatencyMs)."
	SIEMSpoofTitle         string // "⚠️ OS Spoofing / Personality Split"
	SIEMSpoofDesc          string // "Несоответствие физики стека L4 (IoT) прикладной логике L7 (Server)."

	// -- internal/webpanel/templates/dashboard_siem_core.html (JavaScript) --
	JSSIEMDetectionOne  string // "обнаружение" / "detection" (1)
	JSSIEMDetectionFew  string // "обнаружения" / "detections" (2-4)
	JSSIEMDetectionMany string // "обнаружений" / "detections" (5+)

	// -- internal/webpanel/templates/dashboard_tarpit.html (HTML) --
	TarpitPageTitle        string // "Анализ поведения и определение алгоритма Акторов"
	TarpitPageDesc         string // "Исследование метрик динамики удержания..."
	TarpitHoldTitle        string // "Среднее удержание: План vs Факт (сек)"
	TarpitHoldDesc         string // "Сравнение запланированного и фактического времени удержания по часам."
	TarpitCloseReasonTitle string // "Распределение причин закрытия сессий"
	TarpitCloseReasonDesc  string // "FIN (корректное закрытие), RST (сброс), TIMEOUT (таймер AGL), OUR_CLOSE (принудительно)."
	TarpitAggrVsPassTitle  string // "Агрессивность vs Пассивность"
	TarpitAggrVsPassDesc   string // "Соотношение агрессивных и пассивных событий по часам."

	// -- internal/webpanel/templates/dashboard_tarpit.html (JavaScript) --
	JSTarpitAvgPlanSec       string // "Средний план (сек)"
	JSTarpitAvgFactSec       string // "Средний факт (сек)"
	JSTarpitCloseFIN         string // "FIN (корректное)"
	JSTarpitCloseRST         string // "RST (сброс)"
	JSTarpitCloseTIMEOUT     string // "TIMEOUT (таймер AGL)"
	JSTarpitCloseOUR_CLOSE   string // "OUR_CLOSE (принудительно)"
	JSTarpitAggressiveEvents string // "Агрессивные события"
	JSTarpitPassiveEvents    string // "Пассивные события"

	// -- internal/webpanel/templates/dashboard_vitals.html (HTML) --
	VitalsPageTitle               string // "Мониторинг динамики Инцидентов"
	VitalsPageDesc                string // "Комплексная хроника интенсивности, динамики, эскалации, скорости и смещения фаз атак."
	VitalsChronicle60m            string // "Хроника за 60 минут"
	VitalsChronicle60mDesc        string // "Поминутная плотность событий в реальном времени. Всплески указывают на кучность активности."
	VitalsAttackDynamics          string // "Динамика атаки и самодиагностика"
	VitalsAttackDynamicsDesc      string // "Мультислойный таймлайн эскалации фаз: как волна PHASE_1 переходит в PHASE_2 и PHASE_3."
	VitalsPhaseEscalation24h      string // "Эскалация фаз за 24 часа"
	VitalsVelocityIndex           string // "Индекс скорости атаки (средний IAT)"
	VitalsVelocityIndexDesc       string // "Средний интервал между чтениями (мс). Отделяет автоматические скрипты (<5 мс) от имитации человека (>100 мс)."
	VitalsGeoPulse                string // "Метрика географического вовлечения"
	VitalsGeoPulseDesc            string // "Отслеживание динамики и общего количества задействованных стран (глобального охвата) в заданном временном интервале."
	VitalsCountriesPerPeriod      string // "Количество стран за период"
	VitalsGDI                     string // "GDI (Плотность атаки)"
	VitalsGeoChurn                string // "Geo-Churn (Ротация)"
	VitalsGeoDominance            string // "Geo-Dominance"
	VitalsTemporalSignatures      string // "Временные отпечатки (тип инструмента)"
	VitalsTemporalSignaturesDesc  string // "Распределение вердиктов по ритму IAT. TIMER_BOT = фиксированный тайминг, прямой признак автоматического движка."
	VitalsPipelineDiagnostics     string // "Самодиагностика конвейера"
	VitalsDroppedTouchEvents      string // "Упущенные события касания"
	VitalsMaxLatency              string // "Макс. латентность:"
	VitalsDroppedReports          string // "Упущенные отчёты"
	VitalsPipelineDiagnosticsDesc string // "Данные с момента старта процесса. Ненулевые значения указывают на перегрузку каналов или медленный диск."

	// -- internal/webpanel/templates/dashboard_vitals.html (JavaScript) --
	JSVitalsEventsPerMinute          string // "Событий в минуту"
	JSVitalsPhase1BlindRecon         string // "ФАЗА 1: Слепой скан"
	JSVitalsPhase2BannerRecon        string // "ФАЗА 2: Сбор баннеров"
	JSVitalsPhase3PayloadProbe       string // "ФАЗА 3: Payload зонд"
	JSVitalsPhaseUnknown             string // "Не определено"
	JSVitalsAvgIAT                   string // "Средний IAT (мс)"
	JSVitalsThresholdScript          string // "Порог: Скрипт (<5 мс)"
	JSVitalsThresholdHuman           string // "Порог: Человек (>100 мс)"
	JSVitalsTimerBot                 string // "Жёсткий таймер (бот)"
	JSVitalsHumanEmulationOrBadProxy string // "Рваный ритм (человек/прокси)"
	JSVitalsRhythmUnknown            string // "Ритм не определён"
	JSVitalsUniqueCountries          string // "Уникальных стран"
	JSVitalsGDIRegionalRam           string // "Региональный таран (дата-центр)"
	JSVitalsGDIGlobalMimicry         string // "Глобальная мимикрия (распределённый ботнет)"
	JSVitalsGDIModerateDensity       string // "Умеренная плотность"
	JSVitalsGeoChurnHighRotation     string // "Высокая ротация прокси"
	JSVitalsGeoChurnStaticBotnet     string // "Статичный ботнет"
	JSVitalsGeoChurnModerateRotation string // "Умеренная ротация"
	JSVitalsGeoDominates             string // "Доминирует ("
}

// PanelLanguages — словарь переводов для веб-панели.
var PanelLanguages = make(map[string]PanelTranslation)
