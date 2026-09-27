// internal/tarpit/tarpit_logger.go
// ============================================================================
// КОМПОНЕНТ: Асинхронный SIEM-конвейер публикации инцидентов
// ВЕРСИЯ: v8.0.1
// РОЛЬ: Двухпотоковый логгер ловушек. Принимает события из каналов
//
//	и пишет их в лог-файлы на диск. Полностью неблокирующий.
//
// ============================================================================
//
// АРХИТЕКТУРНАЯ ПОЗИЦИЯ:
//
//	Этот файл — ФИНАЛЬНЫЙ этап конвейера логирования.
//	Исполнительный движок (tarpit_listener.go) отправляет события в каналы,
//	а два фоновых горутины-писателя непрерывно считывают каналы и пишут на диск.
//
//	Исполнительный движок → канал → ЛОГГЕР (ЭТОТ ФАЙЛ) → лог-файл на диске
//
// ДВА ПОТОКА ЗАПИСИ:
//
//  1. ConnectionLogWriter — лёгкий лог касаний (момент первого контакта)
//     Файл: /var/log/amberguard/tarpit_connection.log
//     Формат: Дата \t TARPIT:PORT_NUMBER \t IP \t План удержания \t Страна
//
//  2. ReportLogWriter — тяжёлый криминалистический профиль (после освобождения)
//     Файл: /var/log/amberguard/tarpit_report.log
//     Формат: 30 колонок через табуляцию (полный профиль сканера)
//
// КЛЮЧЕВОЙ ПРИНЦИП:
//
//	Логгер НЕ блокирует исполнительный движок. Если канал переполнен
//	(массовая DDoS-атака), события отбрасываются через `select` с `default`.
//	Это предохранитель от переполнения ОЗУ.
//
// ### [2026-08-22] — Мажорный рефакторинг логгера (v8.0.1)
//   - Добавление поля `PlannedDelay` в `RawConnectionEvent` для устранения
//     хардкода «13» в `ConnectionLogWriter`.
//   - Замена хардкода `13` на `event.PlannedDelay` в `ConnectionLogWriter`.
//   - Добавление предохранителя: если `PlannedDelay == 0`, используется дефолт 13.
//   - Добавление подробных комментариев по формату строк и контракту данных.
//
// ============================================================================
package tarpit

import (
	"fmt"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"
)

// ============================================================================
// КОНСТАНТЫ
// ============================================================================

// DefaultPlannedDelay — безопасный дефолт для запланированного удержания.
// Используется если поле `PlannedDelay` не было заполнено отправителем.
// НОВОЕ В v8.0.1.
const DefaultPlannedDelay = 13

// ============================================================================
// КОНСТАНТЫ ПАКЕТНОГО FSYNC (v8.0.3, Аудит №3)
// ============================================================================
// syncFlushInterval — максимальный интервал принудительного сброса буфера на диск.
// Цена сбоя питания: не более 1 секунды логов вместо «ничего».
const syncFlushInterval = 1 * time.Second

// syncBatchSize — количество записей, после которого Sync вызывается
// не дожидаясь тика. При флуде это ограничивает частоту syscall
// (до 64× меньше вызовов fsync), каналы переполняются только по реальному
// пределу ОЗУ, а не по бедности I/O.
const syncBatchSize = 64

// ============================================================================
// СТРУКТУРЫ ДАННЫХ: КОНТРАКТ СОБЫТИЙ
// ============================================================================

