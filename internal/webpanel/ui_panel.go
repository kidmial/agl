// internal/webpanel/ui_panel.go
// =====================================================================
// КОМПОНЕНТ: РЕНДЕРИНГ ВЕБ-ПАНЕЛИ АНАЛИТИКИ
// РОЛЬ: Генерация HTML-дашборда для отображения статистики и профилей.
// ПРИНЦИП:
//   - тёмная тема, моноширинный шрифт, неоновые акценты (стиль AGL);
//   - встроенный шаблон (в будущем будет //go:embed assets/panel.html);
//   - безопасный вывод данных через html/template.
//
// =====================================================================
package webpanel

import (
	"html/template"
	"net/http"
	"time"

	"agl/internal/analytics"
)

// panelTemplateHTML — инлайн-шаблон дашборда.
// Стилизован под терминал AGL: фон #0f141c, текст #a5b4fc, акценты #00ff66.
const panelTemplateHTML = `<!DOCTYPE html>
<html lang="ru">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>AGL Analytics Panel</title>
    <style>
        body {
            background: #0f141c;
            color: #a5b4fc;
            font-family: 'Courier New', Courier, monospace;
            margin: 0;
            padding: 20px;
            font-size: 14px;
        }
        h1, h2, h3 { color: #00ff66; font-weight: normal; border-bottom: 1px solid #1e293b; padding-bottom: 10px; }
        .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(300px, 1fr)); gap: 20px; margin-bottom: 30px; }
        .card { background: #111827; border: 1px solid #1e293b; border-radius: 4px; padding: 20px; }
        .metric { font-size: 24px; color: #ffffff; margin: 10px 0; }
        .metric-label { font-size: 12px; color: #64748b; text-transform: uppercase; letter-spacing: 1px; }
        table { width: 100%; border-collapse: collapse; margin-top: 10px; }
        th, td { text-align: left; padding: 10px; border-bottom: 1px solid #1e293b; }
        th { color: #64748b; font-weight: normal; text-transform: uppercase; font-size: 12px; }
        tr:hover { background: #1e293b; }
        .badge { padding: 2px 6px; border-radius: 3px; font-size: 11px; font-weight: bold; }
        .badge-active { background: #064e3b; color: #00ff66; }
        .badge-low { background: #78350f; color: #fbbf24; }
        .badge-expired { background: #7f1d1d; color: #f87171; }
        .text-muted { color: #64748b; }
    </style>
</head>
<body>
    <h1>Amber Guard Laboratory :: Analytics Dashboard</h1>

    <!-- Сводные метрики -->
    <div class="grid">
        <div class="card">
            <div class="metric-label">Всего событий в буфере</div>
            <div class="metric">{{ .StoreCounters.TotalEvents }}</div>
            <div class="text-muted">Валидных: {{ .StoreCounters.ValidEvents }} | Битых: {{ .StoreCounters.InvalidEvents }}</div>
        </div>
        <div class="card">
            <div class="metric-label">Активные профили акторов</div>
            <div class="metric">{{ .ProfileCounters.Active }}</div>
            <div class="text-muted">Низкая уверенность: {{ .ProfileCounters.LowConfidence }} | Просрочено: {{ .ProfileCounters.Expired }}</div>
        </div>
        <div class="card">
            <div class="metric-label">Временной охват</div>
            <div class="metric">{{ if .StoreCounters.HasEventTime }}{{ .StoreCounters.FirstEventTime.Format "02.01" }} - {{ .StoreCounters.LastEventTime.Format "02.01" }}{{ else }}Нет данных{{ end }}</div>
            <div class="text-muted">Обновлено: {{ .CurrentTime }}</div>
        </div>
    </div>

    <!-- Часовая статистика -->
    <div class="card">
        <h2>Последние 24 часа</h2>
        <table>
            <thead>
                <tr>
                    <th>Час (UTC)</th>
                    <th>Событий</th>
                    <th>Уник. IP</th>
                    <th>Агрессивных</th>
                    <th>Средний RTT</th>
                </tr>
            </thead>
            <tbody>
                {{ range .HourStats }}
                <tr>
                    <td>{{ .Hour.Format "15:04" }}</td>
                    <td>{{ .Events }}</td>
                    <td>{{ .UniqueIPs }}</td>
                    <td>{{ .AggressiveEvents }}</td>
                    <td>{{ printf "%.0f" .RTTAvgMs }} ms</td>
                </tr>
                {{ end }}
            </tbody>
        </table>
    </div>

    <!-- Профили акторов -->
    <div class="card" style="margin-top: 30px;">
        <h2>Последние профили акторов</h2>
        <table>
            <thead>
                <tr>
                    <th>ID Профиля</th>
                    <th>Уник. IP</th>
                    <th>Состояние</th>
                    <th>Фаза (Алг.3)</th>
                    <th>Прогноз (Алг.4)</th>
                    <th>Дрейф (Алг.2)</th>
                    <th>Последняя активность</th>
                </tr>
            </thead>
            <tbody>
                {{ range .Profiles }}
                <tr>
                    <td><code>{{ .ID }}</code></td>
                    <td>{{ .UniqueIPCount }}</td>
                    <td>
                        {{ if eq .State "ACTIVE" }}<span class="badge badge-active">ACTIVE</span>
                        {{ else if eq .State "LOW_CONFIDENCE" }}<span class="badge badge-low">LOW_CONF</span>
                        {{ else if eq .State "MERGED" }}<span class="badge badge-expired">MERGED</span>
                        {{ else }}<span class="badge badge-expired">EXPIRED</span>{{ end }}
                    </td>
                    <td style="color: #fbbf24;">{{ if .CurrentPhase }}{{ .CurrentPhase }}{{ else }}—{{ end }}</td>
                    <td style="color: #00ff66;">{{ if .PredictedVector }}{{ .PredictedVector }}{{ else }}—{{ end }}</td>
                    <td>{{ if .TokenDriftValid }}{{ .TokenDriftMs }} мс{{ else }}—{{ end }}</td>
                    <td>{{ .LastSeen.Format "15:04:05" }}</td>
                </tr>
                {{ end }}
            </tbody>
        </table>
    </div>

</body>
</html>`

// panelTmpl — скомпилированный шаблон.
// Must гарантирует, что шаблон синтезируется на старте, а не во время запроса.
var panelTmpl = template.Must(template.New("panel").Parse(panelTemplateHTML))

// renderPanelHTML отдаёт отрендеренный HTML дашборда.
func renderPanelHTML(w http.ResponseWriter, counters analytics.StoreCounters, profileCounters analytics.ProfileManagerCounters, hourStats []analytics.HourStat, profiles []analytics.ActorProfile) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	data := map[string]interface{}{
		"StoreCounters":   counters,
		"ProfileCounters": profileCounters,
		"HourStats":       hourStats,
		"Profiles":        profiles,
		"CurrentTime":     time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
	}

	if err := panelTmpl.Execute(w, data); err != nil {
		http.Error(w, "Template execution error", http.StatusInternalServerError)
	}
}
