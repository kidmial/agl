// internal/analytics/hourstat.go
// =====================================================================
// КОМПОНЕНТ: ЧАСОВАЯ СТАТИСТИКА
// РОЛЬ: Агрегация нормализованных событий по часовым срезам.
// ПРИНЦИП:
//   - считаем только события с валидным временем;
//   - битые события учитываем как показатель качества данных;
//   - уникальные сущности считаем отдельно от общего числа событий;
//   - топ-списки ограничиваем, чтобы веб-панель не тонула в данных.
//
// =====================================================================
package analytics

import (
	"agl/config"
	"fmt"
	"sort"
	"time"
)

// DefaultTopLimit задаёт размер топ-списков по умолчанию.
//
// Почему ограничиваем:
//   - веб-панель должна оставаться читаемой;
//   - большие таблицы нужны, но не бесконечные;
//   - полный список можно будет отдавать отдельным API позже.
const DefaultTopLimit = 10

// CountItem описывает одну позицию в топ-списке.
//
// Используется для:
//   - топ IP;
//   - топ стран;
//   - распределения CloseReason.
type CountItem struct {
	// Key — значение группировки.
	//
	// Например:
	//   - IP-адрес;
	//   - страна;
	//   - причина закрытия.
	Key string `json:"key"`

	// Count — количество событий для этого значения.
	Count int `json:"count"`
}