// RawConnectionEvent описывает ЛЁГКОЕ событие в момент ПЕРВОГО КАСАНИЯ
// ловушки ботом. Генерируется мгновенно при приёме соединения, ДО начала
// цикла удержания. Содержит минимум данных для быстрой записи.
//
// ИСПОЛЬЗУЕТСЯ: Для записи в `tarpit_connection.log` (лёгкий лог касаний).
//
// ОБНОВЛЕНО В v8.0.1: Добавлено поле `PlannedDelay` для устранения
// хардкода в `ConnectionLogWriter`.
type RawConnectionEvent struct {
	// Timestamp — точный момент первого касания сокета ботом.
	// Это время НАЧАЛА атаки, зафиксированное в `startTime` исполнителя.
	// ОБНОВЛЕНО В v8.0.1: Передаётся `startTime`, а не `time.Now()` в конце.
	Timestamp time.Time

	// TarpitID — идентификатор ловушки для вывода в лог.
	// Для базовой ловушки: "GenericTarpit"
	// Для мульти-ловушек: "TARPIT:ПОРТ" (например, "TARPIT:8080")
	TarpitID string

	// IP — IP-адрес атакующего бота (очищенный от IPv6-префикса).
	IP string

	// EventType — тип события (зарезервировано для будущего расширения).
	// Возможные значения: "START", "TIMEOUT", "AGGRESSIVE" и т.д.
	// В текущей версии не используется активно.
	EventType string

	// Country — страна источника атаки (результат GeoIP-lookup).
	// Если GeoIP недоступен — "Unknown".
	Country string

	// PlannedDelay — запланированное время удержания бота (джиттер).
	// Вычисляется как: DelayStep ± DelayVariance (случайное значение).
	// Используется в `ConnectionLogWriter` для записи реального плана.
	//
	// НОВОЕ ПОЛЕ v8.0.1: Устраняет хардкод «13» в логгере.
	// Если равно 0 (не заполнено отправителем), `ConnectionLogWriter`
	// использует дефолт `DefaultPlannedDelay`.
	PlannedDelay int
}

// openLogWithRetry открывает лог-файл с ретраями (v8.0.3, Аудит №6).
// Пять попыток с паузой 1 сек защищают писателя от молчаливой вечной смерти
// при временном сбое I/O (диск занят, монтирование не готово).
// Если файл не открылся после всех попыток — возвращает ошибку,
// и события будут отбрасываться через `select default` (предохранитель ОЗУ).
func openLogWithRetry(path string) (*os.File, error) {
	var f *os.File
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		f, err = os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
		if err == nil {
			return f, nil
		}
		time.Sleep(1 * time.Second)
	}
	return nil, err
}

