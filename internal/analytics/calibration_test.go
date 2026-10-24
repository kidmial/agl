// internal/analytics/calibration_test.go
// =====================================================================
// КОМПОНЕНТ: РЕГРЕССИОННЫЙ ТЕСТ КАЛИБРОВКИ АЛГОРИТМА 2 (TIME DRIFT)
// РОЛЬ: Доказать, что дрейф между StartTime и токеном близок к нулю
//
//	после калибровки v8.6.2.
//
// ИСТОРИЯ:
//
//	Раньше parseStartTime парсил беззонные строки через time.Parse,
//	который по умолчанию считает их UTC. tarpit_logger при этом пишет
//	локальное представление времени. Для Europe/Moscow это давало
//	систематический дрейф ровно в -3 часа (-10800000 мс).
//	После перехода на time.ParseInLocation(layout, str, time.Local)
//	дрейф вернулся к ожидаемым ~0 мс.
//
// =====================================================================
package analytics

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestTokenTimeDriftCalibrated — регрессионный тест калибровки.
//
// Сценарий:
//  1. Берём время атаки в локальной зоне сервера (как у нас в проде).
//  2. Генерируем токен так же, как это делает tarpit_logger.go.
//  3. Собираем сырое событие с тем же временем в StartTime.
//  4. Нормализуем и проверяем, что |дрейф| < 1 секунда.
//
// На старом коде (до калибровки) тест падает с дрейфом ≈ -10800000 мс
// для серверов в Европе (UTC+1/+2/+3). Это точный отпечаток бага.
func TestTokenTimeDriftCalibrated(t *testing.T) {
	// Константа калибровки должна быть true после v8.6.2.
	if !TokenTimeCalibrated {
		t.Fatalf("TokenTimeCalibrated = false, want true (calibration not applied)")
	}

	// 1. Время атаки в локальной зоне сервера.
	// Мы намеренно берём time.Local, а не time.UTC — это имитирует
	// реальный сценарий, где сервер живёт в не-UTC зоне.
	attackTime := time.Date(2026, 10, 24, 10, 31, 57, 123456789, time.Local)

	// 2. Генерируем токен так же, как tarpit_logger.go:
	//    fmt.Sprintf("18CDB%X_%d", entry.Timestamp.UnixNano(), entry.SourcePort)
	// UnixNano возвращает абсолютное время (UTC), поэтому в токене
	// лежит правильное UTC-время.
	sourcePort := uint16(49868)
	token := fmt.Sprintf("18CDB%X_%d", attackTime.UnixNano(), sourcePort)

	// 3. Собираем сырое событие через боевой парсер ParseLine.
	//
	// Почему НЕ конструируем RawEvent литералом:
	//   - внутренняя структура RawEvent (types.go) может меняться;
	//   - TSV-контракт 30 полей стабилен и является форензик-контрактом;
	//   - заодно проверяем, что парсер принимает строку без ошибок.
	//
	// StartTime записывается как локальное представление времени
	// (именно так это делает tarpit_logger: entry.Timestamp.Format(...)).
	tsvLine := strings.Join([]string{
		attackTime.Format("2006-01-02 15:04:05"), // локальное представление
		"TARPIT:2222",
		"10.0.0.1",
		"15",
		"11",
		"AGGRESSIVE_SCANNER",
		"France",
		"49868",
		token,
		"744", "10", "14480", "205", "0", "8", "0", "0", "7", "7", "FIN",
		"1", "178", "178", "178", "31684", "1", "0", "1", "0", "10361",
	}, "\t")

	raw, err := ParseLine(tsvLine, 1, "calibration_test")
	if err != nil {
		t.Fatalf("ParseLine failed on valid 30-field line: %v", err)
	}

	// 4. Нормализуем и проверяем дрейф.
	rec := NormalizeEvent(raw)

	if !rec.StartTimeValid {
		t.Fatalf("StartTimeValid = false, want true")
	}
	if !rec.TokenTimeDriftValid {
		t.Fatalf("TokenTimeDriftValid = false, want true")
	}

	// После калибровки дрейф должен быть близок к нулю.
	// Берём щедрый допуск в 1 секунду: парсинг и нормализация занимают
	// микросекунды, но в реальном проде между моментом генерации токена
	// и записью StartTime проходит несколько мс (очередь канала писателя).
	const maxAllowedDriftMs int64 = 1000
	if abs := abs64(rec.TokenTimeDriftMs); abs > maxAllowedDriftMs {
		t.Fatalf(
			"TokenTimeDriftMs = %d, want |drift| < %d\n"+
				"Возможные причины:\n"+
				"  - parseStartTime снова использует time.Parse вместо time.ParseInLocation;\n"+
				"  - TokenTimeCalibrated сбросился в false;\n"+
				"  - сломана логика вычитания в normalize.go.",
			rec.TokenTimeDriftMs, maxAllowedDriftMs,
		)
	}

	// 5. Бонус: убеждаемся, что извлечённый порт совпадает.
	if !rec.TokenClientPortValid || rec.TokenClientPort != sourcePort {
		t.Fatalf("TokenClientPort = %d, want %d", rec.TokenClientPort, sourcePort)
	}
}

// abs64 возвращает абсолютное значение int64.
func abs64(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}
