// internal/analytics/botnet.go
// =====================================================================
// КОМПОНЕНТ: ДЕТЕКТОР БОТНЕТ-ПАТТЕРНОВ
// РОЛЬ: Выявление макро-паттернов распределённых атак.
// ПРИНЦИП:
//   - каждый паттерн имеет уровень уверенности;
//   - каждый паттерн имеет доказательства;
//   - используем скользящие временные окна;
//   - не сканируем весь буфер без необходимости.
//
// =====================================================================
package analytics

import (
	"fmt"
	"sort"
	"time"
)

// Коды ботнет-паттернов.
const (
	BotnetPatternNone       = "NONE"
	BotnetPatternSyncWave   = "SYNC_WAVE"
	BotnetPatternCarousel   = "CAROUSEL"
	BotnetPatternWaveJitter = "WAVE_JITTER"
	BotnetPatternCellProbe  = "CELL_PROBE"
)

// Уровни уверенности ботнет-детекции.
const (
	BotnetConfidenceLow      = "LOW"
	BotnetConfidenceMedium   = "MEDIUM"
	BotnetConfidenceHigh     = "HIGH"
	BotnetConfidenceCritical = "CRITICAL"
)

// Пороги детекции (из ТЗ).
const (
	// SyncWaveMinIPs — минимум уникальных IP в секунду для залпа.
	SyncWaveMinIPs = 5

	// SyncWaveStackHomogeneity — процент однородности стека (80%).
	SyncWaveStackHomogeneity = 0.80

	// CarouselMinCycles — минимум циклов для детекции карусели.
	CarouselMinCycles = 3

	// CarouselCycleToleranceMs — допуск по циклу в миллисекундах.
	CarouselCycleToleranceMs = 2000

	// CellProbeMinCells — минимум ячеек для детекции разведки.
	CellProbeMinCells = 2

	// CellProbeEscalationThreshold — порог эскалации (рост числа ячеек).
	CellProbeEscalationThreshold = 2
)

// Временные окна наблюдения.
var (
	Window1Sec  = 1 * time.Second
	Window5Sec  = 5 * time.Second
	Window30Sec = 30 * time.Second
	Window1Min  = 1 * time.Minute
	Window5Min  = 5 * time.Minute
	Window1Hour = 1 * time.Hour
)

// BotnetDetection описывает результат детекции одного паттерна.
type BotnetDetection struct {
	Pattern     string    `json:"pattern"`
	Label       string    `json:"label"`
	Confidence  string    `json:"confidence"`
	IPsInvolved []string  `json:"ips_involved"`
	StartTime   time.Time `json:"start_time"`
	EndTime     time.Time `json:"end_time"`
	Evidence    []string  `json:"evidence"`
	Reasons     []string  `json:"reasons"`
}

// BotnetDetector выполняет детекцию ботнет-паттернов.
type BotnetDetector struct {
	store   *Store
	manager *ProfileManager
}

// NewBotnetDetector создаёт детектор ботнет-паттернов.
func NewBotnetDetector(store *Store, manager *ProfileManager) *BotnetDetector {
	return &BotnetDetector{
		store:   store,
		manager: manager,
	}
}

