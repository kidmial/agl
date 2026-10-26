// internal/analytics/rollup.go
// =====================================================================
// КОМПОНЕНТ: КОЛЬЦА БАКЕТОВ ДЛЯ АГРЕГАТНОЙ ОТЧЁТНОСТИ
// РОЛЬ: Инкрементальные агрегаты с фиксированной памятью.
// ПРИНЦИП:
//   - 24 часовых + 31 суточный бакет — перекрывают окна до месяца;
//   - любой запрос (сутки/неделя/месяц) = сумма бакетов окна;
//   - суточные бакеты переживают вытеснение событий из Store;
//   - инкремент O(1) на событие, отчёт O(бакетов);
//   - rebuild атомарен: Reset() → replay через тот же AddEvent.
//
// =====================================================================
package analytics

import (
	"sort"
	"sync"
	"time"
)

// Глубина колец.
const (
	HourBucketsDepth = 24 // 24 часа покрывают сутки
	DayBucketsDepth  = 31 // 31 день покрывает месяц
)

// Лимиты вместимости, чтобы одна активная страна/IP не раздула память.
const (
	IPPerBucketLimit      = 500  // макс IP в одном часовом бакете
	IPPerDayLimit         = 2000 // макс IP в одном суточном бакете
	CountryPerBucketLimit = 250  // практически все ISO-коды умещаются
)

// CountryBucket — агрегат страны внутри одного бакета.
type CountryBucket struct {
	Code       string
	Events     uint64
	HoldSumSec uint64
	DropSumSec uint64 // только FIN/RST
	DropCount  uint64
	UniqueIPs  map[string]struct{}
}

// IPBucket — агрегат IP-адреса внутри одного бакета.
type IPBucket struct {
	IP           string
	Country      string
	Events       uint64
	HoldSumSec   uint64
	DropSumSec   uint64
	DropCount    uint64
	RTTSumMs     uint64
	RTTCount     uint64
	CloseReasons map[string]uint64
	FirstSeen    time.Time
	LastSeen     time.Time
}

// HourBucket — часовая агрегация.
type HourAggBucket struct {
	Hour      time.Time
	Events    uint64
	Valid     uint64
	HoldSum   uint64
	DropSum   uint64
	DropCount uint64
	Behavior  map[string]uint64
	Countries map[string]*CountryBucket
	IPs       map[string]*IPBucket
}

// DayBucket — суточная агрегация.
type DayAggBucket struct {
	Day       time.Time
	Events    uint64
	Valid     uint64
	HoldSum   uint64
	DropSum   uint64
	DropCount uint64
	Behavior  map[string]uint64
	Countries map[string]*CountryBucket
	IPs       map[string]*IPBucket
}

// Rollup — глобальные кольца бакетов.
type Rollup struct {
	mu    sync.RWMutex
	loc   *time.Location
	hours [HourBucketsDepth]*HourAggBucket
	days  [DayBucketsDepth]*DayAggBucket
}

// SetTimezone устанавливает витринную таймзону для бакетов.
// Вызывается один раз при старте после чтения конфига.
func (r *Rollup) SetTimezone(loc *time.Location) {
	if r == nil || loc == nil {
		return
	}
	r.mu.Lock()
	r.loc = loc
	r.mu.Unlock()
}

// Reset сбрасывает все бакеты.
// Используется Store.Clear() для атомарного rebuild через replay.
func (r *Rollup) Reset() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.hours {
		r.hours[i] = nil
	}
	for i := range r.days {
		r.days[i] = nil
	}
}

