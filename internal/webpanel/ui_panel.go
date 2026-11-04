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
	"strings"
	"time"

	"agl/config"
	"agl/internal/analytics"
	"agl/internal/buildinfo"
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
			"json":           toJSON,
			"formatDuration": analytics.FormatDuration,
			"add":            func(a, b int) int { return a + b },
			"divf": func(a, b interface{}) float64 {
				bv := toTemplateFloat(b)
				if bv == 0 {
					return 0
				}
				return toTemplateFloat(a) / bv
			},
			// floorDiv — целочисленное деление с округлением вниз.
			// Используется для консервативных метрик (например, "IP на страну"),
			// где дробная часть не имеет оперативной ценности.
			// 20.9 → 20, 20.1 → 20. Никогда не завышаем.
			"floorDiv": func(a, b interface{}) int64 {
				av := toTemplateFloat(a)
				bv := toTemplateFloat(b)
				if bv == 0 {
					return 0
				}
				return int64(av / bv) // Для положительных чисел целочисленное деление = округление вниз
			},
		}).
		ParseFS(templateFS, "templates/*.html"),
)

// toTemplateFloat приводит числовые типы шаблонов (int, int64, uint64, float64)
// к float64. Защищает шаблонные функции от падений на смешанных типах полей.
func toTemplateFloat(v interface{}) float64 {
	switch n := v.(type) {
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case uint64:
		return float64(n)
	case float64:
		return n
	default:
		return 0
	}
}

// profileViewDTO — облегчённая структура профиля для JSON-API.
// Методы ActorProfile (TopIPs, UniqueIPCount и т.д.) не маршалятся в JSON,
// поэтому мы вычисляем их здесь один раз при запросе.
type profileViewDTO struct {
	LastEventTime    string  `json:"last_event_time"`
	PrimaryIP        string  `json:"primary_ip"`
	EventCount       int     `json:"event_count"`
	AvgPlannedHold   float64 `json:"avg_planned_hold"`
	AvgActualHold    float64 `json:"avg_actual_hold"`
	DominantBehavior string  `json:"dominant_behavior"`
	ID               string  `json:"id"`
	UniqueIPCount    int     `json:"unique_ip_count"`
	CurrentPhase     string  `json:"current_phase"`
	PredictedVector  string  `json:"predicted_vector"`
	TokenDriftMs     int64   `json:"token_drift_ms"`
	TokenDriftValid  bool    `json:"token_drift_valid"`
	HoldCount        uint64  `json:"hold_count"`
}

func getProfilesJSON(manager *analytics.ProfileManager, limit int) []profileViewDTO {
	if manager == nil {
		return nil
	}
	if limit <= 0 {
		limit = 20
	}
	profiles := manager.ProfilesSnapshot(limit)
	res := make([]profileViewDTO, len(profiles))

	// 1. Определяем витринную локацию (точно так же, как в renderDashboard)
	loc := time.Local
	if tz := config.C.WebPanelTimezone; tz != "" {
		if tz == "UTC" {
			loc = time.UTC
		} else if tz != "local" {
			if loaded, err := time.LoadLocation(tz); err == nil {
				loc = loaded
			}
		}
	}

	for i, p := range profiles {
		// Находим Primary IP
		primaryIP := ""
		maxCount := 0
		for ip, count := range p.IPCounts {
			if count > maxCount {
				maxCount = count
				primaryIP = ip
			}
		}

		// Считаем средние значения
		avgPlanned, avgActual := 0.0, 0.0
		if p.HoldCount > 0 {
			avgPlanned = float64(p.PlannedHoldSumSec) / float64(p.HoldCount)
			avgActual = float64(p.ActualHoldSumSec) / float64(p.HoldCount)
		}

		// Определяем поведение
		behavior := "UNK"
		aggr := p.BehaviorCounts["AGGRESSIVE"]
		pass := p.BehaviorCounts["PASSIVE"]
		if aggr > 0 && pass > 0 {
			behavior = "MIXED"
		} else if aggr > 0 {
			behavior = "AGGR"
		} else if pass > 0 {
			behavior = "PASS"
		}

		// 2. Форматируем время с учётом витринной локации!
		timeStr := "—"
		if !p.LastEventTime.IsZero() {
			timeStr = p.LastEventTime.In(loc).Format("15:04:05")
		}

		res[i] = profileViewDTO{
			LastEventTime:    timeStr,
			PrimaryIP:        primaryIP,
			EventCount:       p.EventCount,
			AvgPlannedHold:   avgPlanned,
			AvgActualHold:    avgActual,
			DominantBehavior: behavior,
			ID:               p.ID,
			UniqueIPCount:    len(p.IPCounts),
			CurrentPhase:     p.CurrentPhase,
			PredictedVector:  p.PredictedVector,
			TokenDriftMs:     p.TokenDriftMs,
			TokenDriftValid:  p.TokenDriftValid,
			HoldCount:        p.HoldCount,
		}
	}
	return res
}

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

