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

// TestRenderOverviewTemplate исполняет дашборд обзора с профилем в списке.
// Ловит ошибки контекста внутри range (например, .Prefix вместо $.Prefix).
func TestRenderOverviewTemplate(t *testing.T) {
	var buf bytes.Buffer
	data := map[string]interface{}{
		"Prefix":     "test/",
		"HourStats":  []analytics.HourStat{},
		"StolenTime": analytics.StolenTimeStats{},
		"Profiles": []analytics.ActorProfile{
			{ID: "AGL_PROFILE_test123"},
		},
	}
	if err := panelTmpl.ExecuteTemplate(&buf, "dashboard_overview", data); err != nil {
		t.Fatalf("execute dashboard_overview: %v", err)
	}
	if !strings.Contains(buf.String(), "AGL_PROFILE_test123") {
		t.Fatal("profile ID not found in rendered output")
	}
	if !strings.Contains(buf.String(), "test/dashboard/profile/AGL_PROFILE_test123") {
		t.Fatal("profile link href not found in rendered output")
	}
}

// TestRenderProfileDetailTemplate исполняет страницу досье с указателем на профиль.
// Ловит ошибки вызова pointer-receiver методов (TopIPs, UniqueIPCount) в шаблоне.
func TestRenderProfileDetailTemplate(t *testing.T) {
	var buf bytes.Buffer
	profile := analytics.ActorProfile{
		ID:            "AGL_PROFILE_test123",
		IPCounts:      map[string]int{"10.0.0.1": 5},
		CountryCounts: map[string]int{"Netherlands": 5},
	}
	data := map[string]interface{}{
		"Prefix":  "test/",
		"Profile": &profile,
		"Found":   true,
	}
	if err := panelTmpl.ExecuteTemplate(&buf, "dashboard_profile_detail", data); err != nil {
		t.Fatalf("execute dashboard_profile_detail: %v", err)
	}
	if !strings.Contains(buf.String(), "10.0.0.1") {
		t.Fatal("top IP not found in rendered output")
	}
	if !strings.Contains(buf.String(), "Netherlands") {
		t.Fatal("top country not found in rendered output")
	}
}

// TestRenderReportsTemplate исполняет страницу отчётов с несколькими странами и IP.
// Ловит обрыв рендеринга таблицы (ошибки типов в шаблонных функциях):
// если шаблон упадёт на первой строке, остальные страны и IP не попадут в вывод.
func TestRenderReportsTemplate(t *testing.T) {
	var buf bytes.Buffer
	countries := []analytics.CountryReport{
		{Code: "France", Events: 100, UniqueIPs: 10, Concentration: 10, TopTrap: "CORE"},
		{Code: "Indonesia", Events: 80, UniqueIPs: 8, Concentration: 10, TopTrap: "2222"},
	}
	ips := []analytics.IPReport{
		{IP: "1.2.3.4", Country: "France", Trap: "3333", Events: 50, TopCloseReason: "TIMEOUT"},
		{IP: "5.6.7.8", Country: "Indonesia", Trap: "4444", Events: 40, TopCloseReason: "RST"},
	}
	macro := analytics.MacroReport{
		Events:   180,
		Behavior: map[string]uint64{"PASSIVE": 100, "AGGRESSIVE": 80},
	}
	data := map[string]interface{}{
		"Prefix":    "test/",
		"Range":     "24h",
		"Countries": countries,
		"IPs":       ips,
		"Macro":     macro,
	}
	if err := panelTmpl.ExecuteTemplate(&buf, "dashboard_reports", data); err != nil {
		t.Fatalf("execute dashboard_reports: %v", err)
	}
	out := buf.String()
	// У каждой сторожимой ячейки — уникальный маркер: иначе удаление ячейки IP
	// маскируется тем же значением в ячейке страны (поймано mutation-проверкой).
	for _, want := range []string{
		"France", "Indonesia", "1.2.3.4", "5.6.7.8",
		// Маркеры отражают реаль структуру вывода с privacy-обёртками:
		// если обёртку уберут — тест упадёт (mutation-дисциплина).
		"<td><agl-private>CORE</agl-private></td>", // ячейка топ-ловушки страны (4-х символьный формат)
		"<td><agl-private>2222</agl-private></td>", // ячейка топ-ловушки страны (4-х символьный формат)
		"<td><agl-private>3333</agl-private></td>", // ячейка ловушки IP (4-х символьный формат)
		"<td><agl-private>4444</agl-private></td>", // ячейка ловушки IP (4-х символьный формат)
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("report output missing %q (template truncated?)", want)
		}
	}
}

// TestRenderSIEMTemplate исполняет страницу SIEM с детекцией, несущей страну.
// Гарантирует, что предупреждения рендерятся вместе с фактами (субъект + основание).
func TestRenderSIEMTemplate(t *testing.T) {
	var buf bytes.Buffer
	dets := []analytics.BotnetDetection{{
		Pattern:     analytics.BotnetPatternProxyTunnel,
		Label:       "PROXY/TUNNEL: Аномальная прикладная задержка при низком RTT",
		Confidence:  analytics.BotnetConfidenceHigh,
		IPsInvolved: []string{"1.2.3.4"},
		CountryByIP: map[string]string{"1.2.3.4": "Indonesia"},
		Evidence:    []string{"событий с аномалией: 12", "средний RTT: 48 мс"},
	}}
	data := map[string]interface{}{
		"Prefix":       "test/",
		"Detections":   dets,
		"ProxyCount":   1,
		"SpoofCount":   0,
		"ProxyDetails": []warningLine{{Subject: "1.2.3.4 (Indonesia)", Facts: "событий с аномалией: 12; средний RTT: 48 мс"}},
		"SpoofDetails": nil,
	}
	if err := panelTmpl.ExecuteTemplate(&buf, "dashboard_siem_core", data); err != nil {
		t.Fatalf("execute dashboard_siem_core: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"1.2.3.4", "Indonesia", "событий с аномалией: 12"} {
		if !strings.Contains(out, want) {
			t.Fatalf("siem output missing %q (warning without facts?)", want)
		}
	}
}
