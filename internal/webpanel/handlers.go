// internal/webpanel/handlers.go
// =====================================================================
// КОМПОНЕНТ: МОНТИРУЕМЫЕ ОБРАБОТЧИКИ ВЕБ-ПАНЕЛИ
// РОЛЬ: Регистрация маршрутов аналитики в существующий HTTP-сервер.
// ПРИНЦИП:
//   - не создаёт новых слушателей;
//   - уважает доктрину скрытности: если панель выключена, отдаём 404;
//   - зависит только от переданных экземпляров Store и ProfileManager.
//
// =====================================================================
package webpanel

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"agl/config"
	"agl/internal/analytics"
	"agl/internal/buildinfo"
	"agl/internal/tarpit"
)

// geoGranularityFor возвращает шаг точек графика для заданной глубины истории.
// Принцип: X-ось покрывает весь выбранный интервал, точки — бакеты естественной
// гранулярности (минутные до 3 часов, часовые для суток, суточные для недель/месяца).
func geoGranularityFor(window analytics.TimeWindow) analytics.TimeWindow {
	switch window.Name {
	case "3h":
		return analytics.TimeWindow{Name: "5min", Duration: 5 * time.Minute}
	case "24h":
		return analytics.TimeWindow{Name: "1h", Duration: time.Hour}
	case "7d", "30d":
		return analytics.TimeWindow{Name: "1d", Duration: 24 * time.Hour}
	default:
		// 1min..1h: поминутные точки (кардиограмма паука в лучшем виде)
		return analytics.TimeWindow{Name: "1min", Duration: time.Minute}
	}
}

// panel404Stub отдаёт стилизованную 404-страницу, когда панель недоступна.
//
// ЛОГИКА:
//   - Если WebHttpsPanelActive == false -> показываем 404 (панель скрыта).
//   - Дизайн скопирован из armor.handle404Stub для визуальной консистентности.
//   - Это локальная копия, так как прямой вызов armor.handle404Stub невозможен
//     из-за циклического импорта (armor импортирует webpanel для RegisterRoutes).
//
// КОНТРАКТ:
//   - HTTP-статус 404 Not Found
//   - Content-Type: text/html; charset=utf-8
//   - Единый стиль с основной 404 комплекса
func panel404Stub(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(`<!DOCTYPE html>
<html lang="ru">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>404</title>
    <style>
        body {
            margin: 0;
            padding: 0;
            background-color: #0f141c;
            color: #ffffff;
            font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Ubuntu, sans-serif;
            display: flex;
            justify-content: center;
            align-items: center;
            height: 100vh;
            text-align: center;
        }
        .error-container {
            background-color: #1c2030;
            padding: 40px 60px;
            border-radius: 8px;
            box-shadow: 0 4px 20px rgba(0,0,0,0.5);
            border: 1px solid #2a2e39;
            max-width: 500px;
        }
        .error-code {
            font-size: 72px;
            font-weight: bold;
            color: #f23645;
            margin: 0;
            line-height: 1;
        }
        .error-title {
            font-size: 18px;
            font-weight: 500;
            text-transform: uppercase;
            letter-spacing: 1px;
            margin: 15px 0 25px 0;
            color: #b2b5be;
            border-bottom: 1px solid #2a2e39;
            padding-bottom: 15px;
        }
        .error-message {
            font-size: 14px;
            line-height: 1.6;
            color: #d1d4dc;
            margin-bottom: 30px;
        }
        .system-status {
            margin-top: 25px;
            font-size: 11px;
            color: #787b86;
        }
    </style>
</head>
<body>
    <div class="error-container">
        <div class="error-code">404</div>
        <div class="error-title">Resource Not Found</div>
        <div class="error-message">The requested resource does not exist on this server.</div>
        <div class="system-status">System Status: Operational</div>
    </div>
</body>
</html>`))
}