func renderDashboard(w http.ResponseWriter, templateName string, prefix string, counters analytics.StoreCounters, profileCounters analytics.ProfileManagerCounters, hourStats []analytics.HourStat, profiles []analytics.ActorProfile, health PipelineHealth, stolenTime analytics.StolenTimeStats) {
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

	// Создаем копию слайса в обратном порядке СПЕЦИАЛЬНО для таблицы.
	// Оригинальный convertedHourStats остается хронологическим для графиков.
	hourStatsDesc := make([]analytics.HourStat, len(convertedHourStats))
	for i, hs := range convertedHourStats {
		hourStatsDesc[len(convertedHourStats)-1-i] = hs
	}

	// Определяем текущую страницу для подсветки меню
	var currentPagePath string
	switch templateName {
	case "dashboard_overview":
		currentPagePath = "dashboard/overview"
	case "dashboard_fingerprints":
		currentPagePath = "dashboard/fingerprints"
	case "dashboard_tarpit":
		currentPagePath = "dashboard/tarpit"
	}

	// 3. Формируем данные для шаблона.
	data := map[string]interface{}{
		"Prefix":          prefix,
		"CurrentPagePath": currentPagePath,
		"ProfileTotal":    profileCounters.Profiles,
		"StoreCounters":   convertedCounters,
		"ProfileCounters": profileCounters,
		"HourStats":       convertedHourStats,
		"HourStatsDesc":   hourStatsDesc, // Для таблицы События за 24 часа (свежие сверху)
		"Profiles":        convertedProfiles,
		"TimezoneLabel":   timezoneLabel,
		"CurrentTime":     time.Now().In(loc).Format("2006-01-02 15:04:05 MST"),
		"HistoryLoaded":   analytics.IsHistoryLoaded(),
		"StoreTotal":      counters.TotalEvents,
		"PipelineHealth":  health,
		"StolenTime":      stolenTime,
		"Version":         buildinfo.Version,
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
		"Prefix":       prefix,
		"ProfileTotal": analytics.GlobalManager.Counters().Profiles,
	}

	if err := panelTmpl.ExecuteTemplate(w, templateName, data); err != nil {
		http.Error(w, "Template execution error", http.StatusInternalServerError)
	}
}

// renderDashboardSIEM рендерит страницу SIEM Core с данными детекций ботнет-паттернов.
func renderDashboardSIEM(w http.ResponseWriter, templateName string, prefix string, counters analytics.StoreCounters, profileCounters analytics.ProfileManagerCounters, hourStats []analytics.HourStat, profiles []analytics.ActorProfile, health PipelineHealth, detections []analytics.BotnetDetection, proxyDets []analytics.BotnetDetection, spoofDets []analytics.BotnetDetection) {
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
		"CurrentPagePath": "dashboard/siem",
		"ProfileTotal":    profileCounters.Profiles,
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
		"ProxyCount":      len(proxyDets),
		"SpoofCount":      len(spoofDets),
		"ProxyDetails":    buildWarningLines(proxyDets, 5),
		"SpoofDetails":    buildWarningLines(spoofDets, 5),
		"Version":         buildinfo.Version,
	}

	if err := panelTmpl.ExecuteTemplate(w, templateName, data); err != nil {
		http.Error(w, "Template execution error", http.StatusInternalServerError)
	}
}