// HourStat описывает один часовой срез статистики.
//
// Это готовая структура для:
//   - веб-панели;
//   - JSON API;
//   - графиков;
//   - сводных таблиц.
type HourStat struct {
	// Hour — начало часового среза в UTC.
	//
	// Все часовые срезы храним в UTC.
	// Это исключает путаницу с локальными таймзонами.
	Hour time.Time `json:"hour"`

	// Events — сколько всего событий попало в этот час.
	//
	// Сюда входят и валидные, и невалидные события.
	// Это общий объём активности.
	Events int `json:"events"`

	// ValidEvents — сколько событий прошли нормализацию без ошибок.
	//
	// Эти события пригодны для глубокой аналитики.
	ValidEvents int `json:"valid_events"`

	// InvalidEvents — сколько событий имели проблемы качества данных.
	//
	// Это важный маркер честности отчёта.
	// Администратор должен видеть не только успешные события,
	// но и повреждённые строки.
	InvalidEvents int `json:"invalid_events"`

	// UniqueIPs — сколько уникальных IP наблюдалось в этом часе.
	//
	// Считаем только события с валидным IP.
	// Это защищает от искажений из-за битых адресов.
	UniqueIPs int `json:"unique_ips"`

	// UniqueCountries — сколько уникальных стран наблюдалось в этом часе.
	//
	// Считаем только непустые страны.
	// Пустая страна означает отсутствие данных, а не отдельную страну.
	UniqueCountries int `json:"unique_countries"`

	// AggressiveEvents — сколько событий помечено как агрессивные.
	AggressiveEvents int `json:"aggressive_events"`

	// PassiveEvents — сколько событий помечено как пассивные.
	PassiveEvents int `json:"passive_events"`

	// UnknownBehaviorEvents — сколько событий не имели распознанного поведения.
	//
	// Это не обязательно ошибка.
	// Возможно, формат поведения расширился, и нормализатор нужно обновить.
	UnknownBehaviorEvents int `json:"unknown_behavior_events"`

	// PhaseCounts — распределение событий по фазам угрозы.
	//
	// Каждый элемент содержит:
	//   - Key: код фазы (PHASE_1_BLIND_RECON_L4, PHASE_2_BANNER_RECON_L7, и т.д.);
	//   - Count: количество событий в этой фазе за час.
	//
	// Используется для мультислойного таймлайна эскалации (Stacked Area Chart).
	PhaseCounts []CountItem `json:"phase_counts"`

	// AvgIATMs — средний интервал между чтениями в миллисекундах за час.
	//
	// Вычисляется как: IATSumMs / IATCount (только для событий с валидным IAT).
	//
	// Интерпретация:
	//   - < 5 мс: автоматический скрипт (жёсткий таймер)
	//   - 5-100 мс: стандартное поведение
	//   - > 100 мс: имитация человека или медленный бот
	//
	// Если валидных IAT не было, значение равно 0.
	// Это нужно интерпретировать вместе с IATEventCount.
	AvgIATMs float64 `json:"avg_iat_ms"`

	// IATEventCount — сколько событий в этом часе имели валидный IAT.
	//
	// Используется для интерпретации AvgIATMs:
	// если IATEventCount == 0, то AvgIATMs == 0 означает отсутствие данных,
	// а не реальный нулевой интервал.
	IATEventCount int `json:"iat_event_count"`

	// ToolProfiles — распределение вердиктов о типе инструмента бота.
	//
	// Ключи: TIMER_BOT, HUMAN_EMULATION_OR_BAD_PROXY, UNKNOWN.
	// Используется для стекового графика «временных отпечатков»:
	// доля событий с фиксированным таймером обнажает автоматический движок.
	ToolProfiles []CountItem `json:"tool_profiles"`

	// TopStackFingerprints — топ уникальных TCP-стеков за час.
	//
	// Каждый элемент содержит:
	//   - Key: комбинация MSS|RcvSpace|SndWscale|RcvWscale
	//   - Count: количество событий с этим стеком
	//
	// Это пассивный TCP Fingerprinting (p0f-логика).
	TopStackFingerprints []CountItem `json:"top_stack_fingerprints"`

	// ReadDistribution — распределение чтений по категориям.
	//
	// Ключи: tiny (0-64), small (65-256), medium (257-1024), large (>1024).
	// Используется для гистограммы прикладных чтений.
	ReadDistribution []CountItem `json:"read_distribution"`

	// AvgIATDispersion — средний дисперсионный след ритма IAT за час.
	//
	// Высокая дисперсия = рваный ритм (человек/прокси).
	// Низкая дисперсия = жёсткий таймер (бот).
	AvgIATDispersion float64 `json:"avg_iat_dispersion"`

	// CloseReasons — распределение причин закрытия.
	//
	// Сюда попадают:
	//   - FIN;
	//   - RST;
	//   - TIMEOUT;
	//   - OUR_CLOSE;
	//   - UNKNOWN, если причина пустая;
	//   - другие значения, если лог принёс новый маркер.
	//
	// Это нужно для психологии удержания и фазового анализа.
	CloseReasons []CountItem `json:"close_reasons"`

	// RTTCount — сколько событий в этом часе имели валидный RTT.
	RTTCount uint64 `json:"rtt_count"`

	// RTTMinMs — минимальный RTT в миллисекундах за час.
	//
	// Если валидных RTT не было, значение равно 0.
	// Это нужно интерпретировать вместе с RTTCount.
	RTTMinMs uint32 `json:"rtt_min_ms"`

	// RTTMaxMs — максимальный RTT в миллисекундах за час.
	//
	// Если валидных RTT не было, значение равно 0.
	// Это нужно интерпретировать вместе с RTTCount.
	RTTMaxMs uint32 `json:"rtt_max_ms"`

	// RTTAvgMs — средний RTT в миллисекундах за час.
	//
	// Если валидных RTT не было, значение равно 0.
	// Средний RTT — слабый маркер сам по себе,
	// но полезен в динамике и сравнении профилей.
	RTTAvgMs float64 `json:"rtt_avg_ms"`

	// PlannedHoldSumSec — суммарный план удержания по событиям часа.
	//
	// Считаем только события, где одновременно валидны план и факт.
	// Это нужно для сравнения план/факт в агрегатах.
	PlannedHoldSumSec uint64 `json:"planned_hold_sum_sec"`

	// ActualHoldSumSec — суммарный факт удержания по событиям часа.
	//
	// Считаем только события, где одновременно валидны план и факт.
	// Это нужно для сравнения план/факт в агрегатах.
	ActualHoldSumSec uint64 `json:"actual_hold_sum_sec"`

	// HoldCount — сколько событий участвуют в суммах план/факт.
	//
	// Если значение равно 0, суммы использовать нельзя.
	// Это защита от деления на ноль и ложных средних.
	HoldCount uint64 `json:"hold_count"`

	// TopIPs — самые активные IP за час.
	//
	// Это не уникальные IP вообще, а именно топ по количеству событий.
	TopIPs []CountItem `json:"top_ips"`

	// TopCountries — самые активные страны за час.
	//
	// Это топ по количеству событий, а не просто список стран.
	TopCountries []CountItem `json:"top_countries"`
}

