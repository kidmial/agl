// i18n/lang_en_analytics.go
package i18n

func init() {
	AnalyticsLanguages["en"] = AnalyticsTranslation{
		// -- internal/analytics/phases.go (Phase Labels) --
		PhaseLabelUnknown:      "PHASE UNKNOWN",
		PhaseLabelBlindRecon:   "PHASE 1: BLIND SCANNING ASSAULT (Recon_L4)",
		PhaseLabelBannerRecon:  "PHASE 2: ACTIVE BANNER COLLECTION (Recon_L7)",
		PhaseLabelPayloadProbe: "PHASE 3: PAYLOAD PROBING (Bruteforce / injection attempt)",
		PhaseLabelSuspect:      "PHASE 3 (SUSPECTED): large payload without full confirmation",

		// -- internal/analytics/phases.go (Evidence strings) --
		EvidenceAllReadsZero:           "all read metrics are zero",
		EvidenceBlindProbeTimeout:      "blind probe held until TIMEOUT",
		EvidenceShortMediumReads:       "short or medium reads detected without large payload",
		EvidenceReadMediumFormat:       "ReadMedium=%d: medium-size data exchange",
		EvidenceReadTinyFormat:         "ReadTiny=%d: short probes or banner checks",
		EvidenceCloseFIN:               "FIN close: bot controls wait timeout",
		EvidenceCloseTimeout:           "TIMEOUT close: bot remained in trap",
		EvidenceReadLargeTimeoutFormat: "ReadLarge=%d with CloseReason=TIMEOUT",
		EvidenceReadMediumAdditional:   "ReadMedium=%d: additional payload",
		EvidenceReadTinyPreliminary:    "ReadTiny=%d: preliminary short probes",
		EvidenceReadLargeFormat:        "ReadLarge=%d, but CloseReason=%s",

		// -- internal/analytics/phases.go (Reason strings) --
		ReasonReadsInvalid:          "read metrics invalid; phase classification impossible",
		ReasonCloseInvalid:          "close reason invalid or unrecognized",
		ReasonOurClose:              "session closed by complex; bot behavior interpreted with caveat",
		ReasonBlindProbeRST:         "blind probe closed via RST; possible panic disconnect",
		ReasonBlindProbeFIN:         "blind probe closed via FIN; possible graceful close",
		ReasonLargePayloadNoTimeout: "large payload detected, but session not held until TIMEOUT",

		// -- internal/analytics/identity.go (Evidence & Reasons) --
		IdentityReasonInvalidIP:          "one or both events lack a valid IP",
		IdentityReasonSameIP:             "events share the same IP; cross-actor comparison not required",
		IdentityReasonInvalidTime:        "one or both events lack a valid timestamp",
		IdentityReasonIncompleteCurrent:  "current event lacks a complete network fingerprint",
		IdentityReasonIncompleteArchive:  "archived event lacks a complete network fingerprint",
		IdentityReasonStackMismatch:      "network stack fingerprint does not match",
		IdentityEvidenceStackMatchFormat: "exact stack match: MSS=%d RcvSpace=%d SndWscale=%d RcvWscale=%d",
		IdentityReasonStackMatchNoRTT:    "stack matched, but one or both RTTs are unavailable",
		IdentityEvidenceCommonProfile:    "common network profile detected",
		IdentityEvidenceRTTDeltaFormat:   "RTT delta: %d ms",
		IdentityReasonCriticalRTT:        "stack matched, RTT delta is very small",
		IdentityReasonHighRTT:            "stack matched, RTT delta is within acceptable high window",
		IdentityReasonMediumRTT:          "stack matched, but RTT delta is large",

		// -- internal/analytics/profile.go (Evidence & Reasons) --
		ProfileReasonStackMismatchEvent:     "event with mismatching network stack detected",
		ProfileReasonLinkConfirmed:          "link confirmation received via identification",
		ProfileReasonHighScoreLowConfidence: "high algorithmic score, but insufficient actual confidence",
		ProfileEvidenceMergedFormat:         "profile %s merged into this profile",

		// -- internal/analytics/profile.go (Prediction Vectors & Probabilities) --
		ProfilePredictBruteforceVector: "Targeted bruteforce / L7 vulnerability exploitation",
		ProfilePredictBruteforceProb:   "90% within 30 minutes",
		ProfilePredictDDoSVector:       "Massive DDoS / Socket pool exhaustion",
		ProfilePredictDDoSProb:         "85% within 1 hour",
		ProfilePredictBackgroundVector: "Background continuous perimeter scan",
		ProfilePredictBackgroundProb:   "Low",
	}
}
