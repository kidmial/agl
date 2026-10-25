// internal/webpanel/ui_panel.go
// =====================================================================
// КОМПОНЕНТ: РЕНДЕРИНГ ВЕБ-ПАНЕЛИ АНАЛИТИКИ
// РОЛЬ: Генерация HTML-дашбордов для отображения статистики и профилей.
// ПРИНЦИП:
//   - использование //go:embed для встраивания шаблонов в бинарник;
//   - безопасный вывод данных через html/template;
//   - поддержка витринной таймзоны из конфига;
//   - универсальная функция renderDashboard для всех страниц.
//
// =====================================================================
package webpanel

import (
	"embed"
	"encoding/json"
	"html/template"
	"net/http"
	"time"

	"agl/config"
	"agl/internal/analytics"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed assets
var assetsFS embed.FS

// toJSON маршалит значение в JSON для встраивания в <script>-блоки.
//
// Возвращает template.JS, чтобы html/template трактовал результат
// как доверенный JavaScript в script-контексте и не переэкранировал его.
// При ошибке маршалинга возвращаем null — график покажет пустые данные,
// но страница не умрёт.
func toJSON(v interface{}) template.JS {
	b, err := json.Marshal(v)
	if err != nil {
		return template.JS("null")
	}
	return template.JS(b)
}

// panelTmpl — скомпилированные шаблоны, загруженные из встроенных файлов.
// Парсим все HTML-файлы вместе, чтобы блоки header/footer были доступны всем дашбордам.
// Funcs регистрирует кастомные функции для шаблонов.
var panelTmpl = template.Must(
	template.New("agl").
		Funcs(template.FuncMap{
			"json": toJSON,
		}).
		ParseFS(templateFS, "templates/*.html"),
)

// renderDashboard рендерит любой дашборд с полной поддержкой данных и таймзоны.
//
// Аргументы:
//   - w: HTTP-ответ
//   - templateName: имя шаблона (dashboard_overview, dashboard_vitals, и т.д.)
//   - prefix: крипто-префикс для навигации
//   - counters, profileCounters: счётчики хранилища и профилей
//   - hourStats: часовые срезы
//   - profiles: профили акторов
//
// PipelineHealth описывает здоровье конвейера логирования.
// Используется для самодиагностики в веб-панели.
type PipelineHealth struct {
	DroppedConnectionEvents uint64 `json:"dropped_connection_events"`
	DroppedReportEntries    uint64 `json:"dropped_report_entries"`
	MaxConnWriteLatencyMs   uint64 `json:"max_conn_write_latency_ms"`
	MaxReportWriteLatencyMs uint64 `json:"max_report_write_latency_ms"`
}

func renderDashboard(w http.ResponseWriter, templateName string, prefix string, counters analytics.StoreCounters, profileCounters analytics.ProfileManagerCounters, hourStats []analytics.HourStat, profiles []analytics.ActorProfile, health PipelineHealth) {
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
					timezoneLabel = timezoneLabel[:10]
				}
			}
		}
	}

	// 2. Конвертируем все time.Time в витринную зону.
	convertedHourStats := make([]analytics.HourStat, len(hourStats))
	for i, hs := range hourStats {
		convertedHourStats[i] = hs
		convertedHourStats[i].Hour = hs.Hour.In(loc)
	}

	convertedProfiles := make([]analytics.ActorProfile, len(profiles))
	for i, p := range profiles {
		convertedProfiles[i] = p
		convertedProfiles[i].LastSeen = p.LastSeen.In(loc)
		if !p.LastEventTime.IsZero() {
			convertedProfiles[i].LastEventTime = p.LastEventTime.In(loc)
		}
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
		"Prefix":          prefix,
		"StoreCounters":   convertedCounters,
		"ProfileCounters": profileCounters,
		"HourStats":       convertedHourStats,
		"Profiles":        convertedProfiles,
		"TimezoneLabel":   timezoneLabel,
		"CurrentTime":     time.Now().In(loc).Format("2006-01-02 15:04:05 MST"),
		"HistoryLoaded":   analytics.IsHistoryLoaded(),
		"StoreTotal":      counters.TotalEvents,
		"PipelineHealth":  health,
	}

	if err := panelTmpl.ExecuteTemplate(w, templateName, data); err != nil {
		http.Error(w, "Template execution error", http.StatusInternalServerError)
	}
}

// renderStub рендерит страницу-заглушку (без данных аналитики).
// Используется для дашбордов в разработке.
func renderStub(w http.ResponseWriter, templateName string, prefix string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	data := map[string]interface{}{
		"Prefix": prefix,
	}

	if err := panelTmpl.ExecuteTemplate(w, templateName, data); err != nil {
		http.Error(w, "Template execution error", http.StatusInternalServerError)
	}
}

// renderDashboardSIEM рендерит страницу SIEM Core с данными детекций ботнет-паттернов.
func renderDashboardSIEM(w http.ResponseWriter, templateName string, prefix string, counters analytics.StoreCounters, profileCounters analytics.ProfileManagerCounters, hourStats []analytics.HourStat, profiles []analytics.ActorProfile, health PipelineHealth, detections []analytics.BotnetDetection, burst []analytics.BurstPoint, proxyCount int, spoofCount int) {
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
					timezoneLabel = timezoneLabel[:10]
				}
			}
		}
	}

	// 2. Конвертируем все time.Time в витринную зону.
	convertedHourStats := make([]analytics.HourStat, len(hourStats))
	for i, hs := range hourStats {
		convertedHourStats[i] = hs
		convertedHourStats[i].Hour = hs.Hour.In(loc)
	}

	convertedProfiles := make([]analytics.ActorProfile, len(profiles))
	for i, p := range profiles {
		convertedProfiles[i] = p
		convertedProfiles[i].LastSeen = p.LastSeen.In(loc)
		if !p.LastEventTime.IsZero() {
			convertedProfiles[i].LastEventTime = p.LastEventTime.In(loc)
		}
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
		"Prefix":          prefix,
		"StoreCounters":   convertedCounters,
		"ProfileCounters": profileCounters,
		"HourStats":       convertedHourStats,
		"Profiles":        convertedProfiles,
		"TimezoneLabel":   timezoneLabel,
		"CurrentTime":     time.Now().In(loc).Format("2006-01-02 15:04:05 MST"),
		"HistoryLoaded":   analytics.IsHistoryLoaded(),
		"StoreTotal":      counters.TotalEvents,
		"PipelineHealth":  health,
		"Detections":      detections,
		"BurstSeries":     burst,
		"ProxyCount":      proxyCount,
		"SpoofCount":      spoofCount,
	}

	if err := panelTmpl.ExecuteTemplate(w, templateName, data); err != nil {
		http.Error(w, "Template execution error", http.StatusInternalServerError)
	}
}
