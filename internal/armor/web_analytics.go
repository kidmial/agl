// internal/armor/web_analytics.go
// Инициализация движка и инжеста
package armor

import (
	"agl/config"
	"agl/internal/analytics"
)

// startWebAnalytics инициализирует движок аналитики и инжест для веб-слоя.
// Единый безопасный вызов. Благодаря sync.Once внутри analytics.Start,
// инициализация и инжест запустятся ровно один раз.
func startWebAnalytics() {
	analytics.Start(config.C.TarpitRptLog)
}