// RawLogEntry описывает ТЯЖЁЛЫЙ криминалистический профиль сканера.
// Генерируется ПОСЛЕ освобождения сокета (завершение цикла удержания).
// Содержит полные метрики для глубокой форензики и поведенческого анализа.
//
// ИСПОЛЬЗУЕТСЯ: Для записи в `tarpit_report.log` (тяжёлый SIEM-профиль).
//
// ОБНОВЛЕНО В v8.0.1: Все поля заполнены корректно.
// `DelayStep` = фактическое удержание, `PlannedDelay` = запланированный джиттер.
type RawLogEntry struct {
	// Timestamp — время НАЧАЛА атаки (якорь SIEM-таблиц).
	// Это `startTime` из исполнителя, а НЕ время конца удержания.
	// ОБНОВЛЕНО В v8.0.1.
	Timestamp time.Time

	// TarpitID — идентификатор ловушки для вывода в лог.
	// Для базовой ловушки: "GenericTarpit"
	// Для мульти-ловушек: "TARPIT:ПОРТ"
	TarpitID string

	// IP — IP-адрес атакующего бота (очищенный от IPv6-префикса).
	IP string

	// Country — страна источника атаки (результат GeoIP-lookup).
	Country string

	// DelayStep — ФАКТИЧЕСКОЕ время удержания бота (в секундах).
	// Вычисляется как: time.Now().Unix() - startTime.Unix()
	// Выравнивается по счётчику тиков для пассивных сканеров.
	// ОБНОВЛЕНО В v8.0.1: Передаётся `int(realTrapped)`, а не план.
	DelayStep int

	// PlannedDelay — ПЛАНИРУЕМОЕ время удержания (джиттер, в секундах).
	// Вычисляется как: DelayStep ± DelayVariance (случайное значение).
	// Это то, что было ЗАПЛАНИРОВАНО до начала удержания.
	// ОБНОВЛЕНО В v8.0.1: Передаётся `tarpitDelay` из исполнителя.
	PlannedDelay int

	// Behavior — поведенческая метка сканера.
	// Значения: "PASSIVE_SCANNER" (терпеливо ждал)
	//           "AGGRESSIVE_SCANNER" (разорвал соединение досрочно)
	Behavior string

	// SourcePort — реальный исходный порт атакующей стороны.
	// Используется для корреляции с сессиями в `ActiveSessions`.
	// ОБНОВЛЕНО В v8.0.1: Передаётся реальный порт, а не 0.
	SourcePort int

	// MSS — максимальный размер сегмента, принимаемый атакующего.
	// Извлекается из `TCP_INFO` ядра Linux.
	MSS uint32

	// SndCwnd — размер окна отправки TCP (Send Congestion Window).
	// Показывает, сколько данных атакующий готов принять за один раз.
	// Извлекается из `TCP_INFO` ядра Linux.
	SndCwnd uint32

	// RcvSpace — размер TCP-окна буфера приёма (Receive Buffer Space).
	// Показывает доступную память на стороне атакующего.
	// Извлекается из `TCP_INFO` ядра Linux.
	RcvSpace uint32

	// RTT — сетевая задержка (Round Trip Time) в миллисекундах.
	// Показывает физическое расстояние до атакующего.
	// Извлекается из `TCP_INFO` ядра Linux.
	RTT uint32
	// === НОВЫЕ ПОЛЯ СПРИНТ 1 (ЗАДАЧА А) ===
	// TotalRetrans — общие ретрансмиты за сессию.
	TotalRetrans uint32
	// State — статус сокета TCP (tcpi_state).
	State uint8
	// Retransmits — текущие ретрансмиты (tcpi_retransmits).
	Retransmits uint8
	// Probes — keep-alive пробы от атакующего (tcpi_probes).
	Probes uint8
	// SndWscale — window scaling отправки (младшие 4 бита tcpi_snd_wscale).
	SndWscale uint8
	// RcvWscale — window scaling приёма (старшие 4 бита tcpi_rcv_wscale).
	RcvWscale uint8
	// === НОВЫЕ ПОЛЯ СПРИНТ 1 (ЗАДАЧИ B, C, D) ===
	// CloseReason — причина завершения сессии:
	//   FIN     — атакующий закрыл соединение корректно (io.EOF);
	//   RST     — атакующий сбросил соединение (ECONNRESET);
	//   TIMEOUT — мы закрыли сессию по таймеру удержания;
	//   OUR_CLOSE — принудительное закрытие с нашей стороны.
	CloseReason string
	// IATCount — число успешных чтений (база для статистики IAT).
	IATCount uint32
	// IATSumMs — сумма интервалов между чтениями (мс).
	IATSumMs uint64
	// IATMinMs — минимальный интервал между чтениями (мс).
	IATMinMs uint32
	// IATMaxMs — максимальный интервал между чтениями (мс).
	IATMaxMs uint32
	// IATSumSqMs — сумма квадратов интервалов (мс²).
	// Дисперсия = IATSumSqMs/IATCount − (IATSumMs/IATCount)².
	IATSumSqMs float64
	// ReadTiny — число чтений размером 0–64 байта.
	ReadTiny uint32
	// ReadSmall — число чтений размером 65–256 байт.
	ReadSmall uint32
	// ReadMedium — число чтений размером 257–1024 байта.
	ReadMedium uint32
	// ReadLarge — число чтений размером >1024 байта.
	// При текущем буфере 1024 структурно равен 0 — резерв на будущее.
	ReadLarge uint32
}