// renderDashboardVitals рендерит страницу Динамики с дополнительными данными живого пульса активности атаки.
func renderDashboardVitals(w http.ResponseWriter, templateName string, prefix string, counters analytics.StoreCounters, profileCounters analytics.ProfileManagerCounters, hourStats []analytics.HourStat, profiles []analytics.ActorProfile, health PipelineHealth, stolenTime analytics.StolenTimeStats, burst []analytics.BurstPoint) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	loc := time.Local
	timezoneLabel := "Local"
	if tz := config.C.WebPanelTimezone; tz != "" {
		if tz == "local" {
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

	data := map[string]interface{}{
		"Prefix":          prefix,
		"CurrentPagePath": "dashboard/vitals",
		"ProfileTotal":    profileCounters.Profiles,
		"StoreCounters":   convertedCounters,
		"ProfileCounters": profileCounters,
		"HourStats":       convertedHourStats,
		"Profiles":        convertedProfiles,
		"TimezoneLabel":   timezoneLabel,
		"CurrentTime":     time.Now().In(loc).Format("2006-01-02 15:04:05 MST"),
		"HistoryLoaded":   analytics.IsHistoryLoaded(),
		"StoreTotal":      counters.TotalEvents,
		"PipelineHealth":  health,
		"StolenTime":      stolenTime,
		"BurstSeries":     burst,
		"Version":         buildinfo.Version,
		"TimeWindows":     analytics.TimeWindows, // v8.6.2: для GeoPulse dropdown
	}

	if err := panelTmpl.ExecuteTemplate(w, templateName, data); err != nil {
		http.Error(w, "Template execution error", http.StatusInternalServerError)
	}
}

// renderDashboardReports рендерит страницу отчётов.
func renderDashboardReports(w http.ResponseWriter, templateName string, prefix string, rng string, countries []analytics.CountryReport, ips []analytics.IPReport, macro analytics.MacroReport, traps []analytics.TrapReport) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := map[string]interface{}{
		"Prefix":          prefix,
		"CurrentPagePath": "dashboard/reports",
		"ProfileTotal":    analytics.GlobalManager.Counters().Profiles,
		"Range":           rng,
		"Countries":       countries,
		"IPs":             ips,
		"Macro":           macro,
		"Traps":           traps,
		"Version":         buildinfo.Version,
	}
	if err := panelTmpl.ExecuteTemplate(w, templateName, data); err != nil {
		http.Error(w, "Template execution error", http.StatusInternalServerError)
	}
}

// warningLine — одна строка фактической нагрузки предупреждения:
// субъект (адрес со страной) и основание (доказательства детекции).
type warningLine struct {
	Subject string `json:"subject"`
	Facts   string `json:"facts"`
}

// buildWarningLines собирает факты для карточки предупреждения.
// Принцип: предупреждение без субъекта и основания — шум.
// limit ограничивает число строк, чтобы карточка не раздувалась при массовых детекциях.
func buildWarningLines(dets []analytics.BotnetDetection, limit int) []warningLine {
	var out []warningLine
	for _, d := range dets {
		for _, ip := range d.IPsInvolved {
			if len(out) >= limit {
				return out
			}
			subj := ip
			if c, ok := d.CountryByIP[ip]; ok && c != "" {
				subj += " (" + c + ")"
			}
			out = append(out, warningLine{
				Subject: subj,
				Facts:   strings.Join(d.Evidence, "; "),
			})
		}
	}
	return out
}
