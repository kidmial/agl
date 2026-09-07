// i18n/i18n_test.go
package i18n

import (
	"reflect"
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