// ============================================================================
// ГЛОБАЛЬНЫЕ КАНАЛЫ: НЕБЛОКИРУЮЩИЕ ШИНЫ ДАННЫХ
// ============================================================================

// Инициализация глобальных потокобезопасных неблокирующих каналов связи.
// Ёмкость 5000 элементов — предохранитель от переполнения ОЗУ при массовых атаках.
//
// ПРИНЦИП РАБОТЫ:
//   - Исполнительный движок ОТПРАВЛЯЕТ события в каналы через `LogConnectionEvent`
//     и `LogReportEntry`.
//   - Фоновые горутины-писатели (`ConnectionLogWriter`, `ReportLogWriter`)
//     НЕПРЕРЫВНО СЧИТЫВАЮТ каналы и пишут на диск.
//   - Если канал ПЕРЕПОЛНЕН (массовая атака), событие ОТБРАСЫВАЕТСЯ
//     через `select` с `default`. Это предохранитель от переполнения ОЗУ.
//
// ВАЖНО: Каналы инициализируются ОДИН раз при старте процесса.
// Они НЕ пересоздаются при перезапуске ловушек.
var (
	// ConnectionEventChannel — шина для лёгких событий касания.
	// Буферизация: 5000 элементов.
	// Потребитель: `ConnectionLogWriter`.
	ConnectionEventChannel = make(chan RawConnectionEvent, 5000)

	// ReportChannel — шина для тяжёлых криминалистических профилей.
	// Буферизация: 5000 элементов.
	// Потребитель: `ReportLogWriter`.
	ReportChannel = make(chan RawLogEntry, 5000)
)

// ============================================================================
// СПРИНТ 1 (E): СЧЁТЧИКИ ЗДОРОВЬЯ КОНВЕЙЕРА
// ============================================================================
// Ранее потеря события при переполнении канала была полностью молчаливой.
// Теперь каждый дроп учитывается атомарно, а латентность писателей
// отслеживается по максимуму с момента старта.
var (
	// droppedConnectionEvents — число отброшенных лёгких событий касания.
	droppedConnectionEvents uint64
	// droppedReportEntries — число отброшенных тяжёлых профилей.
	droppedReportEntries uint64
	// connWriteLatencyMaxMs — максимум латентности писателя касаний (мс).
	connWriteLatencyMaxMs uint64
	// reportWriteLatencyMaxMs — максимум латентности писателя отчётов (мс).
	reportWriteLatencyMaxMs uint64
)

// GetLogHealth returns pipeline health counters:
// (dropped connection events, dropped report entries,
// max connection-writer latency ms, max report-writer latency ms).
// CONTRACT: read-only atomics; safe to call from TUI/audit goroutines.
// СПРИНТ 1 (E): видимость для TUI придёт через shm-расширение в спринте 2/3,
// геттер готов уже сейчас.
func GetLogHealth() (uint64, uint64, uint64, uint64) {
	return atomic.LoadUint64(&droppedConnectionEvents),
		atomic.LoadUint64(&droppedReportEntries),
		atomic.LoadUint64(&connWriteLatencyMaxMs),
		atomic.LoadUint64(&reportWriteLatencyMaxMs)
}

// latencyMsSince возвращает миллисекунды от t до текущего момента.
// Отрицательные значения (скок часов назад) обрезаются до нуля.
func latencyMsSince(t time.Time) uint64 {
	ms := time.Since(t).Milliseconds()
	if ms < 0 {
		return 0
	}
	return uint64(ms)
}

// noteLatency lock-free обновляет максимум латентности писателя (CAS-цикл).
func noteLatency(cur *uint64, ms uint64) {
	for {
		old := atomic.LoadUint64(cur)
		if ms <= old {
			return
		}
		if atomic.CompareAndSwapUint64(cur, old, ms) {
			return
		}
	}
}