// RegisterRoutes монтирует все маршруты панели аналитики в переданный mux.
func RegisterRoutes(mux *http.ServeMux, store *analytics.Store, manager *analytics.ProfileManager, prefix string) {

	// 1. Главная страница: Обзор (HTML)
	mux.HandleFunc(prefix+"dashboard/overview", func(w http.ResponseWriter, r *http.Request) {
		if !config.C.WebHttpsPanelActive {
			panel404Stub(w, r)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		counters := store.Counters()
		profileCounters := manager.Counters()
		hourStats := analytics.BuildLatestHourStatsFromBuckets(analytics.GlobalRollup, 24, 10)
		profiles := manager.ProfilesSnapshot(20)

		// Получаем метрики здоровья конвейера из tarpit
		droppedConn, droppedReport, maxConnLatency, maxReportLatency := tarpit.GetLogHealth()
		health := PipelineHealth{
			DroppedConnectionEvents: droppedConn,
			DroppedReportEntries:    droppedReport,
			MaxConnWriteLatencyMs:   maxConnLatency,
			MaxReportWriteLatencyMs: maxReportLatency,
		}

		// Вычисляем утилизированное время ботнет сетей
		stolenTime := analytics.GlobalRollup.BuildStolenTime()

		renderDashboard(w, "dashboard_overview", prefix, counters, profileCounters, hourStats, profiles, health, stolenTime)
	})

	// 1.2. Дашборд: Динамика атаки
	mux.HandleFunc(prefix+"dashboard/vitals", func(w http.ResponseWriter, r *http.Request) {
		if !config.C.WebHttpsPanelActive {
			panel404Stub(w, r)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		counters := store.Counters()
		profileCounters := manager.Counters()
		hourStats := analytics.BuildLatestHourStatsFromBuckets(analytics.GlobalRollup, 24, 10)
		profiles := manager.ProfilesSnapshot(20)

		// Получаем метрики здоровья конвейера из tarpit
		droppedConn, droppedReport, maxConnLatency, maxReportLatency := tarpit.GetLogHealth()
		health := PipelineHealth{
			DroppedConnectionEvents: droppedConn,
			DroppedReportEntries:    droppedReport,
			MaxConnWriteLatencyMs:   maxConnLatency,
			MaxReportWriteLatencyMs: maxReportLatency,
		}
		stolenTime := analytics.GlobalRollup.BuildStolenTime()
		burst := analytics.BuildBurstSeries(store, analytics.Window1Hour)
		renderDashboardVitals(w, "dashboard_vitals", prefix, counters, profileCounters, hourStats, profiles, health, stolenTime, burst)
	})

	// 1.3. Дашборд: Отпечатки
	mux.HandleFunc(prefix+"dashboard/fingerprints", func(w http.ResponseWriter, r *http.Request) {
		if !config.C.WebHttpsPanelActive {
			panel404Stub(w, r)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		counters := store.Counters()
		profileCounters := manager.Counters()
		hourStats := analytics.BuildLatestHourStatsFromBuckets(analytics.GlobalRollup, 24, 10)
		profiles := manager.ProfilesSnapshot(20)

		// Получаем метрики здоровья конвейера из tarpit
		droppedConn, droppedReport, maxConnLatency, maxReportLatency := tarpit.GetLogHealth()
		health := PipelineHealth{
			DroppedConnectionEvents: droppedConn,
			DroppedReportEntries:    droppedReport,
			MaxConnWriteLatencyMs:   maxConnLatency,
			MaxReportWriteLatencyMs: maxReportLatency,
		}
		stolenTime := analytics.GlobalRollup.BuildStolenTime()
		renderDashboard(w, "dashboard_fingerprints", prefix, counters, profileCounters, hourStats, profiles, health, stolenTime)
	})

	// 1.4. Дашборд: Удержание
	mux.HandleFunc(prefix+"dashboard/tarpit", func(w http.ResponseWriter, r *http.Request) {
		if !config.C.WebHttpsPanelActive {
			panel404Stub(w, r)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		counters := store.Counters()
		profileCounters := manager.Counters()
		hourStats := analytics.BuildLatestHourStatsFromBuckets(analytics.GlobalRollup, 24, 10)
		profiles := manager.ProfilesSnapshot(20)

		// Получаем метрики здоровья конвейера из tarpit
		droppedConn, droppedReport, maxConnLatency, maxReportLatency := tarpit.GetLogHealth()
		health := PipelineHealth{
			DroppedConnectionEvents: droppedConn,
			DroppedReportEntries:    droppedReport,
			MaxConnWriteLatencyMs:   maxConnLatency,
			MaxReportWriteLatencyMs: maxReportLatency,
		}
		stolenTime := analytics.GlobalRollup.BuildStolenTime()
		renderDashboard(w, "dashboard_tarpit", prefix, counters, profileCounters, hourStats, profiles, health, stolenTime)
	})

	// 1.5. Дашборд: SIEM Core
	mux.HandleFunc(prefix+"dashboard/siem", func(w http.ResponseWriter, r *http.Request) {
		if !config.C.WebHttpsPanelActive {
			panel404Stub(w, r)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		counters := store.Counters()
		profileCounters := manager.Counters()
		hourStats := analytics.BuildLatestHourStatsFromBuckets(analytics.GlobalRollup, 24, 10)
		profiles := manager.ProfilesSnapshot(20)

		// Получаем метрики здоровья конвейера из tarpit
		droppedConn, droppedReport, maxConnLatency, maxReportLatency := tarpit.GetLogHealth()
		health := PipelineHealth{
			DroppedConnectionEvents: droppedConn,
			DroppedReportEntries:    droppedReport,
			MaxConnWriteLatencyMs:   maxConnLatency,
			MaxReportWriteLatencyMs: maxReportLatency,
		}

		// Запускаем детектор ботнет-паттернов
		detector := analytics.NewBotnetDetector(store, manager)
		detections := detector.DetectAll(analytics.Window1Hour)

		// Разделяем детекции по карточкам предупреждений:
		// карточка обязана нести факты, а не только счётчик.
		var proxyDets, spoofDets []analytics.BotnetDetection
		for _, det := range detections {
			switch det.Pattern {
			case analytics.BotnetPatternProxyTunnel:
				proxyDets = append(proxyDets, det)
			case analytics.BotnetPatternOSSpoofing:
				spoofDets = append(spoofDets, det)
			}
		}

		renderDashboardSIEM(w, "dashboard_siem_core", prefix, counters, profileCounters, hourStats, profiles, health, detections, proxyDets, spoofDets)
	})

	// 1.6. Дашборд: Детализация профиля
	mux.HandleFunc(prefix+"dashboard/profile/", func(w http.ResponseWriter, r *http.Request) {
		if !config.C.WebHttpsPanelActive {
			panel404Stub(w, r)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Извлекаем ID профиля из пути
		profileID := strings.TrimPrefix(r.URL.Path, prefix+"dashboard/profile/")
		if profileID == "" {
			http.Error(w, "Profile ID required", http.StatusBadRequest)
			return
		}

		// Ищем профиль
		profile, ok := manager.ProfileByID(profileID)

		// Формируем данные для шаблона.
		// ВАЖНО: передаём УКАЗАТЕЛЬ на профиль, а не значение.
		// Методы ActorProfile (TopIPs, UniqueIPCount, AvgPlannedHoldSec и др.)
		// имеют pointer-receiver. Значение, полученное из map, не addressable,
		// и шаблон не видит такие методы — ExecuteTemplate прерывался ошибкой.
		data := map[string]interface{}{
			"Prefix":  prefix,
			"Profile": &profile,
			"Found":   ok,
			"Version": buildinfo.Version,
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := panelTmpl.ExecuteTemplate(w, "dashboard_profile_detail", data); err != nil {
			http.Error(w, "Template execution error", http.StatusInternalServerError)
		}
	})

	// 1.6.5. Дашборд: Отчёты (блок 1-4)
	mux.HandleFunc(prefix+"dashboard/reports", func(w http.ResponseWriter, r *http.Request) {
		if !config.C.WebHttpsPanelActive {
			panel404Stub(w, r)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		rng := r.URL.Query().Get("range")
		var reportRange analytics.ReportRange
		switch rng {
		case "7d":
			reportRange = analytics.Range7d
		case "30d":
			reportRange = analytics.Range30d
		default:
			rng = "24h"
			reportRange = analytics.Range24h
		}

		countries := analytics.GlobalRollup.BuildCountryReport(reportRange, 50)
		ips := analytics.GlobalRollup.BuildIPReport(reportRange, 100)
		macro := analytics.GlobalRollup.BuildMacroReport(reportRange)
		traps := analytics.GlobalRollup.BuildTarpitReport(reportRange, 10)

		renderDashboardReports(w, "dashboard_reports", prefix, rng, countries, ips, macro, traps)
	})

	// === v8.6.2: API для GeoPulse ===
	mux.HandleFunc(prefix+"api/geo-pulse", func(w http.ResponseWriter, r *http.Request) {
		windowName := r.URL.Query().Get("window")
		if windowName == "" {
			windowName = "15min"
		}
		window := analytics.GetTimeWindowByName(windowName)
		if window == nil {
			http.Error(w, "Invalid window", http.StatusBadRequest)
			return
		}

		endTime := time.Now()
		if t := r.URL.Query().Get("end_time"); t != "" {
			if parsed, err := time.Parse(time.RFC3339, t); err == nil {
				endTime = parsed
			}
		}

		current := analytics.GlobalRollup.AggregateGeoStats(*window, endTime)
		previous := analytics.GlobalRollup.AggregateGeoStats(*window, endTime.Add(-window.Duration))
		metrics := analytics.CalculateGeoMetrics(current, previous)

		renderJSON(w, map[string]interface{}{
			"current":  current,
			"previous": previous,
			"metrics":  metrics,
		})
	})

	mux.HandleFunc(prefix+"api/geo-pulse/history", func(w http.ResponseWriter, r *http.Request) {
		windowName := r.URL.Query().Get("window")
		if windowName == "" {
			windowName = "15min"
		}
		window := analytics.GetTimeWindowByName(windowName)
		if window == nil {
			http.Error(w, "Invalid window", http.StatusBadRequest)
			return
		}

		// v8.6.2: интервал = глубина истории, шаг точек = естественная гранулярность.
		step := geoGranularityFor(*window)
		endTime := time.Now()
		startTime := endTime.Add(-window.Duration)

		// Точки от конца окна к началу: каждая = агрегат за step, заканчивающийся в t.
		points := make([]map[string]interface{}, 0)
		for t := endTime; t.After(startTime); t = t.Add(-step.Duration) {
			stats := analytics.GlobalRollup.AggregateGeoStats(step, t)
			points = append(points, map[string]interface{}{
				"time":             t,
				"unique_countries": stats.UniqueCountries,
				"total_sessions":   stats.TotalSessions,
			})
		}

		// Разворачиваем (старые слева, новые справа)
		for i, j := 0, len(points)-1; i < j; i, j = i+1, j-1 {
			points[i], points[j] = points[j], points[i]
		}

		renderJSON(w, map[string]interface{}{
			"points": points,
			"window": window,
			"step":   step.Name,
		})
	})

	// 1.7. Раздача статических файлов (CSS, JS)
	// StripPrefix убирает ТОЛЬКО крипто-префикс: внутри embed.FS пути
	// остаются пакетно-относительными ("assets/js/chart.min.js"),
	// поэтому остаток URL обязан совпадать с embed-путём целиком.
	mux.Handle(prefix+"assets/", http.StripPrefix(prefix, http.FileServer(http.FS(assetsFS))))

	// 2. API: Общая статистика
	mux.HandleFunc(prefix+"api/stats", func(w http.ResponseWriter, r *http.Request) {
		if !config.C.WebHttpsPanelActive {
			panel404Stub(w, r)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// v8.6.2: Читаем лимит строк из запроса
		limitStr := r.URL.Query().Get("limit")
		limit := 20
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}

		// v8.6.2: Получаем stolenTime и current_time для обновления крупных значений
		stolenTime := analytics.GlobalRollup.BuildStolenTime()

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

		renderJSON(w, map[string]interface{}{
			"store_counters":   store.Counters(),
			"profile_counters": manager.Counters(),
			"hourly_stats":     analytics.BuildLatestHourStatsFromBuckets(analytics.GlobalRollup, 24, 10),
			"burst_series":     analytics.BuildBurstSeries(store, 60*time.Minute),
			"profiles":         getProfilesJSON(manager, limit),
			"current_time":     time.Now().In(loc).Format("2006-01-02 15:04:05 MST"),
			"history_loaded":   analytics.IsHistoryLoaded(),
			"stolen_time": map[string]interface{}{
				"HourSec":        stolenTime.HourSec,
				"DaySec":         stolenTime.DaySec,
				"WeekSec":        stolenTime.WeekSec,
				"MonthSec":       stolenTime.MonthSec,
				"HourFormatted":  analytics.FormatDuration(stolenTime.HourSec),
				"DayFormatted":   analytics.FormatDuration(stolenTime.DaySec),
				"WeekFormatted":  analytics.FormatDuration(stolenTime.WeekSec),
				"MonthFormatted": analytics.FormatDuration(stolenTime.MonthSec),
			},
		})
	})

	// 2.2. API: Отчёты для тихого автообновления
	mux.HandleFunc(prefix+"api/reports", func(w http.ResponseWriter, r *http.Request) {
		if !config.C.WebHttpsPanelActive {
			panel404Stub(w, r)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		rng := analytics.Range24h
		switch r.URL.Query().Get("range") {
		case "7d":
			rng = analytics.Range7d
		case "30d":
			rng = analytics.Range30d
		}

		// Лимиты — ТЕ ЖЕ, что в хендлере dashboard/reports (Топ-100 по доктрине шаблона)
		macro := analytics.GlobalRollup.BuildMacroReport(rng)
		countries := analytics.GlobalRollup.BuildCountryReport(rng, 100)
		ips := analytics.GlobalRollup.BuildIPReport(rng, 100)
		traps := analytics.GlobalRollup.BuildTarpitReport(rng, 0)

		// DTO с предформатированными строками: JS вставляет как есть.
		// Формат единственно здесь (FormatDuration, printf) — рассинхрон невозможен.
		type behaviorDTO struct {
			Name    string `json:"name"`
			Count   uint64 `json:"count"`
			Percent string `json:"percent"`
		}
		behaviors := make([]behaviorDTO, 0, len(macro.BehaviorDisplay))
		for _, b := range macro.BehaviorDisplay {
			behaviors = append(behaviors, behaviorDTO{Name: b.Name, Count: b.Count, Percent: fmt.Sprintf("%.2f", b.Percent)})
		}

		type trapRowDTO struct {
			Label           string `json:"label"`
			Events          uint64 `json:"events"`
			Hold            string `json:"hold"`
			UniqueIPs       int    `json:"unique_ips"`
			UniqueCountries int    `json:"unique_countries"`
			DropCount       uint64 `json:"drop_count"`
			Efficiency      string `json:"efficiency"`
		}
		trapRows := make([]trapRowDTO, 0, len(traps))
		for _, t := range traps {
			trapRows = append(trapRows, trapRowDTO{
				Label: t.Label, Events: t.Events, Hold: analytics.FormatDuration(t.HoldSumSec),
				UniqueIPs: t.UniqueIPs, UniqueCountries: t.UniqueCountries, DropCount: t.DropCount,
				Efficiency: fmt.Sprintf("%.1f", t.Efficiency),
			})
		}

		type countryRowDTO struct {
			Code          string `json:"code"`
			TopTrap       string `json:"top_trap"`
			Events        uint64 `json:"events"`
			UniqueIPs     int    `json:"unique_ips"`
			Hold          string `json:"hold"`
			Concentration string `json:"concentration"`
		}
		countryRows := make([]countryRowDTO, 0, len(countries))
		for _, c := range countries {
			conc := "—"
			if c.UniqueIPs > 0 {
				conc = fmt.Sprintf("%.1f", c.Concentration)
			}
			countryRows = append(countryRows, countryRowDTO{
				Code: c.Code, TopTrap: c.TopTrap, Events: c.Events,
				UniqueIPs: c.UniqueIPs, Hold: analytics.FormatDuration(c.HoldSumSec), Concentration: conc,
			})
		}

		type ipRowDTO struct {
			IP          string `json:"ip"`
			Country     string `json:"country"`
			Trap        string `json:"trap"`
			Events      uint64 `json:"events"`
			Hold        string `json:"hold"`
			RTT         string `json:"rtt"`
			DropCount   uint64 `json:"drop_count"`
			CloseReason string `json:"close_reason"`
			LastSeen    string `json:"last_seen"`
		}
		ipRows := make([]ipRowDTO, 0, len(ips))
		for _, ip := range ips {
			country := ip.Country
			if country == "" {
				country = "—"
			}
			rtt := "—"
			if ip.AvgRTTMs > 0 {
				rtt = fmt.Sprintf("%.0f", ip.AvgRTTMs)
			}
			lastSeen := "—"
			if !ip.LastSeen.IsZero() {
				lastSeen = ip.LastSeen.Format("02.01 15:04")
			}
			ipRows = append(ipRows, ipRowDTO{
				IP: ip.IP, Country: country, Trap: ip.Trap, Events: ip.Events,
				Hold: analytics.FormatDuration(ip.HoldSumSec), RTT: rtt, DropCount: ip.DropCount,
				CloseReason: ip.TopCloseReason, LastSeen: lastSeen,
			})
		}

		avgDrop := "—"
		if macro.DropCount > 0 {
			avgDrop = fmt.Sprintf("%.1f", macro.AvgDropSec)
		}

		renderJSON(w, map[string]interface{}{
			"macro": map[string]interface{}{
				"events":           macro.Events,
				"valid_events":     macro.ValidEvents,
				"hold":             analytics.FormatDuration(macro.HoldSumSec),
				"unique_countries": macro.UniqueCountries,
				"unique_ips":       macro.UniqueIPs,
				"drop_count":       macro.DropCount,
				"avg_drop":         avgDrop,
				"behaviors":        behaviors,
			},
			"traps":     trapRows,
			"countries": countryRows,
			"ips":       ipRows,
		})
	})

	// 2.3. API: Данные профиля для тихого автообновления
	mux.HandleFunc(prefix+"api/profile/data", func(w http.ResponseWriter, r *http.Request) {
		if !config.C.WebHttpsPanelActive {
			panel404Stub(w, r)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		profileID := r.URL.Query().Get("id")
		if profileID == "" {
			http.Error(w, "Profile ID required", http.StatusBadRequest)
			return
		}

		profile, found := manager.GetProfile(profileID)
		if !found || profile == nil {
			renderJSON(w, map[string]interface{}{"found": false})
			return
		}

		// Определяем витринную локацию
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

		formatTime := func(t time.Time) string {
			if t.IsZero() {
				return "—"
			}
			return t.In(loc).Format("02.01 15:04")
		}
		formatTimeSec := func(t time.Time) string {
			if t.IsZero() {
				return "—"
			}
			return t.In(loc).Format("02.01 15:04:05")
		}

		// DTO для чистой сериализации (числовые типы, как в analytics.StackFingerprint)
		type stackDTO struct {
			MSS       uint32 `json:"mss"`
			RcvSpace  uint64 `json:"rcv_space"`
			SndWscale uint8  `json:"snd_wscale"`
			RcvWscale uint8  `json:"rcv_wscale"`
		}

		renderJSON(w, map[string]interface{}{
			"found": true,
			"data": map[string]interface{}{
				"id":                    profile.ID,
				"state":                 profile.State,
				"confidence":            profile.Confidence,
				"false_positive_risk":   profile.FalsePositiveRisk,
				"first_event_time":      formatTime(profile.FirstEventTime),
				"last_event_time":       formatTime(profile.LastEventTime),
				"last_seen":             formatTimeSec(profile.LastSeen),
				"event_count":           profile.EventCount,
				"valid_event_count":     profile.ValidEventCount,
				"invalid_event_count":   profile.InvalidEventCount,
				"current_phase":         profile.CurrentPhase,
				"threat_level":          profile.ThreatLevel,
				"predicted_vector":      profile.PredictedVector,
				"predicted_probability": profile.PredictedProbability,
				"token_drift_valid":     profile.TokenDriftValid,
				"token_drift_ms":        profile.TokenDriftMs,
				"token_client_port":     profile.TokenClientPort,
				"stack_valid":           profile.StackValid,
				// Прямое обращение к полям безопасно, т.к. Stack — это value type, а не pointer
				"stack": stackDTO{
					MSS:       profile.Stack.MSS,
					RcvSpace:  profile.Stack.RcvSpace,
					SndWscale: profile.Stack.SndWscale,
					RcvWscale: profile.Stack.RcvWscale,
				},
				"unique_ip_count":      profile.UniqueIPCount,
				"unique_country_count": profile.UniqueCountryCount,
				"top_ips":              profile.TopIPs(10),
				"top_countries":        profile.TopCountries(10),
				"hold_count":           profile.HoldCount,
				"avg_planned_hold":     profile.AvgPlannedHoldSec,
				"avg_actual_hold":      profile.AvgActualHoldSec,
				"rtt_count":            profile.RTTCount,
				"rtt_avg_ms":           profile.RTTAvgMs,
				"rtt_min_ms":           profile.RTTMinMs,
				"rtt_max_ms":           profile.RTTMaxMs,
				"evidence":             profile.Evidence,
				"reasons":              profile.Reasons,
			},
		})
	})

	// 2.1. API: SIEM данные для тихого автообновления
	mux.HandleFunc(prefix+"api/siem", func(w http.ResponseWriter, r *http.Request) {
		if !config.C.WebHttpsPanelActive {
			panel404Stub(w, r)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// 1. Определяем витринную локацию (идентично renderDashboard)
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

		// 2. Запускаем детектор ботнет-паттернов (ровно та же логика, что и в dashboard/siem)
		detector := analytics.NewBotnetDetector(store, manager)
		detections := detector.DetectAll(analytics.Window1Hour)

		// 3. Разделяем детекции по карточкам предупреждений
		var proxyDets, spoofDets []analytics.BotnetDetection
		for _, det := range detections {
			switch det.Pattern {
			case analytics.BotnetPatternProxyTunnel:
				proxyDets = append(proxyDets, det)
			case analytics.BotnetPatternOSSpoofing:
				spoofDets = append(spoofDets, det)
			}
		}

		// 4. DTO для корректной сериализации времени в формате "15:04:05"
		type siemDetectionDTO struct {
			Label       string            `json:"label"`
			Confidence  string            `json:"confidence"`
			IPsInvolved []string          `json:"ips_involved"`
			CountryByIP map[string]string `json:"country_by_ip"`
			Evidence    []string          `json:"evidence"`
			StartTime   string            `json:"start_time"`
		}

		convertDets := func(dets []analytics.BotnetDetection) []siemDetectionDTO {
			res := make([]siemDetectionDTO, len(dets))
			for i, d := range dets {
				timeStr := "—"
				if !d.StartTime.IsZero() {
					timeStr = d.StartTime.In(loc).Format("15:04:05")
				}
				res[i] = siemDetectionDTO{
					Label:       d.Label,
					Confidence:  string(d.Confidence),
					IPsInvolved: d.IPsInvolved,
					CountryByIP: d.CountryByIP,
					Evidence:    d.Evidence,
					StartTime:   timeStr,
				}
			}
			return res
		}

		// 5. Отдаём JSON (используем существующую buildWarningLines из этого же пакета)
		renderJSON(w, map[string]interface{}{
			"detections":    convertDets(detections),
			"proxy_count":   len(proxyDets),
			"proxy_details": buildWarningLines(proxyDets, 5),
			"spoof_count":   len(spoofDets),
			"spoof_details": buildWarningLines(spoofDets, 5),
		})
	})

	// 3. API: Список профилей
	mux.HandleFunc(prefix+"api/profiles", func(w http.ResponseWriter, r *http.Request) {
		if !config.C.WebHttpsPanelActive {
			panel404Stub(w, r)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		limit := 50
		if l := r.URL.Query().Get("limit"); l != "" {
			if parsed, err := parseInt(l, 1, 1000); err == nil {
				limit = parsed
			}
		}
		profiles := manager.ProfilesSnapshot(limit)
		renderJSON(w, map[string]interface{}{
			"count":    len(profiles),
			"limit":    limit,
			"profiles": profiles,
		})
	})

	// 4. API: Конкретный профиль
	mux.HandleFunc(prefix+"api/profile/", func(w http.ResponseWriter, r *http.Request) {
		if !config.C.WebHttpsPanelActive {
			panel404Stub(w, r)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if len(r.URL.Path) <= len(prefix+"api/profile/") {
			http.Error(w, "Profile ID required", http.StatusBadRequest)
			return
		}
		profileID := r.URL.Path[len(prefix+"api/profile/"):]
		profile, ok := manager.ProfileByID(profileID)
		if !ok {
			http.Error(w, "Profile not found", http.StatusNotFound)
			return
		}
		renderJSON(w, map[string]interface{}{"profile": profile})
	})

	// 5. API: Ботнет-детекция
	mux.HandleFunc(prefix+"api/botnet", func(w http.ResponseWriter, r *http.Request) {
		if !config.C.WebHttpsPanelActive {
			panel404Stub(w, r)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		detector := analytics.NewBotnetDetector(store, manager)
		window := analytics.Window1Hour
		if w := r.URL.Query().Get("window"); w != "" {
			if parsed, err := parseDuration(w); err == nil {
				window = parsed
			}
		}
		renderJSON(w, map[string]interface{}{
			"window":     window.String(),
			"detections": detector.DetectAll(window),
		})
	})

	// 6. API: Диагностика инжеста
	mux.HandleFunc(prefix+"api/diag", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		diag := store.Diagnostics()

		// Добавляем последние 3 строки из лога для проверки формата
		if path, ok := diag["ingest_path"].(string); ok && path != "" {
			if data, err := os.ReadFile(path); err == nil {
				lines := strings.Split(strings.TrimSpace(string(data)), "\n")
				lastLines := make([]map[string]interface{}, 0)
				start := len(lines) - 3
				if start < 0 {
					start = 0
				}
				for i := start; i < len(lines); i++ {
					fields := strings.Split(lines[i], "\t")
					lastLines = append(lastLines, map[string]interface{}{
						"line":        lines[i],
						"field_count": len(fields),
					})
				}
				diag["last_lines"] = lastLines
				diag["file_size"] = len(data)
			} else {
				diag["file_read_error"] = err.Error()
			}
		}

		renderJSON(w, diag)
	})

}
