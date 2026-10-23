// internal/analytics/pipeline_test.go
// =====================================================================
// КОМПОНЕНТ: DRY RUN ТЕСТ КОНВЕЙЕРА + АЛГОРИТМ 1 (СКЛЕЙКА IP)
// =====================================================================
package analytics

import (
	"strings"
	"testing"
)

func TestPipelineDryRun(t *testing.T) {
	// Сбрасываем состояние перед тестом
	ResetEngine()

	// Инициализируем движок (вызов Start с пустым путём — для теста OK)
	Start("")

	GlobalStore.Clear()

	// Сессия 1: IP 94.231.206.131, стек MSS=744, RcvSpace=14480, RTT=205
	mockLogLine1 := strings.Join([]string{
		"1991-09-18 10:00:00",
		"TARPIT:2222",
		"94.231.206.131",
		"15", "15", "PASSIVE_SCANNER", "France", "55235",
		"1A1630DA100_55235",
		"744", "10", "14480", "205", "0", "8", "0", "0", "7", "7",
		"TIMEOUT",
		"1", "178", "178", "178", "31684",
		"1", "0", "0", "0", "10361",
	}, "\t")

	// Сессия 2: ДРУГОЙ IP 185.220.101.42, НО ТОТ ЖЕ СТЕК и RTT=207 (дельта 2 мс)
	// Это имитация смены IP ботом через прокси.
	mockLogLine2 := strings.Join([]string{
		"1991-09-18 10:02:00",
		"TARPIT:2222",
		"185.220.101.42",
		"15", "11", "AGGRESSIVE_SCANNER", "Germany", "55236",
		"1A1630DA5C0_55236",
		"744", "10", "14480", "207", "0", "8", "0", "0", "7", "7",
		"FIN",
		"2", "300", "150", "150", "45000",
		"1", "2", "0", "0", "8000",
	}, "\t")

	// Обрабатываем Сессию 1
	raw1, _ := ParseLine(mockLogLine1, 1, "test.log")
	norm1 := GlobalStore.AddRaw(raw1)
	if GlobalManager != nil {
		GlobalManager.ObserveAndCorrelate(norm1)
	}

	// Обрабатываем Сессию 2
	raw2, _ := ParseLine(mockLogLine2, 2, "test.log")
	norm2 := GlobalStore.AddRaw(raw2)
	if GlobalManager != nil {
		GlobalManager.ObserveAndCorrelate(norm2)
	}

	// Проверка: должен быть ОДИН профиль с EventCount=2 и двумя IP
	profiles := GlobalManager.ProfilesSnapshot(10)
	if len(profiles) == 0 {
		t.Fatalf("ОШИБКА: профиль не создан")
	}

	// Ищем активный профиль (не MERGED)
	var activeProfile *ActorProfile
	mergedCount := 0
	for i := range profiles {
		if profiles[i].State == ProfileStateActive {
			activeProfile = &profiles[i]
		}
		if profiles[i].State == ProfileStateMerged {
			mergedCount++
		}
	}

	if activeProfile == nil {
		t.Fatalf("ОШИБКА: активный профиль не найден")
	}

	// Проверяем склейку: EventCount должен быть 2
	if activeProfile.EventCount != 2 {
		t.Errorf("ОШИБКА СКЛЕЙКИ: ожидалось 2 события, получено %d", activeProfile.EventCount)
	}

	// Проверяем, что оба IP в профиле
	if activeProfile.UniqueIPCount() != 2 {
		t.Errorf("ОШИБКА СКЛЕЙКИ: ожидалось 2 уникальных IP, получено %d", activeProfile.UniqueIPCount())
	}

	// Проверяем, что есть слитый профиль
	if mergedCount == 0 {
		t.Errorf("ОШИБКА СКЛЕЙКИ: не найден профиль в состоянии MERGED")
	}

	// =====================================================================
	// ВИЗУАЛЬНЫЙ ОТЧЁТ
	// =====================================================================
	t.Log("\n====================================================================")
	t.Log("✅ АЛГОРИТМ 1: СКЛЕЙКА IP УСПЕШНО РАБОТАЕТ")
	t.Log("====================================================================")
	t.Logf("🔍 АКТИВНЫЙ ПРОФИЛЬ (ID: %s)", activeProfile.ID)
	t.Logf("   ├─ Событий: %d | Уник. IP: %d", activeProfile.EventCount, activeProfile.UniqueIPCount())
	t.Logf("   ├─ Стек: MSS=%d, RcvSpace=%d", activeProfile.Stack.MSS, activeProfile.Stack.RcvSpace)
	t.Logf("   ├─ IP в профиле: %v", activeProfile.TopIPs(10))
	t.Logf("   ├─ Слитых профилей: %d", mergedCount)
	t.Logf("   └─ Уверенность: %d%% | Риск: %s", activeProfile.Confidence, activeProfile.FalsePositiveRisk)
	t.Log("====================================================================\n")
}