// DetectSynchronousWave детектирует синхронный залп.
//
// Алгоритм (из ТЗ):
// 1. Группируем события по секундам.
// 2. Ищем секунды с >5 уникальными IP.
// 3. Проверяем однородность стека (MSS + RcvSpace).
// 4. Если >80% IP имеют одинаковый стек → SYNC_WAVE.
func (d *BotnetDetector) DetectSynchronousWave(window time.Duration) []BotnetDetection {
	if d == nil || d.store == nil {
		return []BotnetDetection{}
	}

	if window <= 0 {
		window = Window5Min
	}

	events := d.store.SnapshotChronological()
	if len(events) == 0 {
		return []BotnetDetection{}
	}

	// v8.6.2: анализируем ТОЛЬКО окно наблюдения, а не всю историю буфера.
	now := time.Now().UTC()
	cutoff := now.Add(-window)
	filtered := make([]EventRecord, 0, len(events))
	for _, event := range events {
		if event.StartTimeValid && !event.StartTime.Before(cutoff) {
			filtered = append(filtered, event)
		}
	}
	events = filtered

	// Группируем события по секундам.
	bySecond := make(map[int64][]EventRecord)
	for _, event := range events {
		if !event.StartTimeValid {
			continue
		}
		second := event.StartTime.Unix()
		bySecond[second] = append(bySecond[second], event)
	}

	var detections []BotnetDetection

	// Проходим по каждой секунде.
	for second, secondEvents := range bySecond {
		// Считаем уникальные IP.
		uniqueIPs := make(map[string]struct{})
		for _, event := range secondEvents {
			if event.IPValid && event.IP != "" {
				uniqueIPs[event.IP] = struct{}{}
			}
		}

		// Если меньше порога, пропускаем.
		if len(uniqueIPs) < SyncWaveMinIPs {
			continue
		}

		// Проверяем однородность стека.
		stackCounts := make(map[StackFingerprint]int)
		for _, event := range secondEvents {
			fp, ok := StackFingerprintFromEvent(event)
			if !ok {
				continue
			}
			stackCounts[fp]++
		}

		// Ищем доминирующий стек.
		var maxStack StackFingerprint
		var maxCount int
		for stack, count := range stackCounts {
			if count > maxCount {
				maxStack = stack
				maxCount = count
			}
		}

		// Проверяем процент однородности.
		totalWithStack := 0
		for _, count := range stackCounts {
			totalWithStack += count
		}

		if totalWithStack == 0 {
			continue
		}

		homogeneity := float64(maxCount) / float64(totalWithStack)
		if homogeneity < SyncWaveStackHomogeneity {
			continue
		}

		// Формируем детекцию.
		startTime := time.Unix(second, 0).UTC()
		endTime := startTime.Add(time.Second)

		ips := make([]string, 0, len(uniqueIPs))
		for ip := range uniqueIPs {
			ips = append(ips, ip)
		}
		sort.Strings(ips)

		detection := BotnetDetection{
			Pattern:     BotnetPatternSyncWave,
			Label:       "СИНХРОННЫЙ ЗАЛП: Централизованный C2-удар",
			Confidence:  BotnetConfidenceHigh,
			IPsInvolved: ips,
			StartTime:   startTime,
			EndTime:     endTime,
			Evidence: []string{
				formatString("%d уникальных IP в одну секунду", len(uniqueIPs)),
				formatString("однородность стека %.0f%%", homogeneity*100),
				formatString("MSS=%d RcvSpace=%d", maxStack.MSS, maxStack.RcvSpace),
			},
			Reasons: []string{
				"все боты одновременно открыли сокеты по команде C2",
			},
		}

		detections = append(detections, detection)
	}

	return detections
}