// hourAgg — внутренний аккумулятор одного часового среза.
//
// Он не отдаётся наружу.
// Его задача — копить данные во время прохода по событиям,
// а затем превратиться в готовую HourStat.
type hourAgg struct {
	// events — общее количество событий в часу.
	events int

	// validEvents — количество валидных событий.
	validEvents int

	// invalidEvents — количество невалидных событий.
	invalidEvents int

	// ipCounts — счётчик событий по каждому IP.
	//
	// Используется для:
	//   - количества уникальных IP;
	//   - построения топ IP.
	ipCounts map[string]int

	// countryCounts — счётчик событий по каждой стране.
	//
	// Используется для:
	//   - количества уникальных стран;
	//   - построения топ стран.
	countryCounts map[string]int

	// aggressiveEvents — количество агрессивных событий.
	aggressiveEvents int

	// passiveEvents — количество пассивных событий.
	passiveEvents int

	// unknownBehaviorEvents — количество событий без распознанного поведения.
	unknownBehaviorEvents int

	// closeReasonCounts — счётчик причин закрытия.
	//
	// Нужно для распределения:
	//   - кто и как закрывает сессии;
	//   - есть ли переход к панике;
	//   - есть ли адаптация к капкану.
	closeReasonCounts map[string]int

	// phaseCounts — счётчик фаз угрозы.
	//
	// Группирует события по кодам фаз:
	//   - PHASE_1_BLIND_RECON_L4 (слепой скан);
	//   - PHASE_2_BANNER_RECON_L7 (сбор баннеров);
	//   - PHASE_3_PAYLOAD_PROBE (зондирование payload);
	//   - PHASE_UNKNOWN (не определена).
	//
	// Используется для мультислойного таймлайна эскалации.
	phaseCounts map[string]int

	// iatCount — суммарное количество интервалов между чтениями за час.
	//
	// Считаем только события с валидным IATCount и IATSumMs.
	// Используется для вычисления среднего интервала (AvgIATMs).
	iatCount uint64

	// iatSum — сумма всех интервалов между чтениями за час (мс).
	//
	// Считаем только события с валидным IATSumMs.
	// Используем uint64, чтобы снизить риск переполнения.
	iatSum uint64

	// iatEventCount — сколько событий имели валидный IAT.
	//
	// Используется для интерпретации AvgIATMs:
	// если значение равно 0, то AvgIATMs == 0 означает отсутствие данных.
	iatEventCount int

	// toolProfileCounts — счётчик вердиктов о типе инструмента бота.
	//
	// Группирует события по кодам:
	//   - TIMER_BOT (жёсткий автоматический таймер);
	//   - HUMAN_EMULATION_OR_BAD_PROXY (рваный ритм);
	//   - UNKNOWN (ритм не дал сигнала).
	//
	// Это «временные отпечатки пальцев» из ТЗ: фиксированные тайминги
	// обнажают автоматический движок без привязки к L7.
	toolProfileCounts map[string]int

	// stackFingerprintCounts — счётчик уникальных TCP-стеков.
	//
	// Ключ — комбинация MSS|RcvSpace|SndWscale|RcvWscale.
	// Значение — количество событий с этим стеком.
	//
	// Это пассивный TCP Fingerprinting (p0f-логика):
	// уникальная комбинация параметров стека идентифицирует ОС.
	stackFingerprintCounts map[string]int

	// readTinyCount — суммарное количество чтений 0–64 байта за час.
	readTinyCount uint64

	// readSmallCount — суммарное количество чтений 65–256 байта за час.
	readSmallCount uint64

	// readMediumCount — суммарное количество чтений 257–1024 байта за час.
	readMediumCount uint64

	// readLargeCount — суммарное количество чтений >1024 байта за час.
	readLargeCount uint64

	// iatDispersionSum — сумма дисперсионных следов IAT за час.
	//
	// Используется для вычисления среднего значения дисперсии.
	iatDispersionSum float64

	// iatDispersionCount — сколько событий имели валидную дисперсию IAT.
	iatDispersionCount int

	// rttCount — сколько событий имели валидный RTT.
	rttCount uint64

	// rttSum — сумма RTT для расчёта среднего.
	//
	// Используем uint64, чтобы снизить риск переполнения
	// при большом количестве событий.
	rttSum uint64

	// rttMin — минимальный RTT за час.
	//
	// Значение имеет смысл только если rttSeen == true.
	rttMin uint32

	// rttMax — максимальный RTT за час.
	//
	// Значение имеет смысл только если rttSeen == true.
	rttMax uint32

	// rttSeen — были ли в этом часе вообще валидные RTT.
	//
	// Без этого флага нельзя отличить ситуацию:
	//   - минимальный RTT реально равен 0;
	//   - валидных RTT не было вовсе.
	//
	// В текущем формате 0 маловероятен как реальный сетевой RTT,
	// но архитектурно лучше не полагаться на догадки.
	rttSeen bool

	// plannedSum — сумма плана удержания.
	//
	// Считаем только когда план и факт оба валидны.
	// Иначе пара план/факт теряет смысл.
	plannedSum uint64

	// actualSum — сумма факта удержания.
	//
	// Считаем только когда план и факт оба валидны.
	// Иначе пара план/факт теряет смысл.
	actualSum uint64

	// holdCount — количество событий с валидной парой план/факт.
	//
	// Используется для проверки, что агрегатные суммы имеют основу.
	holdCount uint64
}

