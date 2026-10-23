// internal/webpanel/helpers.go
// =====================================================================
// КОМПОНЕНТ: ВСПОМОГАТЕЛЬНЫЕ ФУНКЦИИ ВЕБ-ПАНЕЛИ
// РОЛЬ: Хелперы для рендеринга JSON и парсинга параметров.
// ПРИНЦИП: Вынесены из server.go для каноничной структуры пакета.
// =====================================================================
package webpanel

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"
)

// renderJSON кодирует структуру в JSON и отдает клиенту.
func renderJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("[WEBPANEL] JSON encode error: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// parseInt безопасно парсит строку в int с ограничениями min/max.
func parseInt(s string, min, max int) (int, error) {
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, err
	}
	if v < min {
		return min, nil
	}
	if v > max {
		return max, nil
	}
	return v, nil
}

// parseDuration безопасно парсит строку в time.Duration.
func parseDuration(s string) (time.Duration, error) {
	return time.ParseDuration(s)
}
