// internal/webpanel/ui_panel.go
// =====================================================================
// КОМПОНЕНТ: РЕНДЕРИНГ ВЕБ-ПАНЕЛИ АНАЛИТИКИ
// РОЛЬ: Генерация HTML-дашборда для отображения статистики и профилей.
// ПРИНЦИП:
//   - использование //go:embed для встраивания шаблонов в бинарник;
//   - безопасный вывод данных через html/template;
//   - поддержка витринной таймзоны из конфига.
//
// =====================================================================
package webpanel

import (
	"embed"
	"html/template"
	"net/http"
	"time"

	"agl/config"
	"agl/internal/analytics"
)

//go:embed templates/*.html
var templateFS embed.FS

// panelTmpl — скомпилированный шаблон, загруженный из встроенных файлов.
// Must гарантирует, что шаблон синтезируется на старте, а не во время запроса.
var panelTmpl = template.Must(template.ParseFS(templateFS, "templates/panel.html"))

// renderPanelHTML отдаёт отрендеренный HTML дашборда.
//
// Аргументы:
//   - counters, profileCounters: счётчики хранилища и профилей
//   - hourStats: часовые срезы (время внутри в UTC)
//   - profiles: профили акторов (время внутри в UTC)
func renderPanelHTML(w http.ResponseWriter, counters analytics.StoreCounters, profileCounters analytics.ProfileManagerCounters, hourStats []analytics.HourStat, profiles []analytics.ActorProfile) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// 1. Определяем витринную локацию для отображения времени.
	loc := time.Local
	timezoneLabel := "Local"
	if tz := config.C.WebPanelTimezone; tz != "" {
		if tz == "local" {
			// Уже time.Local
		} else if tz == "UTC" {
			loc = time.UTC
			timezoneLabel = "UTC"
		} else {
			if loaded, err := time.LoadLocation(tz); err == nil {
				loc = loaded
				timezoneLabel = loaded.String()
				if len(timezoneLabel) > 10 {
					timezoneLabel = timezoneLabel[:10] // Краткое имя для UI
				}
			}
		}
	}

	// 2. Конвертируем все time.Time в витринную зону перед передачей в шаблон.
	convertedHourStats := make([]analytics.HourStat, len(hourStats))
	for i, hs := range hourStats {
		convertedHourStats[i] = hs
		convertedHourStats[i].Hour = hs.Hour.In(loc)
	}

	convertedProfiles := make([]analytics.ActorProfile, len(profiles))
	for i, p := range profiles {
		convertedProfiles[i] = p
		convertedProfiles[i].LastSeen = p.LastSeen.In(loc)
	}

	convertedCounters := counters
	if !convertedCounters.FirstEventTime.IsZero() {
		convertedCounters.FirstEventTime = counters.FirstEventTime.In(loc)
	}
	if !convertedCounters.LastEventTime.IsZero() {
		convertedCounters.LastEventTime = counters.LastEventTime.In(loc)
	}

	// 3. Формируем данные для шаблона.
	data := map[string]interface{}{
		"StoreCounters":   convertedCounters,
		"ProfileCounters": profileCounters,
		"HourStats":       convertedHourStats,
		"Profiles":        convertedProfiles,
		"TimezoneLabel":   timezoneLabel,
		"CurrentTime":     time.Now().In(loc).Format("2006-01-02 15:04:05 MST"),

		// Критично для кнопки rebuild:
		"HistoryLoaded": analytics.IsHistoryLoaded(),
		"StoreTotal":    counters.TotalEvents,
	}

	if err := panelTmpl.Execute(w, data); err != nil {
		http.Error(w, "Template execution error", http.StatusInternalServerError)
	}
}