// newHourAgg создаёт пустой аккумулятор часового среза.
//
// Все карты инициализируются сразу.
// Это защищает от записи в nil-карту при добавлении событий.
func newHourAgg() *hourAgg {
	return &hourAgg{
		ipCounts:               make(map[string]int),
		countryCounts:          make(map[string]int),
		closeReasonCounts:      make(map[string]int),
		phaseCounts:            make(map[string]int),
		toolProfileCounts:      make(map[string]int),
		stackFingerprintCounts: make(map[string]int),
	}
}

// addEvent добавляет одно событие в часовой аккумулятор.
//
// Аргументы:
//   - rec: нормализованное событие.
//
// Важно:
//   - сюда должны попадать только события с валидным временем;
//   - решение о попадании в часовой срез принимает BuildHourStats;
//   - сам аккумулятор не проверяет время, чтобы не дублировать логику.
func (a *hourAgg) addEvent(rec EventRecord) {
	// Увеличиваем общее количество событий.
	//
	// Это базовый счётчик активности за час.
	a.events++

	// Разделяем события на валидные и невалидные.
	//
	// Валидность уже определена нормализатором.
	// Здесь мы только учитываем её в агрегате.
	if rec.Valid {
		a.validEvents++
	} else {
		a.invalidEvents++
	}

	// Учитываем IP, если он валиден и не пуст.
	//
	// Почему только валидные:
	//   - битый IP не должен влиять на уникальные адреса;
	//   - статистика должна быть честной;
	//   - повреждённые адреса уже видны в счётчиках качества.
	if rec.IPValid && rec.IP != "" {
		a.ipCounts[rec.IP]++
	}

	// Учитываем страну, если она не пустая.
	//
	// Пустая страна означает отсутствие данных.
	// Мы не превращаем её в отдельную сущность.
	if rec.Country != "" {
		a.countryCounts[rec.Country]++
	}

	// Учитываем поведение.
	//
	// Нормализатор уже привёл поведение к коротким формам:
	//   - AGGRESSIVE;
	//   - PASSIVE.
	//
	// Если поведение не распознано, учитываем его отдельно.
	switch rec.Behavior {
	case "AGGRESSIVE":
		// Агрессивное поведение.
		//
		// Обычно означает более активное взаимодействие с капканом.
		a.aggressiveEvents++

	case "PASSIVE":
		// Пассивное поведение.
		//
		// Обычно означает более осторожное или медленное взаимодействие.
		a.passiveEvents++

	default:
		// Поведение не распознано.
		//
		// Это важно учитывать отдельно, чтобы не смешивать
		// реальную пассивность с отсутствием данных.
		a.unknownBehaviorEvents++
	}

	// Учитываем причину закрытия.
	//
	// Если причина пустая, заменяем её на UNKNOWN.
	// Это сохраняет честность:
	//   - мы не выдумываем причину;
	//   - но показываем, что закрытие было без маркера.
	reason := rec.CloseReason
	if reason == "" {
		reason = "UNKNOWN"
	}

	// Увеличиваем счётчик причины закрытия.
	//
	// Сюда попадают и известные причины, и неизвестные.
	// Это полезно для расширения словаря в будущем.
	a.closeReasonCounts[reason]++

	// Учитываем RTT, если он валиден.
	//
	// Мы не пытаемся угадывать отсутствующий RTT.
	// Если RTT нет, он просто не участвует в агрегатах.
	if rec.RTTValid {
		// Увеличиваем количество валидных RTT.
		a.rttCount++

		// Добавляем значение к сумме для среднего.
		//
		// Приводим к uint64, чтобы снизить риск переполнения.
		a.rttSum += uint64(rec.RTTMs)

		// Если это первый валидный RTT за час,
		// принимаем его как минимум и максимум.
		if !a.rttSeen {
			a.rttSeen = true
			a.rttMin = rec.RTTMs
			a.rttMax = rec.RTTMs
		} else {
			// Если история уже есть, обновляем минимум и максимум.
			//
			// Это даёт быстрый часовой диапазон без хранения всех значений.
			if rec.RTTMs < a.rttMin {
				a.rttMin = rec.RTTMs
			}
			if rec.RTTMs > a.rttMax {
				a.rttMax = rec.RTTMs
			}
		}
	}

	// Учитываем пару план/факт только если оба значения валидны.
	//
	// Почему вместе:
	//   - план без факта не даёт полной картины удержания;
	//   - факт без плана не даёт понять, было ли удержание короче плана;
	//   - для дельты нужны оба значения.
	if rec.PlannedHoldValid && rec.ActualHoldValid {
		a.plannedSum += rec.PlannedHoldSec
		a.actualSum += rec.ActualHoldSec
		a.holdCount++
	}
	// Учитываем фазу угрозы через классификатор.
	//
	// ClassifyPhase анализирует чтения (ReadTiny/Small/Medium/Large),
	// CloseReason и IAT-ритм, возвращая код фазы.
	//
	// Мы используем стабильный машинный Code (не Label), чтобы:
	//   - графики не ломались при изменении подписей;
	//   - API оставался совместимым;
	//   - веб-панель могла фильтровать по кодам.
	phase := ClassifyPhase(rec)
	a.phaseCounts[phase.Code]++
	// Учитываем IAT-метрики для индекса скорости атаки.
	//
	// Считаем только события с валидным IATCount и IATSumMs.
	// Это даёт средний интервал между чтениями — маркер автоматизации.
	if rec.IATCountValid && rec.IATSumMsValid && rec.IATCount > 0 {
		a.iatCount += rec.IATCount
		a.iatSum += rec.IATSumMs
		a.iatEventCount++
	}

	// Учитываем вердикт о типе инструмента бота (временной отпечаток).
	//
	// Переиспользуем готовую математику phases.go:
	// дисперсионный след ритма + сравнение Min/Max интервалов.
	// TIMER_BOT означает фиксированный тайминг — прямой признак движка.
	tool := analyzeToolProfile(rec, calculateIATDispersion(rec))
	a.toolProfileCounts[tool]++

	// Учитываем TCP-отпечаток (пассивный fingerprinting).
	//
	// Формируем ключ из комбинации MSS|RcvSpace|SndWscale|RcvWscale.
	// Это уникальная сигнатура сетевого стека, идентифицирующая ОС.
	if rec.MSSValid && rec.RcvSpaceValid && rec.SndWscaleValid && rec.RcvWscaleValid {
		fpKey := fmt.Sprintf("%d|%d|%d|%d", rec.MSS, rec.RcvSpace, rec.SndWscale, rec.RcvWscale)
		a.stackFingerprintCounts[fpKey]++
	}

	// Учитываем распределение чтений по категориям.
	//
	// Это нужно для гистограммы прикладных чтений.
	if rec.ReadTinyValid {
		a.readTinyCount += rec.ReadTiny
	}
	if rec.ReadSmallValid {
		a.readSmallCount += rec.ReadSmall
	}
	if rec.ReadMediumValid {
		a.readMediumCount += rec.ReadMedium
	}
	if rec.ReadLargeValid {
		a.readLargeCount += rec.ReadLarge
	}

	// Учитываем дисперсию IAT для анализа ритма.
	//
	// Переиспользуем готовую функцию calculateIATDispersion из phases.go.
	if rec.IATCountValid && rec.IATSumMsValid && rec.IATSumSqMsValid && rec.IATCount > 0 {
		dispersion := calculateIATDispersion(rec)
		a.iatDispersionSum += dispersion
		a.iatDispersionCount++
	}
}