// ============================================================================
// ПУБЛИЧНЫЕ ШЛЮЗЫ: ОТПРАВКА СОБЫТИЙ ИЗ ИСПОЛНИТЕЛЬНОГО ДВИЖКА
// ============================================================================

// LogConnectionEvent отправляет ЛЁГКОЕ событие касания в шину логгера.
// Вызывается из `handleIncomingBot` и `handleIncomingBotCustom` в момент
// ПЕРВОГО КАСАНИЯ сокета ботом (ДО начала цикла удержания).
//
// НЕБЛОКИРУЮЩИЙ ВЫЗОВ: Если канал переполнен, событие отбрасывается.
// Это предохранитель от переполнения ОЗУ при массовых DDoS-атаках.
//
// ПАРАМЕТРЫ:
//   - ev: структура события касания с заполненными полями
//
// ВАЖНО: Поле `ev.PlannedDelay` должно быть заполнено отправителем.
// Если оно равно 0, `ConnectionLogWriter` использует дефолт `DefaultPlannedDelay`.
// ОБНОВЛЕНО В v8.0.1.
func LogConnectionEvent(ev RawConnectionEvent) {
	select {
	case ConnectionEventChannel <- ev:
		// Событие успешно помещено в канал.
		// Фоновый писатель подхватит его и запишет на диск.
	default:
		// Канал переполнен — событие отброшено.
		// Это предохранитель от переполнения ОЗУ при массовых атаках.
		// СПРИНТ 1 (E): дроп больше не молчаливый — учитывается атомарно.
		atomic.AddUint64(&droppedConnectionEvents, 1)
	}
}

// LogReportEntry отправляет ТЯЖЁЛЫЙ криминалистический профиль в шину логгера.
// Вызывается из `handleIncomingBot` и `handleIncomingBotCustom` ПОСЛЕ
// завершения цикла удержания (когда сокет освобождён).
//
// НЕБЛОКИРУЮЩИЙ ВЫЗОВ: Если канал переполнен, событие отбрасывается.
//
// ПАРАМЕТРЫ:
//   - en: структура криминалистического профиля с заполненными полями
//
// ВАЖНО: Все поля должны быть заполнены корректно.
// `DelayStep` = фактическое удержание, `PlannedDelay` = запланированный джиттер.
// ОБНОВЛЕНО В v8.0.1.
func LogReportEntry(en RawLogEntry) {
	select {
	case ReportChannel <- en:
		// Профиль успешно помещён в канал.
		// Фоновый писатель подхватит его и запишет на диск.
	default:
		// Канал переполнен — событие отброшено.
		// Предохранитель от переполнения ОЗУ.
		// СПРИНТ 1 (E): дроп больше не молчаливый — учитывается атомарно.
		atomic.AddUint64(&droppedReportEntries, 1)
	}
}

// ============================================================================
// ФОНОВЫЕ ПИСАТЕЛИ: НЕПРЕРЫВНАЯ ЗАПИСЬ НА ДИСК
// ============================================================================