// Ingest инкрементирует все бакеты, в окно которых попадает событие.
// O(1) по событию. Вызывается из Store.AddEvent внутри его блокировки.
func (r *Rollup) Ingest(rec EventRecord) {
	if r == nil || !rec.StartTimeValid {
		return
	}

	loc := time.UTC
	r.mu.Lock()
	if r.loc != nil {
		loc = r.loc
	}
	r.mu.Unlock()

	t := rec.StartTime.In(loc)
	hourStart := time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, loc)
	dayStart := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)

	r.mu.Lock()
	defer r.mu.Unlock()

	// Инкремент часового бакета.
	hIdx := hourStart.Hour()
	if r.hours[hIdx] == nil || !r.hours[hIdx].Hour.Equal(hourStart) {
		r.hours[hIdx] = newHourAggBucket(hourStart)
	}
	applyEventToHour(r.hours[hIdx], rec)

	// Инкремент суточного бакета.
	dIdx := dayStart.Day() - 1 // 0..30
	if dIdx < 0 || dIdx >= DayBucketsDepth {
		return
	}
	if r.days[dIdx] == nil || !r.days[dIdx].Day.Equal(dayStart) {
		r.days[dIdx] = newDayAggBucket(dayStart)
	}
	applyEventToDay(r.days[dIdx], rec)
}

func newHourAggBucket(hour time.Time) *HourAggBucket {
	return &HourAggBucket{
		Hour:      hour,
		Behavior:  make(map[string]uint64),
		Countries: make(map[string]*CountryBucket),
		IPs:       make(map[string]*IPBucket),
	}
}

func newDayAggBucket(day time.Time) *DayAggBucket {
	return &DayAggBucket{
		Day:       day,
		Behavior:  make(map[string]uint64),
		Countries: make(map[string]*CountryBucket),
		IPs:       make(map[string]*IPBucket),
	}
}

func applyEventToHour(h *HourAggBucket, rec EventRecord) {
	h.Events++
	if rec.Valid {
		h.Valid++
	}
	if rec.ActualHoldValid {
		h.HoldSum += rec.ActualHoldSec
	}
	if isClientDrop(rec.CloseReason) && rec.ActualHoldValid {
		h.DropSum += rec.ActualHoldSec
		h.DropCount++
	}
	behavior := rec.Behavior
	if behavior == "" {
		behavior = "UNKNOWN"
	}
	h.Behavior[behavior]++

	if rec.Country != "" {
		c, ok := h.Countries[rec.Country]
		if !ok {
			if len(h.Countries) >= CountryPerBucketLimit {
				// Не расширяем страну, просто не считаем
			} else {
				c = &CountryBucket{Code: rec.Country, UniqueIPs: make(map[string]struct{})}
				h.Countries[rec.Country] = c
			}
		}
		if c != nil {
			c.Events++
			if rec.ActualHoldValid {
				c.HoldSumSec += rec.ActualHoldSec
			}
			if isClientDrop(rec.CloseReason) && rec.ActualHoldValid {
				c.DropSumSec += rec.ActualHoldSec
				c.DropCount++
			}
			if rec.IPValid && rec.IP != "" {
				c.UniqueIPs[rec.IP] = struct{}{}
			}
		}
	}

	if rec.IPValid && rec.IP != "" {
		ip := h.IPs[rec.IP]
		if ip == nil {
			if len(h.IPs) >= IPPerBucketLimit {
				// Вытесняем IP с минимальным числом событий
				minKey, minCount := "", uint64(^uint64(0))
				for k, v := range h.IPs {
					if v.Events < minCount {
						minCount = v.Events
						minKey = k
					}
				}
				if minKey != "" {
					delete(h.IPs, minKey)
				}
			}
			ip = &IPBucket{IP: rec.IP, Country: rec.Country, CloseReasons: make(map[string]uint64), FirstSeen: rec.StartTime, LastSeen: rec.StartTime}
			h.IPs[rec.IP] = ip
		}
		if rec.Country != "" {
			ip.Country = rec.Country
		}
		ip.Events++
		if rec.ActualHoldValid {
			ip.HoldSumSec += rec.ActualHoldSec
		}
		if isClientDrop(rec.CloseReason) && rec.ActualHoldValid {
			ip.DropSumSec += rec.ActualHoldSec
			ip.DropCount++
		}
		if rec.RTTValid {
			ip.RTTSumMs += uint64(rec.RTTMs)
			ip.RTTCount++
		}
		cr := rec.CloseReason
		if cr == "" {
			cr = "UNKNOWN"
		}
		ip.CloseReasons[cr]++
		if rec.StartTime.Before(ip.FirstSeen) {
			ip.FirstSeen = rec.StartTime
		}
		if rec.StartTime.After(ip.LastSeen) {
			ip.LastSeen = rec.StartTime
		}
	}
}