// toHourStat превращает внутренний аккумулятор в готовую часовую статистику.
//
// Аргументы:
//   - hour: временная метка часового среза;
//   - topLimit: сколько элементов отдавать в топ-списках.
//
// Возврат:
//   - готовая структура HourStat для веб-панели и API.
//
// Важно:
//   - внутренние карты наружу не отдаём;
//   - отдаём только отсортированные срезы;
//   - все числовые агрегаты уже посчитаны.
func (a *hourAgg) toHourStat(hour time.Time, topLimit int) HourStat {
	// Считаем средний RTT.
	//
	// Если валидных RTT не было, среднее остаётся нулевым.
	// Это осознанное поведение.
	// Нулевое среднее при нулевом количестве нельзя интерпретировать как реальный ноль.
	var rttAvg float64
	if a.rttCount > 0 {
		rttAvg = float64(a.rttSum) / float64(a.rttCount)
	}

	// Считаем средний интервал между чтениями (индекс скорости атаки).
	//
	// Если валидных IAT не было, среднее остаётся нулевым.
	// IATEventCount используется для интерпретации:
	// если он равен 0, то AvgIATMs == 0 означает отсутствие данных.
	var avgIAT float64
	if a.iatCount > 0 {
		avgIAT = float64(a.iatSum) / float64(a.iatCount)
	}

	// Считаем средний дисперсионный след ритма IAT.
	//
	// Высокая дисперсия = рваный ритм (человек/прокси).
	// Низкая дисперсия = жёсткий таймер (бот).
	var avgDispersion float64
	if a.iatDispersionCount > 0 {
		avgDispersion = a.iatDispersionSum / float64(a.iatDispersionCount)
	}

	// Формируем распределение чтений по категориям.
	readDist := []CountItem{
		{Key: "tiny (0-64)", Count: int(a.readTinyCount)},
		{Key: "small (65-256)", Count: int(a.readSmallCount)},
		{Key: "medium (257-1024)", Count: int(a.readMediumCount)},
		{Key: "large (>1024)", Count: int(a.readLargeCount)},
	}

	// Формируем итоговую часовую статистику.
	//
	// Все внутренние аккумуляторные поля переводятся
	// в публичные поля структуры.
	return HourStat{
		Hour:                  hour,
		Events:                a.events,
		ValidEvents:           a.validEvents,
		InvalidEvents:         a.invalidEvents,
		UniqueIPs:             len(a.ipCounts),
		UniqueCountries:       len(a.countryCounts),
		AggressiveEvents:      a.aggressiveEvents,
		PassiveEvents:         a.passiveEvents,
		UnknownBehaviorEvents: a.unknownBehaviorEvents,

		// CloseReasons и PhaseCounts отдаём полностью, но отсортированно.
		//
		// Причины закрытия и фазы — небольшие справочные наборы.
		// Их не нужно жёстко резать топ-лимитом.
		CloseReasons: topFromMap(a.closeReasonCounts, 0),
		PhaseCounts:  topFromMap(a.phaseCounts, 0),
		ToolProfiles: topFromMap(a.toolProfileCounts, 0),

		RTTCount: a.rttCount,
		RTTMinMs: a.rttMin,
		RTTMaxMs: a.rttMax,
		RTTAvgMs: rttAvg,

		// Индекс скорости атаки (средний интервал между чтениями).
		AvgIATMs:         avgIAT,
		IATEventCount:    a.iatEventCount,
		AvgIATDispersion: avgDispersion,

		PlannedHoldSumSec: a.plannedSum,
		ActualHoldSumSec:  a.actualSum,
		HoldCount:         a.holdCount,

		// Топ IP и топ стран ограничиваем.
		//
		// Это защищает веб-панель от избыточных списков.
		TopIPs:       topFromMap(a.ipCounts, topLimit),
		TopCountries: topFromMap(a.countryCounts, topLimit),

		// TCP-отпечатки (p0f-логика) — топ-10 уникальных стеков.
		TopStackFingerprints: topFromMap(a.stackFingerprintCounts, 10),

		// Распределение чтений по категориям.
		ReadDistribution: readDist,
	}
}

