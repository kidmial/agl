// internal/analytics/geo_metrics.go
// =====================================================================
// КОМПОНЕНТ: ГЕОГРАФИЧЕСКИЕ МЕТРИКИ ДЛЯ БОТНЕТ-ДЕТЕКЦИИ
// РОЛЬ: Вычисление GDI, Geo-Churn, Geo-Dominance для виджета GeoPulse.
// ПРИНЦИП:
//   - агрегация из минутных, часовых или суточных бакетов в зависимости от окна;
//   - расчёт метрик для текущего и предыдущего интервала (для Geo-Churn);
//   - интерпретация метрик для криминалистического анализа.
//
// =====================================================================
package analytics

import (
	"time"
)

// TimeWindow описывает временное окно для агрегации.
type TimeWindow struct {
	Name     string        // "1min", "5min", "1h", "24h", "7d", "30d"
	Duration time.Duration // 1*time.Minute, 5*time.Minute, ...
	Label    string        // "1 минута", "5 минут", ...
}

// TimeWindows — все доступные интервалы для GeoPulse.
var TimeWindows = []TimeWindow{
	{"1min", 1 * time.Minute, "1 минута"},
	{"5min", 5 * time.Minute, "5 минут"},
	{"10min", 10 * time.Minute, "10 минут"},
	{"15min", 15 * time.Minute, "15 минут"},
	{"30min", 30 * time.Minute, "30 минут"},
	{"1h", 1 * time.Hour, "1 час"},
	{"3h", 3 * time.Hour, "3 часа"},
	{"24h", 24 * time.Hour, "24 часа"},
	{"7d", 7 * 24 * time.Hour, "7 дней"},
	{"30d", 30 * 24 * time.Hour, "30 дней"},
}

// GeoStats — агрегированная статистика по странам за интервал.
type GeoStats struct {
	TotalSessions   uint64            // общее число сессий
	CountrySessions map[string]uint64 // страна -> количество сессий
	UniqueCountries int               // количество уникальных стран
	StartTime       time.Time         // начало интервала
	EndTime         time.Time         // конец интервала
}

// GeoMetrics — криминалистические метрики географического анализа.
type GeoMetrics struct {
	GDI          float64 // Geographic Density Index (Total_Sessions / Unique_Countries)
	GeoChurn     float64 // % новых стран относительно предыдущего интервала
	GeoDominance string  // страна-лидер (если >50%)
	DominancePct float64 // % доминирования страны-лидера
}

// AggregateGeoStats агрегирует географию за заданное окно.
// Выбор источника бакетов:
//   - Duration <= 30min → минутные бакеты
//   - Duration <= 3h → часовые бакеты
//   - Duration > 3h → суточные бакеты
func (r *Rollup) AggregateGeoStats(window TimeWindow, endTime time.Time) GeoStats {
	if r == nil {
		return GeoStats{CountrySessions: make(map[string]uint64)}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	loc := time.UTC
	if r.loc != nil {
		loc = r.loc
	}
	endTime = endTime.In(loc)
	startTime := endTime.Add(-window.Duration)

	stats := GeoStats{
		CountrySessions: make(map[string]uint64),
		StartTime:       startTime,
		EndTime:         endTime,
	}

	switch {
	case window.Duration <= 30*time.Minute:
		// Суммируем минутные бакеты
		for _, m := range r.minutes {
			if m == nil || m.Minute.Before(startTime) || !m.Minute.Before(endTime) {
				continue
			}
			stats.TotalSessions += m.Events
			for country, cb := range m.Countries {
				stats.CountrySessions[country] += cb.Events
			}
		}
	case window.Duration <= 3*time.Hour:
		// Суммируем часовые бакеты
		for _, h := range r.hours {
			if h == nil || h.Hour.Before(startTime) || !h.Hour.Before(endTime) {
				continue
			}
			stats.TotalSessions += h.Events
			for country, cb := range h.Countries {
				stats.CountrySessions[country] += cb.Events
			}
		}
	default:
		// Суммируем суточные бакеты
		for _, d := range r.days {
			if d == nil || d.Day.Before(startTime) || !d.Day.Before(endTime) {
				continue
			}
			stats.TotalSessions += d.Events
			for country, cb := range d.Countries {
				stats.CountrySessions[country] += cb.Events
			}
		}
	}

	stats.UniqueCountries = len(stats.CountrySessions)
	return stats
}

// CalculateGeoMetrics вычисляет криминалистические метрики.
// Аргументы:
//   - current: статистика за текущий интервал
//   - previous: статистика за предыдущий интервал (для Geo-Churn)
func CalculateGeoMetrics(current, previous GeoStats) GeoMetrics {
	// Метрика 1: GDI (Geographic Density Index)
	gdi := 0.0
	if current.UniqueCountries > 0 {
		gdi = float64(current.TotalSessions) / float64(current.UniqueCountries)
	}

	// Метрика 2: Geo-Churn (% новых стран)
	newCountries := 0
	for country := range current.CountrySessions {
		if _, exists := previous.CountrySessions[country]; !exists {
			newCountries++
		}
	}
	geoChurn := 0.0
	if current.UniqueCountries > 0 {
		geoChurn = (float64(newCountries) / float64(current.UniqueCountries)) * 100
	}

	// Метрика 3: Geo-Dominance (страна-лидер >50%)
	geoDominance := ""
	dominancePct := 0.0
	for country, sessions := range current.CountrySessions {
		pct := (float64(sessions) / float64(current.TotalSessions)) * 100
		if pct > 50 {
			geoDominance = country
			dominancePct = pct
			break
		}
	}

	return GeoMetrics{
		GDI:          gdi,
		GeoChurn:     geoChurn,
		GeoDominance: geoDominance,
		DominancePct: dominancePct,
	}
}

// GetTimeWindowByName возвращает TimeWindow по имени или nil, если не найдено.
func GetTimeWindowByName(name string) *TimeWindow {
	for i := range TimeWindows {
		if TimeWindows[i].Name == name {
			return &TimeWindows[i]
		}
	}
	return nil
}
