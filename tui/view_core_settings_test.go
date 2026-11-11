// tui/view_core_settings_test.go
package tui

import (
	"strconv"
	"strings"
	"testing"

	"agl/config"
	"agl/i18n"
)

// ============================================================================
// РЕГРЕСС ФОРМАТА СПРАВКИ CORE_SETTINGS
// ============================================================================
// PURPOSE: глаголы Sprintf обязаны совпадать с типами аргументов.
// Исторический баг: DefaultVal (string) заменён на factoryVal (int),
// а глагол остался %s → в UI печаталось «%!s(int=2048)».
// go build такое пропускает, go vet ловит; тест закрепляет поведение.
// SIDE-EFFECTS: нет. ui.Init() не вызывается — терминал не нужен.
// ============================================================================

func TestFormatCoreHelpText_NoVerbMismatch(t *testing.T) {
	i18n.InitLanguage("ru") // заполняем i18n.T текстами словаря

	// Пустой словарь = тест прошёл бы вхолостую. Фиксируем это явно.
	if len(i18n.T.SysctlDocs) == 0 {
		t.Fatal("SysctlDocs пуст после InitLanguage — тест не имеет смысла")
	}

	for i, doc := range i18n.T.SysctlDocs {
		spec, ok := config.SysctlSpecByIndex(i)
		if !ok {
			t.Fatalf("строка %d: в словаре есть описание, в спеке нет параметра", i)
		}
		// curVal = Factory: заведомо валидное значение внутри границ.
		got := formatCoreHelpText(doc, spec.Factory, spec.Factory, spec.Min, spec.Max)

		// Артефакты неверного глагола: %!s(int=…), %!d(string=…), %!v(MISSING)
		if strings.Contains(got, "%!") {
			t.Errorf("строка %d (%s): артефакт форматирования в справке", i, spec.ParamName)
		}
		// Эталон обязан реально присутствовать числом, а не потеряться.
		if !strings.Contains(got, strconv.Itoa(spec.Factory)) {
			t.Errorf("строка %d (%s): эталон %d не отображён", i, spec.ParamName, spec.Factory)
		}
	}
}

// TestSysctlDocs_AlignedWithSpec — словарь и спека не разъезжаются.
// CONTRACT: в словаре 7 записей (rp_filter default не выведен в меню),
// в спеке 8. Порядок и имена первых семи обязаны совпадать.
func TestSysctlDocs_AlignedWithSpec(t *testing.T) {
	i18n.InitLanguage("ru")
	specs := config.SysctlSpec()

	if len(i18n.T.SysctlDocs) == 0 {
		t.Fatal("SysctlDocs пуст после InitLanguage")
	}
	if len(i18n.T.SysctlDocs) > len(specs) {
		t.Fatalf("словарь длиннее спеки: %d > %d", len(i18n.T.SysctlDocs), len(specs))
	}
	for i, doc := range i18n.T.SysctlDocs {
		// TrimSpace страховует от висячих пробелов в словарях.
		if strings.TrimSpace(doc.ParamName) != specs[i].ParamName {
			t.Errorf("строка %d: словарь=%q спека=%q",
				i, strings.TrimSpace(doc.ParamName), specs[i].ParamName)
		}
	}
}