func applyEventToDay(d *DayAggBucket, rec EventRecord) {
	d.Events++
	if rec.Valid {
		d.Valid++
	}
	if rec.ActualHoldValid {
		d.HoldSum += rec.ActualHoldSec
	}
	if isClientDrop(rec.CloseReason) && rec.ActualHoldValid {
		d.DropSum += rec.ActualHoldSec
		d.DropCount++
	}
	behavior := rec.Behavior
	if behavior == "" {
		behavior = "UNKNOWN"
	}
	d.Behavior[behavior]++

	if rec.Country != "" {
		c, ok := d.Countries[rec.Country]
		if !ok {
			if len(d.Countries) >= CountryPerBucketLimit {
				// лимит
			} else {
				c = &CountryBucket{Code: rec.Country, UniqueIPs: make(map[string]struct{})}
				d.Countries[rec.Country] = c
			}
		}
		if c != nil {
			c.Events++
			if rec.ActualHoldValid {
				c.HoldSumSec += rec.ActualHoldSec
			}
			if isClientDrop(rec.CloseReason) && rec.ActualHoldValid {
				c.DropSumSec += rec.ActualHoldSec
				c.DropCount++
			}
			if rec.IPValid && rec.IP != "" {
				c.UniqueIPs[rec.IP] = struct{}{}
			}
		}
	}

	if rec.IPValid && rec.IP != "" {
		ip := d.IPs[rec.IP]
		if ip == nil {
			if len(d.IPs) >= IPPerDayLimit {
				minKey, minCount := "", uint64(^uint64(0))
				for k, v := range d.IPs {
					if v.Events < minCount {
						minCount = v.Events
						minKey = k
					}
				}
				if minKey != "" {
					delete(d.IPs, minKey)
				}
			}
			ip = &IPBucket{IP: rec.IP, Country: rec.Country, CloseReasons: make(map[string]uint64), FirstSeen: rec.StartTime, LastSeen: rec.StartTime}
			d.IPs[rec.IP] = ip
		}
		if rec.Country != "" {
			ip.Country = rec.Country
		}
		ip.Events++
		if rec.ActualHoldValid {
			ip.HoldSumSec += rec.ActualHoldSec
		}
		if isClientDrop(rec.CloseReason) && rec.ActualHoldValid {
			ip.DropSumSec += rec.ActualHoldSec
			ip.DropCount++
		}
		if rec.RTTValid {
			ip.RTTSumMs += uint64(rec.RTTMs)
			ip.RTTCount++
		}
		cr := rec.CloseReason
		if cr == "" {
			cr = "UNKNOWN"
		}
		ip.CloseReasons[cr]++
		if rec.StartTime.Before(ip.FirstSeen) {
			ip.FirstSeen = rec.StartTime
		}
		if rec.StartTime.After(ip.LastSeen) {
			ip.LastSeen = rec.StartTime
		}
	}
}

// isClientDrop возвращает true, если закрытие инициировал клиент (FIN/RST).
// Используется для метрики Avg Drop Second: только сессии, в которых бот
// проявил волю к сбросу. TIMEOUT и OUR_CLOSE исключены.
func isClientDrop(reason string) bool {
	return reason == "FIN" || reason == "RST"
}

// =====================================================================
// ОТЧЁТНЫЕ СТРУКТУРЫ
// =====================================================================

// ReportRange — период отчёта.
type ReportRange string

const (
	Range24h ReportRange = "24h"
	Range7d  ReportRange = "7d"
	Range30d ReportRange = "30d"
)

