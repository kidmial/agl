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
	"net/http"
	"os"
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
		renderJSON(w, map[string]interface{}{
			"store_counters":   store.Counters(),
			"profile_counters": manager.Counters(),
			"hourly_stats":     analytics.BuildLatestHourStatsFromBuckets(analytics.GlobalRollup, 24, 10),
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

	// 7. API: Принудительная загрузка истории из лога (v8.6.2)
	// Использует централизованный RebuildHistory из analytics пакета.
	// Это гарантирует единый источник правды для автозагрузки и ручной кнопки.
	mux.HandleFunc(prefix+"api/rebuild", func(w http.ResponseWriter, r *http.Request) {
		if !config.C.WebHttpsPanelActive {
			panel404Stub(w, r)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed (use POST)", http.StatusMethodNotAllowed)
			return
		}

		logPath := config.C.TarpitRptLog
		if logPath == "" {
			logPath = "/var/log/amberguard/tarpit_report.log"
		}

		// Вызываем централизованную функцию загрузки истории.
		// Она сама проверяет guard и возвращает статистику.
		result, err := analytics.RebuildHistory(logPath)
		if err != nil {
			renderJSON(w, map[string]interface{}{
				"success": false,
				"error":   err.Error(),
			})
			return
		}

		if result == nil {
			// Guard сработал: история уже загружена в этом процессе.
			renderJSON(w, map[string]interface{}{
				"success":     false,
				"reason":      "history already loaded in this process; restart service to reload",
				"store_total": store.Counters().TotalEvents,
			})
			return
		}

		// Успешная загрузка.
		renderJSON(w, map[string]interface{}{
			"success":     true,
			"file":        logPath,
			"lines_read":  result.LinesRead,
			"parsed":      result.Parsed,
			"valid":       result.Valid,
			"broken":      result.Broken,
			"store_total": store.Counters().TotalEvents,
			"profiles":    manager.Counters().Profiles,
		})
	})

}
