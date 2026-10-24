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

	"agl/config"
	"agl/internal/analytics"
)

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

	// 1. Главная страница панели (HTML)
	mux.HandleFunc(prefix+"panel", func(w http.ResponseWriter, r *http.Request) {
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
		hourStats := analytics.BuildLatestHourStats(store, 24, 10)
		profiles := manager.ProfilesSnapshot(20)

		// Вызов стал чище: вся логика таймзоны теперь внутри renderPanelHTML
		renderPanelHTML(w, counters, profileCounters, hourStats, profiles)
	})

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
			"hourly_stats":     analytics.BuildLatestHourStats(store, 24, 10),
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
