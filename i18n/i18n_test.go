// i18n/i18n_test.go
package i18n

import (
	"reflect"
	"strings"
	"testing"
)

func TestTranslationsParity(t *testing.T) {
	// 1. Проверка паритета ключей событий в LogLanguages
	ruEvents := LogLanguages["ru"].Events
	enEvents := LogLanguages["en"].Events

	if len(ruEvents) != len(enEvents) {
		t.Errorf("Log events parity failed: RU has %d keys, EN has %d keys", len(ruEvents), len(enEvents))
	}

	for code, ruDesc := range ruEvents {
		enDesc, ok := enEvents[code]
		if !ok {
			t.Errorf("Log event %q missing in EN translation", code)
		}
		if ruDesc == "" {
			t.Errorf("Log event %q has empty description in RU", code)
		}
		if enDesc == "" {
			t.Errorf("Log event %q has empty description in EN", code)
		}
	}

	for code, enDesc := range enEvents {
		if _, ok := ruEvents[code]; !ok {
			t.Errorf("Log event %q missing in RU translation", code)
		}
		if enDesc == "" {
			t.Errorf("Log event %q has empty description in EN", code)
		}
	}

	// 2. Проверка полноты UITranslation через reflection
	checkUITranslation := func(lang string, tr UITranslation) {
		v := reflect.ValueOf(tr)
		typeOfTr := v.Type()

		for i := 0; i < v.NumField(); i++ {
			field := v.Field(i)
			fieldName := typeOfTr.Field(i).Name

			if field.Kind() == reflect.String {
				if field.String() == "" {
					t.Errorf("UI translation %q: field %q is empty", lang, fieldName)
				}
			} else if field.Kind() == reflect.Slice {
				// Проверяем SysctlDocs
				if field.Len() == 0 {
					t.Errorf("UI translation %q: slice %q is empty", lang, fieldName)
				} else {
					for j := 0; j < field.Len(); j++ {
						docVal := field.Index(j)
						for k := 0; k < docVal.NumField(); k++ {
							docField := docVal.Field(k)
							docFieldName := docVal.Type().Field(k).Name
							if docField.Kind() == reflect.String && docField.String() == "" {
								t.Errorf("UI translation %q: SysctlDocs[%d].%q is empty", lang, j, docFieldName)
							}
						}
					}
				}
			}
		}
	}

	for lang, tr := range UILanguages {
		checkUITranslation(lang, tr)
	}
}

// TestTrEventFallback проверяет контракт А.2: при отсутствии кода или пустом
// шаблоне возвращается сам машинный код; строка журнала никогда не пуста.
func TestTrEventFallback(t *testing.T) {
	savedT, savedL := T, L
	defer func() { T, L = savedT, savedL }()
	InitLanguage("ru")

	// 1. Несуществующий код -> возвращается сам код, не пустая строка.
	if got := TrEvent("nonexistent.code"); got != "nonexistent.code" {
		t.Errorf("TrEvent fallback broken: got %q, want %q", got, "nonexistent.code")
	}
	// 2. TrEventf без аргументов на несуществующем коде -> сам код, без хвостов Sprintf.
	if got := TrEventf("nonexistent.code"); got != "nonexistent.code" {
		t.Errorf("TrEventf fallback broken: got %q, want %q", got, "nonexistent.code")
	}
	// 3. Пустой шаблон в Events -> тоже фолбэк в код (ветка ok && val != "").
	L.Events["test.empty_template"] = ""
	if got := TrEvent("test.empty_template"); got != "test.empty_template" {
		t.Errorf("TrEvent empty-template fallback broken: got %q", got)
	}
	delete(L.Events, "test.empty_template")

	// 4. TrEventf форматирует аргументы по локализованному шаблону (без двойного Sprintf).
	got := TrEventf("watcher.idle_half_warning", 5, 7)
	if got == "watcher.idle_half_warning" || !strings.Contains(got, "5") || !strings.Contains(got, "7") {
		t.Errorf("TrEventf formatting broken: got %q", got)
	}
	if strings.Contains(got, "%!") {
		t.Errorf("TrEventf produced Sprintf artifact: %q", got)
	}

	// 5. Защита в глубину (TD-hotfix): TrEventf с аргументами на несуществующем коде
	// возвращает код ДОСЛОВНО, без хвоста %!(EXTRA ...).
	got = TrEventf("nonexistent.code", 42, "arg")
	if got != "nonexistent.code" {
		t.Errorf("TrEventf fallback with args broken: got %q, want %q", got, "nonexistent.code")
	}
	if strings.Contains(got, "%!") {
		t.Errorf("TrEventf fallback produced Sprintf artifact: %q", got)
	}
}