// DetectCarousel детектирует карусель/эстафету.
//
// Алгоритм:
// 1. Ищем профили с циклическими интервалами между событиями.
// 2. Если интервалы стабильны (в пределах допуска) → CAROUSEL.
//
// ВАЖНО (v8.6.2, hotfix CPU 100%):
//   - снапшот буфера берётся ОДИН раз на всю детекцию;
//   - число профилей ограничено 200 самыми свежими.
func (d *BotnetDetector) DetectCarousel(window time.Duration) []BotnetDetection {
	if d == nil || d.manager == nil || d.store == nil {
		return []BotnetDetection{}
	}

	if window <= 0 {
		window = Window1Hour
	}

	profiles := d.manager.ProfilesSnapshot(200)
	if len(profiles) == 0 {
		return []BotnetDetection{}
	}

	snapshot := d.store.SnapshotChronological()
	if len(snapshot) == 0 {
		return []BotnetDetection{}
	}

	var detections []BotnetDetection

	for _, profile := range profiles {
		events := d.getProfileEvents(profile.ID, window, snapshot)
		if len(events) < CarouselMinCycles {
			continue
		}

		sort.Slice(events, func(i, j int) bool {
			return events[i].StartTime.Before(events[j].StartTime)
		})

		intervals := make([]time.Duration, 0, len(events)-1)
		for i := 1; i < len(events); i++ {
			interval := events[i].StartTime.Sub(events[i-1].StartTime)
			intervals = append(intervals, interval)
		}

		if len(intervals) < CarouselMinCycles-1 {
			continue
		}

		if d.isCyclic(intervals) {
			ips := make([]string, 0, len(profile.IPCounts))
			for ip := range profile.IPCounts {
				ips = append(ips, ip)
			}
			sort.Strings(ips)

			avgInterval := d.averageInterval(intervals)

			detection := BotnetDetection{
				Pattern:     BotnetPatternCarousel,
				Label:       "КАРУСЕЛЬ: Эстафетная распределённая разведка",
				Confidence:  BotnetConfidenceMedium,
				IPsInvolved: ips,
				StartTime:   events[0].StartTime,
				EndTime:     events[len(events)-1].StartTime,
				Evidence: []string{
					formatString("%d событий с циклическими интервалами", len(events)),
					formatString("средний интервал %v", avgInterval),
					formatString("%d уникальных IP", len(ips)),
				},
				Reasons: []string{
					"ботнет обходит IPS-правила через ротацию IP",
				},
			}

			detections = append(detections, detection)
		}
	}

	return detections
}

// DetectWaveJitter детектирует волновой джиттер.
//
// Алгоритм:
// 1. Делим окно на интервалы (например, 1 минута).
// 2. Ищем чередование активных и пассивных интервалов.
// 3. Если есть паттерн "активность → затишье → активность" → WAVE_JITTER.
func (d *BotnetDetector) DetectWaveJitter(window time.Duration) []BotnetDetection {
	if d == nil || d.store == nil {
		return []BotnetDetection{}
	}

	if window <= 0 {
		window = Window1Hour
	}

	events := d.store.SnapshotChronological()
	if len(events) == 0 {
		return []BotnetDetection{}
	}

	// v8.6.2: анализируем ТОЛЬКО окно наблюдения, а не всю историю буфера.
	now := time.Now().UTC()
	cutoff := now.Add(-window)
	filtered := make([]EventRecord, 0, len(events))
	for _, event := range events {
		if event.StartTimeValid && !event.StartTime.Before(cutoff) {
			filtered = append(filtered, event)
		}
	}
	events = filtered

	// Делим окно на минутные интервалы.
	intervalSize := time.Minute
	byInterval := make(map[int64]int)

	for _, event := range events {
		if !event.StartTimeValid {
			continue
		}
		interval := event.StartTime.Unix() / int64(intervalSize.Seconds())
		byInterval[interval]++
	}

	if len(byInterval) < 3 {
		return []BotnetDetection{}
	}

	// Сортируем интервалы по времени.
	intervals := make([]int64, 0, len(byInterval))
	for interval := range byInterval {
		intervals = append(intervals, interval)
	}
	sort.Slice(intervals, func(i, j int) bool {
		return intervals[i] < intervals[j]
	})

	// Ищем паттерн чередования.
	var waveStart, waveEnd time.Time
	var activeCount, quietCount int
	var ipsInvolved []string

	threshold := 5 // минимум событий для "активного" интервала

	for i := 0; i < len(intervals)-2; i++ {
		count1 := byInterval[intervals[i]]
		count2 := byInterval[intervals[i+1]]
		count3 := byInterval[intervals[i+2]]

		// Паттерн: активность → затишье → активность.
		if count1 >= threshold && count2 < threshold && count3 >= threshold {
			waveStart = time.Unix(intervals[i]*int64(intervalSize.Seconds()), 0).UTC()
			waveEnd = time.Unix(intervals[i+2]*int64(intervalSize.Seconds()), 0).UTC()
			activeCount = count1 + count3
			quietCount = count2

			// Собираем IP из активных интервалов.
			ipSet := make(map[string]struct{})
			for _, event := range events {
				if !event.StartTimeValid {
					continue
				}
				eventInterval := event.StartTime.Unix() / int64(intervalSize.Seconds())
				if eventInterval == intervals[i] || eventInterval == intervals[i+2] {
					if event.IPValid && event.IP != "" {
						ipSet[event.IP] = struct{}{}
					}
				}
			}

			for ip := range ipSet {
				ipsInvolved = append(ipsInvolved, ip)
			}
			sort.Strings(ipsInvolved)

			break
		}
	}

	if activeCount == 0 {
		return []BotnetDetection{}
	}

	detection := BotnetDetection{
		Pattern:     BotnetPatternWaveJitter,
		Label:       "ВОЛНОВОЙ ДЖИТТЕР: Интеллектуальный ботнет с эмуляцией трафика",
		Confidence:  BotnetConfidenceMedium,
		IPsInvolved: ipsInvolved,
		StartTime:   waveStart,
		EndTime:     waveEnd,
		Evidence: []string{
			formatString("паттерн: активность(%d) → затишье(%d) → активность(%d)", activeCount/2, quietCount, activeCount/2),
			formatString("%d уникальных IP", len(ipsInvolved)),
		},
		Reasons: []string{
			"ботнет симулирует естественный трафик для обхода детекта",
		},
	}

	return []BotnetDetection{detection}
}