// HourBucket приводит время к началу часа в UTC.
//
// Например:
//
//	2026-10-17 17:39:15 -> 2026-10-17 17:00:00
//
// Зачем:
//   - все часовые срезы должны быть в единой системе координат;
//   - агрегаты не должны зависеть от минут и секунд внутри часа;
//   - веб-панель сможет группировать данные без повторной логики.
func HourBucket(t time.Time) time.Time {
	// Получаем витринную локацию из конфига.
	// "" или "local" → time.Local (дефолт, fail-safe)
	// "UTC" → time.UTC
	// IANA-имя → time.LoadLocation (валидировано в config.go)
	loc := time.Local
	if tz := config.C.WebPanelTimezone; tz != "" && tz != "local" {
		if loaded, err := time.LoadLocation(tz); err == nil {
			loc = loaded
		}
	}

	// Приводим время к витринной зоне и обрезаем до часа.
	// Это гарантирует, что бакеты совпадают с локальными часами админа.
	return t.In(loc).Truncate(time.Hour)
}

// BuildHourStats строит все доступные часовые срезы из хранилища.
//
// Аргументы:
//   - s: хранилище событий;
//   - topLimit: размер топ-списков.
//
// Возврат:
//   - список часовых срезов в хронологическом порядке: от старых к новым.
//
// Важно:
//   - события без валидного времени не попадают в часовые срезы;
//   - они уже учтены в счётчиках качества самого хранилища;
//   - результат не кэшируется, это честный снимок текущего буфера.
func BuildHourStats(s *Store, topLimit int) []HourStat {
	// Защита от вызова на пустом хранилище.
	//
	// Если хранилище не создано, возвращаем пустой список.
	// Это безопаснее, чем паника.
	if s == nil {
		return []HourStat{}
	}

	// Если топ-лимит не задан или задан некорректно,
	// используем значение по умолчанию.
	//
	// Это защищает от нулевых или отрицательных значений.
	if topLimit <= 0 {
		topLimit = DefaultTopLimit
	}

	// Получаем хронологический снимок событий.
	//
	// Мы сознательно используем SnapshotChronological:
	//   - события уже идут от старых к новым;
	//   - это упрощает отладку;
	//   - порядок полезен при будущей трассировке инцидентов.
	events := s.SnapshotChronological()

	// Создаём карту часовых аккумуляторов.
	//
	// Ключ — начало часа в UTC.
	// Значение — аккумулятор для этого часа.
	buckets := make(map[time.Time]*hourAgg)

	// Проходим по всем событиям снимка.
	//
	// Это пакетная агрегация.
	// Для большого буфера она может быть тяжёлой,
	// но для текущей архитектуры и кольцевого буфера это приемлемо.
	//
	// Позже при необходимости можно перейти на инкрементальные счётчики.
	for _, event := range events {
		// Если время события невалидно, пропускаем событие.
		//
		// Часовой срез без времени невозможен.
		// Такие события уже видны в счётчиках качества хранилища.
		if !event.StartTimeValid {
			continue
		}

		// Приводим время события к началу часа.
		//
		// Это ключ агрегации.
		// Все события одного часа попадут в один аккумулятор.
		hour := HourBucket(event.StartTime)

		// Достаём аккумулятор для этого часа.
		//
		// Если его ещё нет, создаём новый.
		agg, ok := buckets[hour]
		if !ok {
			agg = newHourAgg()
			buckets[hour] = agg
		}

		// Добавляем событие в часовой аккумулятор.
		//
		// Внутри аккумулируем:
		//   - количество;
		//   - качество;
		//   - уникальные сущности;
		//   - поведение;
		//   - причины закрытия;
		//   - RTT;
		//   - план/факт.
		agg.addEvent(event)
	}

	// Собираем все часовые метки в отдельный срез.
	//
	// Это нужно для сортировки.
	// Карты в Go не гарантируют порядок обхода.
	hours := make([]time.Time, 0, len(buckets))
	for hour := range buckets {
		hours = append(hours, hour)
	}

	// Сортируем часовые метки по возрастанию.
	//
	// Итоговый список будет идти от старого к новому.
	// Это удобно для графиков и временных линий.
	sort.Slice(hours, func(i, j int) bool {
		return hours[i].Before(hours[j])
	})

	// Формируем итоговый список часовых срезов.
	//
	// Заранее выделяем вместимость, чтобы уменьшить реаллокации.
	result := make([]HourStat, 0, len(hours))

	// Проходим по отсортированным часам.
	//
	// Для каждого часа превращаем аккумулятор в готовую структуру.
	for _, hour := range hours {
		agg := buckets[hour]
		result = append(result, agg.toHourStat(hour, topLimit))
	}

	// Возвращаем готовый список часовых срезов.
	//
	// Он может быть пустым, если:
	//   - буфер пуст;
	//   - нет событий с валидным временем;
	//   - хранилище ещё не загружено.
	//
	// Это нормальное состояние, а не ошибка.
	return result
}