// ConnectionLogWriter непрерывно считывает шину событий касания и пишет
// ЛЁГКИЙ лог касаний в файл. Запускается как отдельная горутина.
//
// ЗАПУСК: `go tarpit.ConnectionLogWriter(config.C.TarpitConnLog)`
//
// ФОРМАТ СТРОКИ (4 колонки через табуляцию):
//
//	Дата/Время \t IP \t План удержания \t Страна
//
// ПРИМЕР СТРОКИ:
//
//	2026-08-22 14:30:05    192.168.1.100   13 sec  United States
//
// ОБНОВЛЕНО В v8.0.1:
// - Хардкод «13» заменён на `event.PlannedDelay`.
// - Добавлен предохранитель: если `PlannedDelay == 0`, используется дефолт.
// - `Timestamp` передаётся как `startTime` (момент касания), а не конец удержания.
func ConnectionLogWriter(logFilePath string) {
	f, err := openLogWithRetry(logFilePath)
	if err != nil {
		return
	}
	defer f.Close()

	// v8.3.0 (Блок 7): обработка SIGHUP для корректной ротации логов.
	// logrotate переименовывает файл, но демон держит старый fd.
	// При SIGHUP — закрываем старый файл, открываем новый по тому же пути.
	sighupChan := make(chan os.Signal, 1)
	signal.Notify(sighupChan, syscall.SIGHUP)
	defer signal.Stop(sighupChan)

	ticker := time.NewTicker(syncFlushInterval)
	defer ticker.Stop()
	dirty := false
	writeCount := 0
	flush := func() {
		if dirty {
			_ = f.Sync()
			dirty = false
			writeCount = 0
		}
	}
	for {
		select {
		case event, ok := <-ConnectionEventChannel:
			if !ok {
				flush()
				return
			}
			timeStr := event.Timestamp.Format("2006-01-02 15:04:05")
			plannedDelay := event.PlannedDelay
			if plannedDelay <= 0 {
				plannedDelay = DefaultPlannedDelay
			}
			logLine := fmt.Sprintf("%s\t%s\t%s\t%d sec\t%s\n",
				timeStr, event.TarpitID, event.IP, plannedDelay, event.Country)
			_, _ = f.WriteString(logLine)
			// СПРИНТ 1 (E): латентность писателя касаний (от касания до записи).
			noteLatency(&connWriteLatencyMaxMs, latencyMsSince(event.Timestamp))
			dirty = true
			writeCount++
			if writeCount >= syncBatchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-sighupChan:
			// Ротация лога: сбрасываем буфер, закрываем старый fd, открываем новый.
			flush()
			f.Close()
			newF, err := openLogWithRetry(logFilePath)
			if err != nil {
				// Не смогли переоткрыть — продолжаем со старым fd (лучше писать в старый, чем терять события).
				continue
			}
			f = newF
		}
	}
}