// DetectCellProbe детектирует ячеечную разведку.
//
// Алгоритм:
// 1. Ищем профили с постепенным ростом числа связанных IP.
// 2. Если рост превышает порог → CELL_PROBE.
//
// ВАЖНО (v8.6.2, hotfix CPU 100%):
//   - снапшот буфера берётся ОДИН раз на всю детекцию;
//   - число профилей ограничено 200 самыми свежими.
func (d *BotnetDetector) DetectCellProbe(window time.Duration) []BotnetDetection {
	if d == nil || d.manager == nil || d.store == nil {
		return []BotnetDetection{}
	}

	if window <= 0 {
		window = Window1Hour
	}

	profiles := d.manager.ProfilesSnapshot(200)
	if len(profiles) == 0 {
		return []BotnetDetection{}
	}

	snapshot := d.store.SnapshotChronological()
	if len(snapshot) == 0 {
		return []BotnetDetection{}
	}

	var detections []BotnetDetection

	for _, profile := range profiles {
		events := d.getProfileEvents(profile.ID, window, snapshot)
		if len(events) < CellProbeMinCells {
			continue
		}

		sort.Slice(events, func(i, j int) bool {
			return events[i].StartTime.Before(events[j].StartTime)
		})

		windowSize := Window5Min
		cellsByWindow := make(map[int64]map[string]struct{})

		for _, event := range events {
			if !event.StartTimeValid || !event.IPValid {
				continue
			}
			windowKey := event.StartTime.Unix() / int64(windowSize.Seconds())
			if cellsByWindow[windowKey] == nil {
				cellsByWindow[windowKey] = make(map[string]struct{})
			}
			cellsByWindow[windowKey][event.IP] = struct{}{}
		}

		windows := make([]int64, 0, len(cellsByWindow))
		for w := range cellsByWindow {
			windows = append(windows, w)
		}
		sort.Slice(windows, func(i, j int) bool {
			return windows[i] < windows[j]
		})

		if len(windows) < 2 {
			continue
		}

		firstCells := len(cellsByWindow[windows[0]])
		lastCells := len(cellsByWindow[windows[len(windows)-1]])

		if lastCells-firstCells >= CellProbeEscalationThreshold {
			ips := make([]string, 0, len(profile.IPCounts))
			for ip := range profile.IPCounts {
				ips = append(ips, ip)
			}
			sort.Strings(ips)

			detection := BotnetDetection{
				Pattern:     BotnetPatternCellProbe,
				Label:       "ЯЧЕЕЧНАЯ РАЗВЕДКА: Постепенное наращивание ботнет-ячеек",
				Confidence:  BotnetConfidenceMedium,
				IPsInvolved: ips,
				StartTime:   events[0].StartTime,
				EndTime:     events[len(events)-1].StartTime,
				Evidence: []string{
					formatString("рост числа ячеек: %d → %d", firstCells, lastCells),
					formatString("%d уникальных IP", len(ips)),
				},
				Reasons: []string{
					"ботнет начинает с 1-2 ячеек и наращивает при успехе",
				},
			}

			detections = append(detections, detection)
		}
	}

	return detections
}