// BuildLatestHourStats возвращает последние часовые срезы.
//
// Аргументы:
//   - s: хранилище событий;
//   - maxHours: сколько последних часов вернуть;
//   - topLimit: размер топ-списков.
//
// Возврат:
//   - список часовых срезов в хронологическом порядке.
//
// Поведение:
//   - если maxHours <= 0, возвращаем все доступные часы;
//   - если доступных часов меньше, возвращаем все доступные;
//   - если часов больше, возвращаем только хвост.
//
// Зачем:
//   - веб-панель обычно показывает последние часы, а не всю историю;
//   - администратору нужен быстрый срез текущей обстановки;
//   - полный список можно запрашивать отдельным режимом позже.
func BuildLatestHourStats(s *Store, maxHours int, topLimit int) []HourStat {
	// Строим полный список часовых срезов.
	//
	// Это переиспользует уже готовую логику агрегации.
	// Мы сознательно не дублируем её здесь.
	stats := BuildHourStats(s, topLimit)

	// Если ограничение не задано или список меньше ограничения,
	// возвращаем весь список.
	//
	// Это защищает от лишних аллокаций и пустых манипуляций.
	if maxHours <= 0 || len(stats) <= maxHours {
		return stats
	}

	// Возвращаем только последние часы.
	//
	// Список отсортирован от старых к новым,
	// поэтому хвост — это самые свежие часы.
	//
	// Пример:
	//   - всего 50 часов;
	//   - maxHours = 24;
	//   - вернутся часы с 27-го по 50-й.
	return stats[len(stats)-maxHours:]
}