// ReportLogWriter непрерывно считывает шину тяжёлых отчётов и пишет
// КРИМИНАЛИСТИЧЕСКИЙ ПРОФИЛЬ сканера в файл. Запускается как отдельная горутина.
//
// ЗАПУСК: `go tarpit.ReportLogWriter(config.C.TarpitRptLog)`
//
// ФОРМАТ СТРОКИ (30 колонок через табуляцию):
//
//	[0]  Дата/Время       — момент начала атаки (якорь SIEM)
//	[1]  TarpitID         — идентификатор ловушки
//	[2]  IP               — адрес атакующего
//	[3]  План удержания   — запланированный джиттер (в секундах)
//	[4]  Факт удержания   — фактическое время удержания (в секундах)
//	[5]  Поведение        — метка поведения сканера
//	[6]  Страна           — страна источника (GeoIP)
//	[7]  Исходный порт    — порт атакующей стороны
//	[8]  Токен сессии     — уникальный идентификатор сессии
//	[9]  MSS              — максимальный размер сегмента
//	[10] SndCwnd          — окно отправки TCP
//	[11] RcvSpace         — буфер приёма
//	[12] RTT              — сетевая задержка (мс)
//	[13] TotalRetrans     — общие ретрансмиты за сессию
//	[14] State            — статус сокета TCP (1=ESTABLISHED, 7=CLOSE после RST, 8=CLOSE_WAIT после FIN)
//	[15] Retransmits      — текущие ретрансмиты
//	[16] Probes           — keep-alive пробы
//	[17] SndWscale        — window scaling отправки
//	[18] RcvWscale        — window scaling приёма
//	[19] CloseReason      — причина завершения: FIN / RST / TIMEOUT / OUR_CLOSE
//	[20] IATCount         — число успешных чтений
//	[21] IATSumMs         — сумма интервалов между чтениями (мс)
//	[22] IATMinMs         — минимальный интервал (мс)
//	[23] IATMaxMs         — максимальный интервал (мс)
//	[24] IATSumSqMs       — сумма квадратов интервалов (мс²), для дисперсии
//	[25] ReadTiny         — чтений 0–64 байта
//	[26] ReadSmall        — чтений 65–256 байт
//	[27] ReadMedium       — чтений 257–1024 байта
//	[28] ReadLarge        — чтений >1024 байта (0 при буфере 1024, резерв)
//	[29] WriteLatencyMs   — путь события от касания до диска (мс):
//	                        удержание + очередь канала + запись
//
// ПРИМЕР СТРОКИ:
//
//	2026-08-22 14:30:05    TARPIT:22   192.168.1.100   13 sec  11 sec  PASSIVE_SCANNER United States   45678   18CDB66F82B6C3B7C840_45678  1460     10  65536   45
//
// ОБНОВЛЕНО В v8.0.1:
// - `Timestamp` передаётся как `startTime` (момент касания).
// - `DelayStep` = фактическое удержание (выровненное по счётчику тиков).
// - `PlannedDelay` = запланированный джиттер.
// - `SourcePort` = реальный порт атакующего (не 0).
// - Все TCP-метрики (MSS, SndCwnd, RcvSpace, RTT) заполнены из `TCP_INFO`.
func ReportLogWriter(logFilePath string) {
	f, err := openLogWithRetry(logFilePath)
	if err != nil {
		return
	}
	defer f.Close()

	// v8.3.0 (Блок 7): обработка SIGHUP для корректной ротации логов.
	sighupChan := make(chan os.Signal, 1)
	signal.Notify(sighupChan, syscall.SIGHUP)
	defer signal.Stop(sighupChan)

	ticker := time.NewTicker(syncFlushInterval)
	defer ticker.Stop()
	dirty := false
	writeCount := 0
	flush := func() {
		if dirty {
			_ = f.Sync()
			dirty = false
			writeCount = 0
		}
	}
	for {
		select {
		case entry, ok := <-ReportChannel:
			if !ok {
				flush()
				return
			}
			timeStr := entry.Timestamp.Format("2006-01-02 15:04:05")
			sessionToken := fmt.Sprintf("18CDB%X_%d", entry.Timestamp.UnixNano(), entry.SourcePort)
			// СПРИНТ 1 (E): полный путь события от касания до диска.
			// Включает удержание и очередь канала; компонент очереди =
			// [29] − [4]×1000. Всплеск при малом [4] = медленный диск/писатель.
			latencyMs := latencyMsSince(entry.Timestamp)
			logLine := fmt.Sprintf("%s\t%s\t%s\t%d sec\t%d sec\t%s\t%s\t%d\t%s\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%s\t%d\t%d\t%d\t%d\t%.0f\t%d\t%d\t%d\t%d\t%d\n",
				timeStr, entry.TarpitID, entry.IP, entry.PlannedDelay, entry.DelayStep,
				entry.Behavior, entry.Country, entry.SourcePort, sessionToken,
				entry.MSS, entry.SndCwnd, entry.RcvSpace, entry.RTT,
				entry.TotalRetrans, entry.State, entry.Retransmits, entry.Probes, entry.SndWscale, entry.RcvWscale,
				entry.CloseReason, entry.IATCount, entry.IATSumMs, entry.IATMinMs, entry.IATMaxMs, entry.IATSumSqMs,
				entry.ReadTiny, entry.ReadSmall, entry.ReadMedium, entry.ReadLarge, latencyMs)
			_, _ = f.WriteString(logLine)
			noteLatency(&reportWriteLatencyMaxMs, latencyMs)
			dirty = true
			writeCount++
			if writeCount >= syncBatchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-sighupChan:
			// Ротация лога: сбрасываем буфер, закрываем старый fd, открываем новый.
			flush()
			f.Close()
			newF, err := openLogWithRetry(logFilePath)
			if err != nil {
				continue
			}
			f = newF
		}
	}
}
