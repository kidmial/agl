// internal/analytics/ingest_test.go
// =====================================================================
// КОМПОНЕНТ: РЕГРЕССИОННЫЕ ТЕСТЫ ИНЖЕСТА
// РОЛЬ: Доказать, что хвост лога читается непрерывно, в том числе
//
//	после достижения EOF, и что Stop-канал реально останавливает горутину.
//
// ПРИНЦИП:
//   - тест изолирован: свой временный файл, свой Store, свой Manager;
//   - таймауты щедрые (3 сек), чтобы тест не флапал на медленных машинах;
//   - после завершения состояние движка сбрасывается через ResetEngine.
//
// ИСТОРИЯ (регессия):
//
//	bufio.Scanner в Go при достижении EOF ставит внутренний флаг done=true,
//	и все последующие Scan() сразу возвращают false, даже если писатель
//	дописал новые строки. Без пересоздания сканера инжест "засыпал навсегда".
//	Этот тест падает на старом коде и проходит на исправленном.
//
// =====================================================================
package analytics

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// makeTestLine собирает валидную 30-польную TSV-строку для парсера.
//
// Аргументы:
//   - ip: IP-адрес актора;
//   - port: исходный порт (строкой);
//   - ts: время начала сессии в формате лога.
//
// Возврат: строка, полностью соответствующая формату tarpit_report.log.
func makeTestLine(ip, port, ts string) string {
	return strings.Join([]string{
		ts,                              // [0]  StartTime
		"TARPIT:2222",                   // [1]  TarpitID
		ip,                              // [2]  IP
		"15",                            // [3]  PlannedHold
		"11",                            // [4]  ActualHold
		"AGGRESSIVE_SCANNER",            // [5]  Behavior
		"France",                        // [6]  Country
		port,                            // [7]  SourcePort
		"18CDB18DF580516D22337_" + port, // [8]  Token
		"744",                           // [9]  MSS
		"10",                            // [10] SndCwnd
		"14480",                         // [11] RcvSpace
		"205",                           // [12] RTT
		"0",                             // [13] TotalRetrans
		"8",                             // [14] State
		"0",                             // [15] Retransmits
		"0",                             // [16] Probes
		"7",                             // [17] SndWscale
		"7",                             // [18] RcvWscale
		"FIN",                           // [19] CloseReason
		"1",                             // [20] IATCount
		"178",                           // [21] IATSumMs
		"178",                           // [22] IATMinMs
		"178",                           // [23] IATMaxMs
		"31684",                         // [24] IATSumSqMs
		"1",                             // [25] ReadTiny
		"0",                             // [26] ReadSmall
		"1",                             // [27] ReadMedium
		"0",                             // [28] ReadLarge
		"10361",                         // [29] WriteLatencyMs
	}, "\t")
}

// appendLine дописывает одну строку в конец файла лога (имитация писателя капкана).
func appendLine(t *testing.T, path, line string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("cannot open log for append: %v", err)
	}
	defer f.Close()
	if _, err := f.WriteString(line + "\n"); err != nil {
		t.Fatalf("cannot append line: %v", err)
	}
}

// waitForEvents ждёт, пока в GlobalStore накопится не менее want событий.
//
// Почему polling, а не каналы: инжест не сигнализирует о каждом событии,
// он просто пишет в store. Polling с дедлайном — честная проверка снаружи.
func waitForEvents(t *testing.T, want int64, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if GlobalStore.Counters().TotalEvents >= want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %d events, got %d", want, GlobalStore.Counters().TotalEvents)
}

// waitForIngestStart ждёт, пока инжест откроет файл и зафиксирует путь.
//
// ПОЧЕМУ ЭТО КРИТИЧНО: StartIngest запускает горутину асинхронно.
// Если дописать строки до того, как горутина сделает seek to end,
// они окажутся ПОЗАДИ оффсета, и хвостовое чтение их никогда не увидит
// (их смог бы прочитать только rebuild). В тесте с пустым файлом
// это гарантированный фейк-фейл первой фазы.
//
// Сигнал готовности: SetIngestPath вызывается в StartIngest сразу
// после успешного seek и виден в Diagnostics() как ingest_started.
func waitForIngestStart(t *testing.T, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if started, ok := GlobalStore.Diagnostics()["ingest_started"].(bool); ok && started {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for ingest to start (%v)", timeout)
}

// TestIngestContinuesAfterEOF — регрессионный тест хвостового чтения.

// TestIngestContinuesAfterEOF — регрессионный тест хвостового чтения.
//
// Сценарий:
//  1. Пустой файл, инжест стартует и встаёт в конец (seek to end).
//  2. Дописываем 2 строки — инжест обязан их прочитать (базовый tail).
//  3. Ждём несколько poll-циклов, чтобы сканер гарантированно упёрся в EOF.
//  4. Дописываем ещё 2 строки ПОСЛЕ EOF — инжест обязан прочитать и их.
//     На старом коде (без пересоздания scanner) тест падает именно здесь.
//  5. Закрываем Stop-канал и дописываем 1 строку — счётчик обязан замереть.
//     Это поведенческое доказательство, что горутина реально завершилась.
func TestIngestContinuesAfterEOF(t *testing.T) {
	// 1. Изолированное окружение: временный файл и свежие глобальные структуры.
	path := filepath.Join(t.TempDir(), "tarpit_report.log")
	if err := os.WriteFile(path, []byte(""), 0644); err != nil {
		t.Fatalf("cannot create temp log: %v", err)
	}

	ResetEngine()
	GlobalStore = NewStore(100)
	GlobalManager = NewProfileManager(GlobalStore)
	defer ResetEngine()

	// 2. Старт инжеста с коротким poll-интервалом и Stop-каналом.
	stop := make(chan struct{})
	StartIngest(IngestConfig{
		LogPath:      path,
		PollInterval: 50 * time.Millisecond,
		Stop:         stop,
	})

	// 2.5. Синхронизация: дожидаемся, пока инжест откроет файл и встанет
	// в конец (seek to end). Без этого первые append'ы опережают seek
	// и оказываются позади оффсета — гонка, описанная в waitForIngestStart.
	waitForIngestStart(t, 3*time.Second)

	// 3. Базовый tail: первые 2 строки доходят до store.
	appendLine(t, path, makeTestLine("10.0.0.1", "40001", "2026-10-22 10:00:00"))
	appendLine(t, path, makeTestLine("10.0.0.2", "40002", "2026-10-22 10:00:05"))
	waitForEvents(t, 2, 3*time.Second)

	// 4. Даём сканеру гарантированно достичь EOF (несколько poll-циклов).
	//    Именно здесь старый bufio.Scanner ставил done=true навсегда.
	time.Sleep(300 * time.Millisecond)

	// 5. РЕГРЕССИЯ: строки, дописанные ПОСЛЕ EOF, обязаны дойти до store.
	appendLine(t, path, makeTestLine("10.0.0.3", "40003", "2026-10-22 10:00:10"))
	appendLine(t, path, makeTestLine("10.0.0.4", "40004", "2026-10-22 10:00:15"))
	waitForEvents(t, 4, 3*time.Second)

	// 6. Stop-канал: после закрытия горутина выходит и больше не читает.
	close(stop)
	time.Sleep(200 * time.Millisecond)
	appendLine(t, path, makeTestLine("10.0.0.5", "40005", "2026-10-22 10:00:20"))
	time.Sleep(300 * time.Millisecond)

	if got := GlobalStore.Counters().TotalEvents; got != 4 {
		t.Fatalf("ingest did not stop after Stop signal: events = %d, want 4", got)
	}
}
