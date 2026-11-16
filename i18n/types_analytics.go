// i18n/types_analytics.go
package i18n

// AnalyticsTranslation описывает жесткий каркас всех текстовых строк подсистемы аналитики.
// Отделена от PanelTranslation (webpanel) и UITranslation (TUI), чтобы не смешивать домены.
type AnalyticsTranslation struct {
	// -- internal/analytics/phases.go (Phase Labels) --
	PhaseLabelUnknown      string // "ФАЗА НЕ ОПРЕДЕЛЕНА"
	PhaseLabelBlindRecon   string // "ФАЗА 1: СЛЕПОЙ СКАНИРУЮЩИЙ ШТУРМ (Recon_L4)"
	PhaseLabelBannerRecon  string // "ФАЗА 2: АКТИВНЫЙ СБОР БАННЕРОВ (Recon_L7)"
	PhaseLabelPayloadProbe string // "ФАЗА 3: ЗОНДИРОВАНИЕ PAYLOAD (Попытка брутфорса / внедрения)"
	PhaseLabelSuspect      string // "ФАЗА 3 (ПОДОЗРЕНИЕ): крупная нагрузка без полного подтверждения"

	// -- internal/analytics/phases.go (Evidence strings) --
	EvidenceAllReadsZero           string // "все метрики чтений равны нулю"
	EvidenceBlindProbeTimeout      string // "слепой зонд удержан до TIMEOUT"
	EvidenceShortMediumReads       string // "обнаружены короткие или средние чтения без крупной полезной нагрузки"
	EvidenceReadMediumFormat       string // "ReadMedium=%d: обмен данными среднего размера"
	EvidenceReadTinyFormat         string // "ReadTiny=%d: короткие зонды или баннерные проверки"
	EvidenceCloseFIN               string // "закрытие по FIN: бот контролирует лимит ожидания"
	EvidenceCloseTimeout           string // "закрытие по TIMEOUT: бот оставался в капкане"
	EvidenceReadLargeTimeoutFormat string // "ReadLarge=%d при CloseReason=TIMEOUT"
	EvidenceReadMediumAdditional   string // "ReadMedium=%d: дополнительная полезная нагрузка"
	EvidenceReadTinyPreliminary    string // "ReadTiny=%d: предварительные короткие зонды"
	EvidenceReadLargeFormat        string // "ReadLarge=%d, но CloseReason=%s"

	// -- internal/analytics/phases.go (Reason strings) --
	ReasonReadsInvalid          string // "метрики чтений невалидны; фазовая классификация невозможна"
	ReasonCloseInvalid          string // "причина закрытия невалидна или не распознана"
	ReasonOurClose              string // "сессия закрыта комплексом; поведение бота интерпретируется с оговоркой"
	ReasonBlindProbeRST         string // "слепой зонд закрыт через RST; возможен панический обрыв"
	ReasonBlindProbeFIN         string // "слепой зонд закрыт через FIN; возможно аккуратное закрытие"
	ReasonLargePayloadNoTimeout string // "крупная полезная нагрузка обнаружена, но сессия не удержана до TIMEOUT"

	// -- internal/analytics/identity.go (Evidence & Reasons) --
	IdentityReasonInvalidIP          string // "одно или оба события не имеют валидного IP"
	IdentityReasonSameIP             string // "события имеют одинаковый IP; сравнение разных акторов не требуется"
	IdentityReasonInvalidTime        string // "одно или оба события не имеют валидного времени"
	IdentityReasonIncompleteCurrent  string // "текущее событие не содержит полного сетевого отпечатка"
	IdentityReasonIncompleteArchive  string // "архивное событие не содержит полного сетевого отпечатка"
	IdentityReasonStackMismatch      string // "сетевой отпечаток стека не совпал"
	IdentityEvidenceStackMatchFormat string // "полное совпадение стека: MSS=%d RcvSpace=%d SndWscale=%d RcvWscale=%d"
	IdentityReasonStackMatchNoRTT    string // "стек совпал, но один или оба RTT недоступны"
	IdentityEvidenceCommonProfile    string // "обнаружен распространённый сетевой профиль"
	IdentityEvidenceRTTDeltaFormat   string // "дельта RTT: %d мс"
	IdentityReasonCriticalRTT        string // "стек совпал, дельта RTT очень мала"
	IdentityReasonHighRTT            string // "стек совпал, дельта RTT находится в допустимом высоком окне"
	IdentityReasonMediumRTT          string // "стек совпал, но дельта RTT велика"

	// -- internal/analytics/profile.go (Evidence & Reasons) --
	ProfileReasonStackMismatchEvent     string // "обнаружено событие с несовпадающим сетевым стеком"
	ProfileReasonLinkConfirmed          string // "получено подтверждение связки через идентификацию"
	ProfileReasonHighScoreLowConfidence string // "высокий алгоритмический балл, но недостаточная фактическая уверенность"
	ProfileEvidenceMergedFormat         string // "профиль %s слит в этот профиль"

	// -- internal/analytics/profile.go (Prediction Vectors & Probabilities) --
	ProfilePredictBruteforceVector string // "Целевой брутфорс / Эксплуатация L7-уязвимости"
	ProfilePredictBruteforceProb   string // "90% в течение 30 минут"
	ProfilePredictDDoSVector       string // "Массированный DDoS / Забивание пула сокетов"
	ProfilePredictDDoSProb         string // "85% в течение 1 часа"
	ProfilePredictBackgroundVector string // "Фоновый непрерывный скан периметра"
	ProfilePredictBackgroundProb   string // "Низкая"
}

// AnalyticsLanguages — словарь переводов для подсистемы аналитики.
var AnalyticsLanguages = make(map[string]AnalyticsTranslation)
