// internal/analytics/rollup_test.go
package analytics

import (
	"testing"
	"time"
)

// TestRollupTrapReportCoversWeek сторожит симметрию колец:
// ловушки обязаны агрегироваться и в часовых, и в суточных бакетах,
// иначе недельный и месячный отчёты остаются пустыми при живых сутках.
func TestRollupTrapReportCoversWeek(t *testing.T) {
	r := &Rollup{}
	now := time.Now().UTC()
	rec := EventRecord{
		StartTime:       now,
		StartTimeValid:  true,
		TarpitID:        "TARPIT:2222",
		TarpitIDValid:   true,
		IP:              "10.0.0.1",
		IPValid:         true,
		Country:         "France",
		ActualHoldSec:   30,
		ActualHoldValid: true,
		CloseReason:     "FIN",
	}
	r.Ingest(rec)

	for _, rng := range []ReportRange{Range24h, Range7d, Range30d} {
		rep := r.BuildTarpitReport(rng, 10)
		if len(rep) == 0 {
			t.Fatalf("trap report empty for range %s (bucket ring asymmetry?)", rng)
		}
		if rep[0].Port != "2222" || rep[0].Events != 1 || rep[0].HoldSumSec != 30 {
			t.Fatalf("unexpected trap report for %s: %+v", rng, rep[0])
		}
	}
}

// TestRollupStolenTime проверяет утилизированное время из бакетов по всем периодам.
// Главный источник плашек главной страницы обязан видеть каждое событие.
func TestRollupStolenTime(t *testing.T) {
	r := &Rollup{}
	rec := EventRecord{
		StartTime:       time.Now().UTC(),
		StartTimeValid:  true,
		ActualHoldSec:   42,
		ActualHoldValid: true,
	}
	r.Ingest(rec)

	st := r.BuildStolenTime()
	if st.HourSec != 42 || st.DaySec != 42 || st.WeekSec != 42 || st.MonthSec != 42 {
		t.Fatalf("stolen time mismatch across periods: %+v", st)
	}
}
