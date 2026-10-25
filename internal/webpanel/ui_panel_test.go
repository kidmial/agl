// internal/webpanel/ui_panel_test.go
// =====================================================================
// КОМПОНЕНТ: РЕГРЕССИОННЫЕ ТЕСТЫ ШАБЛОНОВ ВЕБ-ПАНЕЛИ
// РОЛЬ: Ловить ошибки шаблонов НА ЭТАПЕ ТЕСТОВ, а не в init прода.
// ИСТОРИЯ:
//
//	Паника "function json not defined" ушла в прод, потому что
//	парсинг шаблонов происходит в init пакета webpanel, который
//	тесты аналитики не трогают. Этот файл исполняет шаблоны в тестах.
//
// =====================================================================
package webpanel

import (
	"bytes"
	"strings"
	"testing"

	"agl/internal/analytics"
)

// TestTemplatesDefined проверяет, что все ожидаемые блоки и дашборды определены.
func TestTemplatesDefined(t *testing.T) {
	names := []string{
		"header",
		"footer",
		"dashboard_overview",
		"dashboard_vitals",
		"dashboard_fingerprints",
		"dashboard_tarpit",
		"dashboard_siem_core",
	}
	for _, name := range names {
		if panelTmpl.Lookup(name) == nil {
			t.Fatalf("template %q is not defined", name)
		}
	}
}

// TestToJSON проверяет работу кастомной функции json.
func TestToJSON(t *testing.T) {
	got := string(toJSON(map[string]int{"a": 1}))
	if got != `{"a":1}` {
		t.Fatalf("toJSON() = %s, want {\"a\":1}", got)
	}
}

// TestRenderVitalsTemplate исполняет дашборд динамики с пустыми данными.
// Ловит рантайм-ошибки шаблона (неизвестные функции, битые пайплайны).
func TestRenderVitalsTemplate(t *testing.T) {
	var buf bytes.Buffer
	data := map[string]interface{}{
		"Prefix":    "test/",
		"HourStats": []analytics.HourStat{},
	}
	if err := panelTmpl.ExecuteTemplate(&buf, "dashboard_vitals", data); err != nil {
		t.Fatalf("execute dashboard_vitals: %v", err)
	}
	if !strings.Contains(buf.String(), "phaseChart") {
		t.Fatal("phaseChart canvas not found in rendered output")
	}
	if !strings.Contains(buf.String(), "const hourStats = [];") {
		t.Fatal("hourStats JSON injection not found in rendered output")
	}
}
