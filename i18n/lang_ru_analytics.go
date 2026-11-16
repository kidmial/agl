// i18n/lang_ru_analytics.go
package i18n

func init() {
	AnalyticsLanguages["ru"] = AnalyticsTranslation{
		// -- internal/analytics/phases.go (Phase Labels) --
		PhaseLabelUnknown:      "ФАЗА НЕ ОПРЕДЕЛЕНА",
		PhaseLabelBlindRecon:   "ФАЗА 1: СЛЕПОЙ СКАНИРУЮЩИЙ ШТУРМ (Recon_L4)",
		PhaseLabelBannerRecon:  "ФАЗА 2: АКТИВНЫЙ СБОР БАННЕРОВ (Recon_L7)",
		PhaseLabelPayloadProbe: "ФАЗА 3: ЗОНДИРОВАНИЕ PAYLOAD (Попытка брутфорса / внедрения)",
		PhaseLabelSuspect:      "ФАЗА 3 (ПОДОЗРЕНИЕ): крупная нагрузка без полного подтверждения",

		// -- internal/analytics/phases.go (Evidence strings) --
		EvidenceAllReadsZero:           "все метрики чтений равны нулю",
		EvidenceBlindProbeTimeout:      "слепой зонд удержан до TIMEOUT",
		EvidenceShortMediumReads:       "обнаружены короткие или средние чтения без крупной полезной нагрузки",
		EvidenceReadMediumFormat:       "ReadMedium=%d: обмен данными среднего размера",
		EvidenceReadTinyFormat:         "ReadTiny=%d: короткие зонды или баннерные проверки",
		EvidenceCloseFIN:               "закрытие по FIN: бот контролирует лимит ожидания",
		EvidenceCloseTimeout:           "закрытие по TIMEOUT: бот оставался в капкане",
		EvidenceReadLargeTimeoutFormat: "ReadLarge=%d при CloseReason=TIMEOUT",
		EvidenceReadMediumAdditional:   "ReadMedium=%d: дополнительная полезная нагрузка",
		EvidenceReadTinyPreliminary:    "ReadTiny=%d: предварительные короткие зонды",
		EvidenceReadLargeFormat:        "ReadLarge=%d, но CloseReason=%s",

		// -- internal/analytics/phases.go (Reason strings) --
		ReasonReadsInvalid:          "метрики чтений невалидны; фазовая классификация невозможна",
		ReasonCloseInvalid:          "причина закрытия невалидна или не распознана",
		ReasonOurClose:              "сессия закрыта комплексом; поведение бота интерпретируется с оговоркой",
		ReasonBlindProbeRST:         "слепой зонд закрыт через RST; возможен панический обрыв",
		ReasonBlindProbeFIN:         "слепой зонд закрыт через FIN; возможно аккуратное закрытие",
		ReasonLargePayloadNoTimeout: "крупная полезная нагрузка обнаружена, но сессия не удержана до TIMEOUT",

		// -- internal/analytics/identity.go (Evidence & Reasons) --
		IdentityReasonInvalidIP:          "одно или оба события не имеют валидного IP",
		IdentityReasonSameIP:             "события имеют одинаковый IP; сравнение разных акторов не требуется",
		IdentityReasonInvalidTime:        "одно или оба события не имеют валидного времени",
		IdentityReasonIncompleteCurrent:  "текущее событие не содержит полного сетевого отпечатка",
		IdentityReasonIncompleteArchive:  "архивное событие не содержит полного сетевого отпечатка",
		IdentityReasonStackMismatch:      "сетевой отпечаток стека не совпал",
		IdentityEvidenceStackMatchFormat: "полное совпадение стека: MSS=%d RcvSpace=%d SndWscale=%d RcvWscale=%d",
		IdentityReasonStackMatchNoRTT:    "стек совпал, но один или оба RTT недоступны",
		IdentityEvidenceCommonProfile:    "обнаружен распространённый сетевой профиль",
		IdentityEvidenceRTTDeltaFormat:   "дельта RTT: %d мс",
		IdentityReasonCriticalRTT:        "стек совпал, дельта RTT очень мала",
		IdentityReasonHighRTT:            "стек совпал, дельта RTT находится в допустимом высоком окне",
		IdentityReasonMediumRTT:          "стек совпал, но дельта RTT велика",

		// -- internal/analytics/profile.go (Evidence & Reasons) --
		ProfileReasonStackMismatchEvent:     "обнаружено событие с несовпадающим сетевым стеком",
		ProfileReasonLinkConfirmed:          "получено подтверждение связки через идентификацию",
		ProfileReasonHighScoreLowConfidence: "высокий алгоритмический балл, но недостаточная фактическая уверенность",
		ProfileEvidenceMergedFormat:         "профиль %s слит в этот профиль",

		// -- internal/analytics/profile.go (Prediction Vectors & Probabilities) --
		ProfilePredictBruteforceVector: "Целевой брутфорс / Эксплуатация L7-уязвимости",
		ProfilePredictBruteforceProb:   "90% в течение 30 минут",
		ProfilePredictDDoSVector:       "Массированный DDoS / Забивание пула сокетов",
		ProfilePredictDDoSProb:         "85% в течение 1 часа",
		ProfilePredictBackgroundVector: "Фоновый непрерывный скан периметра",
		ProfilePredictBackgroundProb:   "Низкая",
	}
}