// CountryReport — одна строка отчёта по стране.
type CountryReport struct {
	Code       string
	Events     uint64
	HoldSumSec uint64
	DropSumSec uint64
	DropCount  uint64
	UniqueIPs  int

	// Concentration — событий на один уникальный IP.
	// Маркер ротации: низкая = много IP по чуть-чуть (ботнет),
	// высокая = мало IP по много (резидент).
	Concentration float64
}

// IPReport — одна строка отчёта по IP.
type IPReport struct {
	IP             string
	Country        string
	Events         uint64
	HoldSumSec     uint64
	AvgRTTMs       float64
	TopCloseReason string
	DropCount      uint64
	LastSeen       time.Time
}

// MacroReport — сводные цифры за период.
type MacroReport struct {
	Events          uint64
	ValidEvents     uint64
	HoldSumSec      uint64
	DropSumSec      uint64
	DropCount       uint64
	AvgDropSec      float64
	Behavior        map[string]uint64
	BehaviorDisplay []BehaviorDisplay
	UniqueCountries int
	UniqueIPs       int
	WindowHours     int
}

// BehaviorDisplay описывает тактику с процентной долей для отображения.
type BehaviorDisplay struct {
	Name    string
	Count   uint64
	Percent float64
}

// =====================================================================
// ПОСТРОЕНИЕ ОТЧЁТОВ
// =====================================================================

