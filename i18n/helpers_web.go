// i18n/helpers_web.go
package i18n

// GetPanelTranslation возвращает перевод для веб-панели.
// Если передан пустой строка или язык не найден, делает fallback на английский (en).
func GetPanelTranslation(lang string) PanelTranslation {
	if t, ok := PanelLanguages[lang]; ok {
		return t
	}
	return PanelLanguages["en"]
}