// topFromMap превращает карту счётчиков в отсортированный топ-список.
//
// Аргументы:
//   - m: карта вида ключ -> количество;
//   - limit: максимальное количество элементов.
//
// Поведение:
//   - если limit <= 0, возвращаем все элементы;
//   - сортировка: сначала по количеству убыванию, затем по ключу возрастанию;
//   - при равных количествах побеждает лексически меньший ключ.
//
// Зачем:
//   - карты в Go не имеют порядка;
//   - веб-панель должна получать стабильный и предсказуемый список;
//   - стабильная сортировка уменьшает визуальный шум при обновлении данных.
func topFromMap(m map[string]int, limit int) []CountItem {
	// Создаём срез заранее известного размера.
	//
	// Если карта пустая, вернётся пустой срез, а не nil.
	// Это удобно для JSON и веб-рендера.
	items := make([]CountItem, 0, len(m))

	// Переносим карту в срез.
	//
	// Это необходимо, потому что карты нельзя отсортировать напрямую.
	for key, count := range m {
		items = append(items, CountItem{
			Key:   key,
			Count: count,
		})
	}

	// Сортируем элементы.
	//
	// Правила сортировки:
	//   1. Большее количество выше.
	//   2. При равном количестве меньший ключ выше.
	//
	// Это даёт стабильный и читаемый топ.
	sort.Slice(items, func(i, j int) bool {
		// Если количество различается, сортируем по убыванию количества.
		//
		// Например:
		//   10 выше 7;
		//   7 выше 2.
		if items[i].Count != items[j].Count {
			return items[i].Count > items[j].Count
		}

		// Если количество одинаковое, сортируем по ключу.
		//
		// Это делает результат детерминированным.
		// Без этого порядок мог бы зависеть от внутреннего устройства карты.
		return items[i].Key < items[j].Key
	})

	// Если задан лимит и элементов больше, обрезаем список.
	//
	// Это нужно для топ-списков.
	// Полный список оставляем для служебных распределений,
	// где лимит равен 0.
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}

	// Возвращаем готовый список.
	//
	// Он может быть пустым, если карта пустая.
	// Это нормальное состояние.
	return items
}

// BurstPoint описывает плотность событий в одну секунду.
//
// Используется временным коррелятором кучности на странице SIEM Core:
// пики посекундной плотности обнажают синхронные залпы ботнета.
type BurstPoint struct {
	// Label — человекочитаемая метка секунды в витринной зоне.
	Label string `json:"label"`

	// Count — количество событий в эту секунду.
	Count int `json:"count"`
}

// BuildBurstSeries возвращает посекундные счётчики событий за окно.
//
// Поведение:
//   - отсутствующие секунды заполняются нулями, чтобы график был непрерывным;
//   - метки формируются в витринной зоне (как HourBucket);
//   - окно по умолчанию 5 минут, если передано некорректное.
//
// Сложность O(E): один проход по снапшоту, без вложенных циклов.
func BuildBurstSeries(s *Store, window time.Duration) []BurstPoint {
	if s == nil {
		return []BurstPoint{}
	}
	if window <= 0 {
		window = 5 * time.Minute
	}

	// Витринная зона — та же логика, что в HourBucket.
	loc := time.Local
	if tz := config.C.WebPanelTimezone; tz != "" && tz != "local" {
		if loaded, err := time.LoadLocation(tz); err == nil {
			loc = loaded
		}
	}

	now := time.Now().UTC().Truncate(time.Second)
	start := now.Add(-window)

	counts := make(map[int64]int)
	for _, e := range s.SnapshotChronological() {
		if !e.StartTimeValid {
			continue
		}
		sec := e.StartTime.Unix()
		if sec < start.Unix() || sec > now.Unix() {
			continue
		}
		counts[sec]++
	}

	// Заполняем каждую секунду окна, включая нулевые.
	points := make([]BurstPoint, 0, int(window/time.Second)+1)
	for sec := start.Unix(); sec <= now.Unix(); sec++ {
		points = append(points, BurstPoint{
			Label: time.Unix(sec, 0).In(loc).Format("15:04:05"),
			Count: counts[sec],
		})
	}
	return points
}