// BuildMacroReport суммирует бакеты за указанный период.
func (r *Rollup) BuildMacroReport(rng ReportRange) MacroReport {
	if r == nil {
		return MacroReport{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := MacroReport{Behavior: make(map[string]uint64)}
	countries := make(map[string]struct{})
	ips := make(map[string]struct{})

	loc := time.UTC
	if r.loc != nil {
		loc = r.loc
	}
	now := time.Now().In(loc)

	switch rng {
	case Range24h:
		out.WindowHours = 24
		for _, h := range r.hours {
			if h == nil || h.Hour.After(now) {
				continue
			}
			if h.Hour.Before(now.Add(-24 * time.Hour)) {
				continue
			}
			mergeHour(&out, h, countries, ips)
		}
	case Range7d:
		out.WindowHours = 7 * 24
		for _, d := range r.days {
			if d == nil || d.Day.After(now) {
				continue
			}
			if d.Day.Before(now.Add(-7 * 24 * time.Hour)) {
				continue
			}
			mergeDay(&out, d, countries, ips)
		}
	case Range30d:
		out.WindowHours = 30 * 24
		for _, d := range r.days {
			if d == nil || d.Day.After(now) {
				continue
			}
			if d.Day.Before(now.Add(-30 * 24 * time.Hour)) {
				continue
			}
			mergeDay(&out, d, countries, ips)
		}
	}

	out.UniqueCountries = len(countries)
	out.UniqueIPs = len(ips)
	if out.DropCount > 0 {
		out.AvgDropSec = float64(out.DropSumSec) / float64(out.DropCount)
	}
	// Вычисляем процентные доли тактик.
	totalBehavior := uint64(0)
	for _, v := range out.Behavior {
		totalBehavior += v
	}
	for k, v := range out.Behavior {
		percent := float64(0)
		if totalBehavior > 0 {
			percent = float64(v) / float64(totalBehavior) * 100
		}
		out.BehaviorDisplay = append(out.BehaviorDisplay, BehaviorDisplay{
			Name:    k,
			Count:   v,
			Percent: percent,
		})
	}
	// Сортируем по убыванию Count.
	sort.Slice(out.BehaviorDisplay, func(i, j int) bool {
		return out.BehaviorDisplay[i].Count > out.BehaviorDisplay[j].Count
	})

	return out
}

func mergeHour(m *MacroReport, h *HourAggBucket, countries, ips map[string]struct{}) {
	m.Events += h.Events
	m.ValidEvents += h.Valid
	m.HoldSumSec += h.HoldSum
	m.DropSumSec += h.DropSum
	m.DropCount += h.DropCount
	for k, v := range h.Behavior {
		m.Behavior[k] += v
	}
	for code, cb := range h.Countries {
		countries[code] = struct{}{}
		for ip := range cb.UniqueIPs {
			ips[ip] = struct{}{}
		}
	}
}

func mergeDay(m *MacroReport, d *DayAggBucket, countries, ips map[string]struct{}) {
	m.Events += d.Events
	m.ValidEvents += d.Valid
	m.HoldSumSec += d.HoldSum
	m.DropSumSec += d.DropSum
	m.DropCount += d.DropCount
	for k, v := range d.Behavior {
		m.Behavior[k] += v
	}
	for code, cb := range d.Countries {
		countries[code] = struct{}{}
		for ip := range cb.UniqueIPs {
			ips[ip] = struct{}{}
		}
	}
}

// BuildCountryReport возвращает топ стран за период.
func (r *Rollup) BuildCountryReport(rng ReportRange, limit int) []CountryReport {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	aggregated := make(map[string]*CountryReport)
	loc := time.UTC
	if r.loc != nil {
		loc = r.loc
	}
	now := time.Now().In(loc)

	addCountry := func(cb *CountryBucket) {
		if cb == nil || cb.Code == "" {
			return
		}
		cr := aggregated[cb.Code]
		if cr == nil {
			cr = &CountryReport{Code: cb.Code}
			aggregated[cb.Code] = cr
		}
		cr.Events += cb.Events
		cr.HoldSumSec += cb.HoldSumSec
		cr.DropSumSec += cb.DropSumSec
		cr.DropCount += cb.DropCount
		for ip := range cb.UniqueIPs {
			_ = ip
			// UniqueIPs per country — считаем через map на уровне агрегата,
			// но для простоты берём максимум по бакетам (приблизительно).
		}
	}

	switch rng {
	case Range24h:
		for _, h := range r.hours {
			if h == nil || h.Hour.After(now) || h.Hour.Before(now.Add(-24*time.Hour)) {
				continue
			}
			for _, cb := range h.Countries {
				addCountry(cb)
			}
		}
	case Range7d:
		for _, d := range r.days {
			if d == nil || d.Day.After(now) || d.Day.Before(now.Add(-7*24*time.Hour)) {
				continue
			}
			for _, cb := range d.Countries {
				addCountry(cb)
			}
		}
	case Range30d:
		for _, d := range r.days {
			if d == nil || d.Day.After(now) || d.Day.Before(now.Add(-30*24*time.Hour)) {
				continue
			}
			for _, cb := range d.Countries {
				addCountry(cb)
			}
		}
	}

	// Пересчитаем UniqueIPs честно: пройдём по бакетам ещё раз.
	// Это второй проход, но карта стран маленькая, поэтому быстро.
	ipPerCountry := make(map[string]map[string]struct{})
	addIPs := func(cb *CountryBucket) {
		if cb == nil {
			return
		}
		if ipPerCountry[cb.Code] == nil {
			ipPerCountry[cb.Code] = make(map[string]struct{})
		}
		for ip := range cb.UniqueIPs {
			ipPerCountry[cb.Code][ip] = struct{}{}
		}
	}
	switch rng {
	case Range24h:
		for _, h := range r.hours {
			if h == nil || h.Hour.After(now) || h.Hour.Before(now.Add(-24*time.Hour)) {
				continue
			}
			for _, cb := range h.Countries {
				addIPs(cb)
			}
		}
	case Range7d:
		for _, d := range r.days {
			if d == nil || d.Day.After(now) || d.Day.Before(now.Add(-7*24*time.Hour)) {
				continue
			}
			for _, cb := range d.Countries {
				addIPs(cb)
			}
		}
	case Range30d:
		for _, d := range r.days {
			if d == nil || d.Day.After(now) || d.Day.Before(now.Add(-30*24*time.Hour)) {
				continue
			}
			for _, cb := range d.Countries {
				addIPs(cb)
			}
		}
	}
	for code, cr := range aggregated {
		cr.UniqueIPs = len(ipPerCountry[code])
		if cr.UniqueIPs > 0 {
			cr.Concentration = float64(cr.Events) / float64(cr.UniqueIPs)
		}
	}

	out := make([]CountryReport, 0, len(aggregated))
	for _, cr := range aggregated {
		out = append(out, *cr)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Events != out[j].Events {
			return out[i].Events > out[j].Events
		}
		return out[i].Code < out[j].Code
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// BuildIPReport возвращает топ IP за период.
func (r *Rollup) BuildIPReport(rng ReportRange, limit int) []IPReport {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	type ipAgg struct {
		Events       uint64
		HoldSumSec   uint64
		RTTSumMs     uint64
		RTTCount     uint64
		DropCount    uint64
		CloseReasons map[string]uint64
		CountryVotes map[string]uint64
		LastSeen     time.Time
	}
	aggregated := make(map[string]*ipAgg)
	loc := time.UTC
	if r.loc != nil {
		loc = r.loc
	}
	now := time.Now().In(loc)

	addIP := func(ip *IPBucket) {
		if ip == nil || ip.IP == "" {
			return
		}
		a := aggregated[ip.IP]
		if a == nil {
			a = &ipAgg{CloseReasons: make(map[string]uint64), CountryVotes: make(map[string]uint64)}
			aggregated[ip.IP] = a
		}
		a.Events += ip.Events
		if ip.Country != "" {
			// Голосуем событиями бакета: доминирует страна с наибольшим весом.
			a.CountryVotes[ip.Country] += ip.Events
		}
		a.HoldSumSec += ip.HoldSumSec
		a.RTTSumMs += ip.RTTSumMs
		a.RTTCount += ip.RTTCount
		a.DropCount += ip.DropCount
		for k, v := range ip.CloseReasons {
			a.CloseReasons[k] += v
		}
		if ip.LastSeen.After(a.LastSeen) {
			a.LastSeen = ip.LastSeen
		}
	}

	switch rng {
	case Range24h:
		for _, h := range r.hours {
			if h == nil || h.Hour.After(now) || h.Hour.Before(now.Add(-24*time.Hour)) {
				continue
			}
			for _, ip := range h.IPs {
				addIP(ip)
			}
		}
	case Range7d:
		for _, d := range r.days {
			if d == nil || d.Day.After(now) || d.Day.Before(now.Add(-7*24*time.Hour)) {
				continue
			}
			for _, ip := range d.IPs {
				addIP(ip)
			}
		}
	case Range30d:
		for _, d := range r.days {
			if d == nil || d.Day.After(now) || d.Day.Before(now.Add(-30*24*time.Hour)) {
				continue
			}
			for _, ip := range d.IPs {
				addIP(ip)
			}
		}
	}

	out := make([]IPReport, 0, len(aggregated))
	for ipStr, a := range aggregated {
		var avgRTT float64
		if a.RTTCount > 0 {
			avgRTT = float64(a.RTTSumMs) / float64(a.RTTCount)
		}
		topCR := topCloseReason(a.CloseReasons)
		out = append(out, IPReport{
			IP:             ipStr,
			Country:        dominantCountry(a.CountryVotes),
			Events:         a.Events,
			HoldSumSec:     a.HoldSumSec,
			AvgRTTMs:       avgRTT,
			TopCloseReason: topCR,
			DropCount:      a.DropCount,
			LastSeen:       a.LastSeen,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Events != out[j].Events {
			return out[i].Events > out[j].Events
		}
		return out[i].IP < out[j].IP
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// dominantCountry возвращает страну с наибольшим весом событий.
// При равенстве весов побеждает лексически меньший код — детерминизм вывода.
func dominantCountry(votes map[string]uint64) string {
	best := ""
	var bestCount uint64
	for country, count := range votes {
		if count > bestCount || (count == bestCount && count > 0 && country < best) {
			best = country
			bestCount = count
		}
	}
	return best
}

func topCloseReason(cr map[string]uint64) string {
	if len(cr) == 0 {
		return "—"
	}
	top := ""
	var topCount uint64
	for k, v := range cr {
		if v > topCount {
			topCount = v
			top = k
		}
	}
	if top == "" {
		return "—"
	}
	return top
}
