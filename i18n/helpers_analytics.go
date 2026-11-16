package i18n

// GetAnalyticsTranslation возвращает перевод для подсистемы аналитики.
// Если передана пустая строка или язык не найден, делает fallback на английский (en).
func GetAnalyticsTranslation(lang string) AnalyticsTranslation {
	if t, ok := AnalyticsLanguages[lang]; ok {
		return t
	}
	return AnalyticsLanguages["en"]
}