// DetectAll выполняет детекцию всех ботнет-паттернов.
func (d *BotnetDetector) DetectAll(window time.Duration) []BotnetDetection {
	if d == nil {
		return []BotnetDetection{}
	}

	if window <= 0 {
		window = Window1Hour
	}

	var allDetections []BotnetDetection

	allDetections = append(allDetections, d.DetectSynchronousWave(window)...)
	allDetections = append(allDetections, d.DetectCarousel(window)...)
	allDetections = append(allDetections, d.DetectWaveJitter(window)...)
	allDetections = append(allDetections, d.DetectCellProbe(window)...)

	return allDetections
}

// getProfileEvents возвращает события профиля за заданное окно.
//
// ВАЖНО (v8.6.2, hotfix CPU 100%):
//   - снапшот событий принимается параметром: буфер больше не копируется
//     на каждый профиль (ранее P копий буфера);
//   - ProfileByID вызывается ОДИН раз до цикла: cloneProfile дорог,
//     ранее было P×E глубоких клонирований — причина зависания.
func (d *BotnetDetector) getProfileEvents(profileID string, window time.Duration, events []EventRecord) []EventRecord {
	if d == nil || d.manager == nil {
		return []EventRecord{}
	}

	// Достаём профиль ОДИН раз до цикла по событиям.
	profile, ok := d.manager.ProfileByID(profileID)
	if !ok {
		return []EventRecord{}
	}

	if len(events) == 0 {
		return []EventRecord{}
	}

	now := time.Now().UTC()
	cutoff := now.Add(-window)

	var profileEvents []EventRecord
	for _, event := range events {
		if !event.StartTimeValid {
			continue
		}
		if event.StartTime.Before(cutoff) {
			continue
		}

		// Проверяем принадлежность события профилю по IP.
		if _, exists := profile.IPCounts[event.IP]; exists {
			profileEvents = append(profileEvents, event)
		}
	}

	return profileEvents
}

// isCyclic проверяет, являются ли интервалы циклическими.
func (d *BotnetDetector) isCyclic(intervals []time.Duration) bool {
	if len(intervals) < 2 {
		return false
	}

	avg := d.averageInterval(intervals)
	tolerance := time.Duration(CarouselCycleToleranceMs) * time.Millisecond

	cyclicCount := 0
	for _, interval := range intervals {
		diff := interval - avg
		if diff < 0 {
			diff = -diff
		}
		if diff <= tolerance {
			cyclicCount++
		}
	}

	// Если >70% интервалов в пределах допуска, считаем циклическим.
	return float64(cyclicCount)/float64(len(intervals)) >= 0.70
}

// averageInterval вычисляет средний интервал.
func (d *BotnetDetector) averageInterval(intervals []time.Duration) time.Duration {
	if len(intervals) == 0 {
		return 0
	}

	var sum time.Duration
	for _, interval := range intervals {
		sum += interval
	}

	return sum / time.Duration(len(intervals))
}

// formatString — вспомогательная функция для форматирования.
func formatString(format string, args ...interface{}) string {
	return fmt.Sprintf(format, args...)
}
