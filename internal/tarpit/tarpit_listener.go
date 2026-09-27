// internal/tarpit/tarpit_listener.go
// ============================================================================
// КОМПОНЕНТ: Исполнительный Асинхронный Слушатель Ловушек (Исполнительный движок)
// ВЕРСИЯ: v8.0.1
// РОЛЬ: Высокоскоростный асинхронный перехватчик сокетов на базе RAM-маппинга.
//
//	Содержит логику удержания бота в дегтярной яме.
//
// ============================================================================
//
// АРХИТЕКТУРНАЯ ПОЗИЦИЯ:
//
//	Этот файл — ИСПОЛНИТЕЛЬНЫЙ движок ловушек.
//	Он вызывается из `tarpit_manager.go` (демон) для каждого входящего соединения.
//
//	Демон (tarpit_manager.go) → принимает соединение → вызывает
//	`handleIncomingBotCustom()` → удерживает бота → пишет логи
//
// ВАЖНОЕ ИЗМЕНЕНИЕ v8.0.1:
//
//	`StartTarpitCoreListener` помечена как LEGACY. В новой архитектуре
//	базовая ловушка порта 22 управляется через демон как «нулевой воркер»
//	с флагом `is_core = true`. Функция сохранена для обратной совместимости.
//
// ### [2026-08-22] — Мажорный рефакторинг исполнительного движка (v8.0.1)
//   - Переименование `cfg.Jitter` → `cfg.DelayVariance` в `handleIncomingBotCustom`.
//   - Добавление поля `MaxConn` в `defaultCoreConfig` структуры `TarpitConfig`.
//   - Фикс Проблемы 1: хронометрия в `handleIncomingBot` выровнена по эталону.
//     `Timestamp` = `startTime`, `DelayStep` = `int(realTrapped)`,
//     `PlannedDelay` = `tarpitDelay`, `SourcePort` и TCP-метрики заполнены.
//   - Добавление поля `PlannedDelay` в `RawConnectionEvent` для устранения
//     хардкода `13` в `ConnectionLogWriter`.
//   - `StartTarpitCoreListener` помечена как LEGACY для обратной совместимости.
//
// ============================================================================
package tarpit

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall" // для обеспечения работы syscall.Conn и ECONNRESET
	"time"
	"unsafe"

	"agl/config"
	"agl/i18n"
	"agl/internal/sys"
)

// ============================================================================
// КОНСТАНТЫ
// ============================================================================

const (
	// maxDelayStep / minDelayStep — границы джиттера для защиты от
	// некорректных значений конфигурации
	maxDelayStep = 500
	minDelayStep = 1

	// defaultGeoTool — дефолтная утилита гео-аналитики
	defaultGeoTool = "geoiplookup"
)

// ============================================================================
// ГЛОБАЛЬНЫЕ ПЕРЕМЕННЫЕ
// ============================================================================

// ActiveSessions — глобальный реестр активных сессий в памяти для TUI-панели.
// Ключ: sourcePort (int), Значение: ScannerFingerprint.
var (
	ipRegex = regexp.MustCompile(`[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}`)
)

// ============================================================================
// ВСПОМОГАТЕЛЬНЫЕ ФУНКЦИИ
// ============================================================================

// geoStateMu защищает edge-triggered флаг деградации гео-аналитики
// от гонок между воркерами ловушек (БЛОК 3.1).
var geoStateMu sync.Mutex

// geoDegraded — флаг деградации гео-утилиты: один WARNING на сбой,
// один SUCCESS на восстановление, без спама на каждое подключение бота.
var geoDegraded bool

// getCountryFromGeoTool использует системные утилиты для гео-анализа.
// Возвращает имя страны или «Unknown» при сбое.
// БЛОК 3.1: внешний вызов через sys.RunTimeout; ошибка не игнорируется,
// но логируется edge-triggered (это ГОРЯЧИЙ путь воркеров).
func getCountryFromGeoTool(clientIP, geoTool string) string {
	if geoTool == "" {
		geoTool = defaultGeoTool
	}

	// Выполняем гео-запрос через единый интерфейс.
	// v8.4.4: гео ушло во второй эшелон после баннера.
	// Таймаут снижен: локальная база не требует 5 секунд,
	// а зависший гео-процесс не должен держать воркер.
	geoRes := sys.RunTimeout(700*time.Millisecond, geoTool, clientIP)

	// Сбой внешней команды: утилита отсутствует, таймаут или nonzero exit.
	if geoRes.Err != nil {
		// Один WARNING на переход в деградацию — не на каждого бота.
		geoStateMu.Lock()
		already := geoDegraded
		geoDegraded = true
		geoStateMu.Unlock()
		if !already {
			// event=geo.lookup_failed — машинный код сбоя гео-аналитики.
			geoRes.LogWarnIfErr("TARPIT", "GEO_LOOKUP", "event=geo.lookup_failed")
		}
		return "Unknown"
	}

	// Гео-утилита снова отвечает — закрываем ребро деградации одним SUCCESS.
	geoStateMu.Lock()
	wasDegraded := geoDegraded
	geoDegraded = false
	geoStateMu.Unlock()
	if wasDegraded {
		sys.LogAction("TARPIT", "GEO_RESTORED", "SUCCESS",
			i18n.TrEvent("geo.restored"))
	}

	// Разбираем вывод гео-утилиты (страна — во второй части после запятой).
	parts := strings.Split(geoRes.Stdout, ",")
	if len(parts) >= 2 {
		return strings.TrimSpace(parts[1]) // Очистка от лишних символов
	}
	// Вывод без ожидаемого формата — гео не распознано, без спама.
	return "Unknown"
}

// TcpInfoMetrics — криминалистический слепок TCP_INFO ядра Linux.
// Содержит как базовые метрики (MSS, RTT), так и расширенные (статус, ретрансмиты, wscale).
// НОВОЕ В v8.4.x (Спринт 1, задача А): добавлены бесплатные поля из struct tcp_info.
type TcpInfoMetrics struct {
	MSS          uint32
	SndCwnd      uint32
	RcvSpace     uint32
	RTT          uint32
	TotalRetrans uint32
	State        uint8
	Retransmits  uint8
	Probes       uint8
	SndWscale    uint8
	RcvWscale    uint8
}

// extractTcpInfo извлекает криминалистическую метрику TCP_INFO из ядра Linux.
// v8.2.0: прямой getsockopt через stdlib syscall — без зависимости от
// символов golang.org/x/sys/unix. ABI struct tcp_info (linux/tcp.h) —
// стабильный UAPI, смещения полей гарантированы ядром.
// ОБНОВЛЕНО В v8.4.x: возвращает структуру с расширенными полями (Спринт 1).
func extractTcpInfo(conn net.Conn) TcpInfoMetrics {
	var m TcpInfoMetrics
	sysConn, ok := conn.(syscall.Conn)
	if !ok {
		return m
	}
	rawConn, err := sysConn.SyscallConn()
	if err != nil {
		return m
	}
	_ = rawConn.Control(func(fd uintptr) {
		// Читаем первые 104 байта struct tcp_info — этого достаточно
		// для всех нужных полей (включая total_retrans на offset 100).
		buf := make([]byte, 104)
		socklen := uint32(len(buf))
		_, _, errno := syscall.Syscall6(
			syscall.SYS_GETSOCKOPT,
			fd,
			uintptr(syscall.IPPROTO_TCP),
			0xb, // TCP_INFO = 11 (linux/tcp.h)
			uintptr(unsafe.Pointer(&buf[0])),
			uintptr(unsafe.Pointer(&socklen)),
			0,
		)
		// Требуем минимум 104 байта, так как total_retrans лежит на offset 100.
		if errno != 0 || socklen < 104 {
			return
		}
		u32 := func(off int) uint32 { return binary.LittleEndian.Uint32(buf[off : off+4]) }

		// Базовые метрики
		m.MSS = u32(20)        // tcpi_rcv_mss
		m.RTT = u32(68) / 1000 // tcpi_rtt (микросекунды → миллисекунды)
		m.SndCwnd = u32(80)    // tcpi_snd_cwnd
		m.RcvSpace = u32(96)   // tcpi_rcv_space

		// Новые бесплатные поля (Спринт 1, задача А)
		m.State = buf[0]       // tcpi_state
		m.Retransmits = buf[2] // tcpi_retransmits
		m.Probes = buf[3]      // tcpi_probes

		// tcpi_snd_wscale (младшие 4 бита) и tcpi_rcv_wscale (старшие 4 бита)
		wscale := buf[6]
		m.SndWscale = wscale & 0x0F
		m.RcvWscale = (wscale >> 4) & 0x0F

		m.TotalRetrans = u32(100) // tcpi_total_retrans
	})
	return m
}

// ============================================================================
// ЭТАЛОННЫЙ ИСПОЛНИТЕЛЬНЫЙ ДВИЖОК: КАСТОМНЫЙ ВОРКЕР
// ============================================================================

// handleIncomingBotCustom — параметризованный воркер удержания ботов для пула
// Дегтярной Ямы. Вызывается демоном из `WorkerLoop` для каждого входящего соединения.
//
// ЭТАЛОННАЯ РЕАЛИЗАЦИЯ: Именно по этому образцу выровнен `handleIncomingBot`.
// Все временные метки и метрики заполнены корректно.
//
// ОБНОВЛЕНО В v8.0.1:
//   - Переименование `cfg.Jitter` → `cfg.DelayVariance` для консистентности
//     с обновлённой структурой `TarpitConfig`.
//   - Добавлено поле `PlannedDelay` в `RawConnectionEvent` для устранения
//     хардкода в `ConnectionLogWriter`.
func handleIncomingBotCustom(conn net.Conn, tarpitID string, port int, cfg sys.TarpitConfig) {
	// 1. ВЗВОД СЧЕТЧИКА (0 наносекунд CPU)
	IncrementLiveBots()
	defer func() {
		// ОЧИСТКА (0 наносекунд CPU)
		DecrementLiveBots()
		conn.Close()
	}()

	remoteAddr := conn.RemoteAddr().String()
	rawClientIP, portStr, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		rawClientIP = "0.0.0.0"
		portStr = "0"
	}
	sourcePort, _ := strconv.Atoi(portStr)

	// Очистка IP от IPv6-префикса
	clientIP := rawClientIP
	if strings.HasPrefix(clientIP, "::ffff:") {
		clientIP = strings.TrimPrefix(clientIP, "::ffff:")
	}

	// Эталонный объект времени для сквозного SIEM-хронометража
	// Это момент ПЕРВОГО КАСАНИЯ ботом сокета ловушки.
	// ОБНОВЛЕНО В v8.0.1.
	startTime := time.Now()
	// tokenHex := fmt.Sprintf("%X_%d", startTime.UnixNano(), sourcePort)

	// Вычисляем джиттер на основе персональных параметров воркера из инвентаря.
	// Математика: итоговый сон = DelayStep ± DelayVariance (равномерное распределение).
	//
	// ПЕРЕИМЕНОВАНО В v8.0.1: `cfg.Jitter` → `cfg.DelayVariance`
	// для консистентности с глобальным конфигом `config.C.DelayVariance`.
	delayStep := cfg.DelayStep
	variance := cfg.DelayVariance

	// v8.2.0 (2.1): Санация на точке применения.
	// Гарантируем, что DelayStep положительный и Variance корректен.
	if delayStep <= 0 {
		delayStep = 13 // Дефолтный шаг Дегтярной Ямы
	}

	if variance < 0 {
		variance = 0
	}

	// Гарантируем, что variance не больше, чем сам шаг, иначе minDelay станет 0 или отрицательным
	if variance >= delayStep {
		variance = delayStep - 1
	}
	if variance < 1 {
		variance = 1
	} else if variance > maxDelayStep-minDelayStep {
		variance = maxDelayStep - minDelayStep
	}
	tarpitDelay := delayStep
	if variance > 0 {
		minDelay := delayStep - variance
		maxDelay := delayStep + variance
		// Использование глобального генератора
		// Примечание: Global rand.Intn() является потокобезопасным через внутренний мьютекс Go.
		tarpitDelay = rand.Intn(maxDelay-minDelay+1) + minDelay

	}

	// Вычисляем имя для вывода в TUI в зависимости от порта ловушки
	tarpitLogName := fmt.Sprintf("TARPIT:%d", port)

	// Имитируем ответ кастомной SSH-версии воркера (баннер).
	// v8.4.4: баннер — первый эшелон.
	// Гео больше не задерживает ответ ловушки.
	sshBanner := fmt.Sprintf("%s\r\n", cfg.SSHVersion)
	if cfg.SSHVersion == "" {
		sshBanner = "SSH-2.0-OpenSSH_9.6p1 Ubuntu-3ubuntu13\r\n"
	}
	_, _ = conn.Write([]byte(sshBanner))

	// Гео-идентификация источника атаки — второй эшелон.
	// Выполняется ПОСЛЕ отправки баннера.
	countryName := "Unknown"
	if ipRegex.MatchString(clientIP) {
		countryName = getCountryFromGeoTool(clientIP, config.C.GeoTool)
	}

	// Асинхронная публикация события старта в глобальную шину логгера.
	LogConnectionEvent(RawConnectionEvent{
		Timestamp:    startTime,     // [SIEM YANKOR] Точный момент касания сокета (начало атаки)
		TarpitID:     tarpitLogName, // Уникальный идентификатор ловушки/воркера
		IP:           clientIP,      // Источник трафика
		Country:      countryName,   // Гео-информация источника
		PlannedDelay: tarpitDelay,   // [JITTER PLAN] Запланированный джиттер данного воркера (для сравнения с фактом)
	})

	// Пауза перед циклом удержания (поведение пока не меняем).
	// Позже решим: убрать или сделать настраиваемым джиттером.
	time.Sleep(50 * time.Millisecond)

	// v8.0.3 (Аудит №5): первичный слепок удалён — метрики снимаются ОДИН раз,
	// перед финальным отчётом (ниже по коду).

	// --- ЦИКЛ УДЕРЖАНИЯ БОТА ---
	// Каждый тик = 1 секунда. Бот держится до истечения `tarpitDelay`.
	// Если бот рвёт соединение досрочно (FIN/RST) — помечаем как `AGGRESSIVE`.
	// СПРИНТ 1 (B): фиксируем причину завершения сессии.
	// СПРИНТ 1 (C, D): running-статистика IAT и бакеты размеров чтений —
	// без массивов, O(1) памяти на сессию.
	currentTick := 0
	behaviorTag := "PASSIVE_SCANNER"
	closeReason := "TIMEOUT" // дефолт: сессию закрыли мы по таймеру
	buffer := make([]byte, 1024)
	// === Running-статистика IAT (интервалы между успешными чтениями) ===
	// time.Now() здесь — vDSO clock_gettime, НЕ syscall: в горячем пути допустимо.
	var lastRead time.Time
	var iatCount uint32
	var iatSumMs uint64
	var iatMinMs, iatMaxMs uint32
	var iatSumSqMs float64
	// === Бакеты размеров чтений (running-счётчики) ===
	var readTiny, readSmall, readMedium, readLarge uint32
	for currentTick < tarpitDelay {
		_ = conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		n, err := conn.Read(buffer)
		// === СПРИНТ 1 (C, D): учёт успешных чтений ===
		if n > 0 {
			// Бакет размера чтения.
			switch {
			case n <= 64:
				readTiny++
			case n <= 256:
				readSmall++
			case n <= 1024:
				readMedium++
			default:
				readLarge++
			}
			// IAT: интервал от предыдущего успешного чтения.
			nowRead := time.Now()
			if !lastRead.IsZero() {
				dMs := nowRead.Sub(lastRead).Milliseconds()
				if dMs < 0 {
					dMs = 0
				}
				uMs := uint64(dMs)
				if iatCount == 0 || uMs < uint64(iatMinMs) {
					iatMinMs = uint32(uMs)
				}
				if uMs > uint64(iatMaxMs) {
					iatMaxMs = uint32(uMs)
				}
				iatCount++
				iatSumMs += uMs
				iatSumSqMs += float64(uMs) * float64(uMs)
			}
			lastRead = nowRead
		}
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				// Бот пассивно ждёт — инкрементируем тик
				currentTick++
				continue
			}
			// Бот агрессивно разорвал соединение (FIN/RST/Ctrl+C)
			behaviorTag = "AGGRESSIVE_SCANNER"
			// СПРИНТ 1 (B): классификация причины завершения на стороне атакующего.
			switch {
			case errors.Is(err, io.EOF):
				closeReason = "FIN" // атакующий закрыл соединение корректно
			case errors.Is(err, syscall.ECONNRESET):
				closeReason = "RST" // атакующий сбросил соединение
			default:
				closeReason = "OUR_CLOSE" // сокет закрыт с нашей стороны или иная ошибка
			}
			break
		}
		currentTick++
	}

	// Удаляем сессию из RAM-реестра перед записью финального лога
	// ActiveSessions.Delete(sourcePort)

	// v8.0.3 (Аудит №5): реестр больше не ведётся — Delete удалён.
	// Расчёт РЕАЛЬНОГО времени удержания.
	// Используем разницу Unix-времени между текущим моментом и `startTime`.

	// ОБНОВЛЕНО В v8.0.1.
	realTrapped := time.Now().Unix() - startTime.Unix()

	// ЖЁСТКИЙ ИБ-ВЫРАВНИВАТЕЛЬ: Если бот пассивный — факт строго равен
	// счётчику тиков удержания. Это исключает расхождение из-за накладных
	// расходов планировщика горутин.
	// ОБНОВЛЕНО В v8.0.1.
	if behaviorTag == "PASSIVE_SCANNER" || realTrapped < int64(currentTick) {
		realTrapped = int64(currentTick)
	}
	if realTrapped < 0 {
		realTrapped = 0
	}

	// !!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!
	// !!! Извлекаем финальные метрики TCP_INFO ядра Linux (после удержания) !!!
	// !!! и сразу их пишем в LogReportEntry                                 !!!
	// !!! Это даёт криминалистический слепок сетевого состояния бота.       !!!
	// !!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!
	// ОБНОВЛЕНО В v8.0.3.
	// ОБНОВЛЕНО В v8.4.x (Спринт 1): парсинг в единую структуру.
	tcpMetrics := extractTcpInfo(conn)

	// Публикуем в шину логгера очищенные, синхронизированные данные.
	//
	// Формат строки в файле:
	// Дата \t TarpitID \t IP \t План \t Факт \t Поведение \t Страна \t Порт \t Токен \t MSS \t SndCwnd \t RcvSpace \t RTT \t TotalRetrans \t State \t Retransmits \t Probes \t SndWscale \t RcvWscale \t CloseReason \t IATCount \t IATSumMs \t IATMinMs \t IATMaxMs \t IATSumSqMs \t ReadTiny \t ReadSmall \t ReadMedium \t ReadLarge \t WriteLatencyMs
	//
	// ОБНОВЛЕНО В v8.1.1: Все поля заполнены корректно.
	// `DelayStep` = фактическое удержание, `PlannedDelay` = запланированный джиттер.
	// ОБНОВЛЕНО В v8.1.1.
	LogReportEntry(RawLogEntry{
		// Timestamp: Момент начала взаимодействия (первое касание сокета ботом).
		// Используется как абсолютный якорь для всех систем мониторинга (SIEM).
		Timestamp: startTime,
		// TarpitID: Уникальный идентификатор ловушки (например, "TARPIT:22").
		// Позволяет фильтровать логи по типу защиты.
		TarpitID: tarpitLogName,
		// IP: Истинный IP-адрес источника атаки, извлеченный из пакета.
		IP: clientIP,
		// Country: Идентифицированная страна источника атаки.
		Country: countryName,
		// DelayStep: Фактическое (реальное) время удержания в секундах.
		DelayStep: int(realTrapped),
		// PlannedDelay: Запланированное время джиттера.
		// Позволяет выявлять расхождения между планом и фактом.
		PlannedDelay: tarpitDelay,
		// Behavior: Оценка поведения атакующего: PASSIVE_SCANNER или AGGRESSIVE_SCANNER.
		Behavior: behaviorTag,
		// SourcePort: Исходный порт, который использовал атакующий.
		SourcePort: sourcePort,
		// MSS: Maximum Segment Size (Max Packet Size). Метрика TCP для анализа размера пакетов.
		MSS: tcpMetrics.MSS,
		// SndCwnd: Sender Congestion Window. Окно отправки TCP.
		SndCwnd: tcpMetrics.SndCwnd,
		// RcvSpace: Размер TCP-окна буфера приёма (Receive Buffer Space).
		// Показывает доступную память на стороне атакующего.
		// Извлекается из `TCP_INFO` ядра Linux.
		RcvSpace: tcpMetrics.RcvSpace,
		// RTT: Round Trip Time. Сетевая задержка.
		// Показывает физическое расстояние до атакующего.
		// Извлекается из `TCP_INFO` ядра Linux.
		RTT: tcpMetrics.RTT,
		// === НОВЫЕ ПОЛЯ СПРИНТ 1 (ЗАДАЧА А) ===
		// TotalRetrans: Общие ретрансмиты за сессию.
		TotalRetrans: tcpMetrics.TotalRetrans,
		// State: Статус сокета TCP (ESTABLISHED, CLOSE_WAIT и т.д.).
		State: tcpMetrics.State,
		// Retransmits: Текущие ретрансмиты (неподтверждённые).
		Retransmits: tcpMetrics.Retransmits,
		// Probes: Keep-alive пробы от атакующего.
		Probes: tcpMetrics.Probes,
		// SndWscale: Window scaling отправки.
		SndWscale: tcpMetrics.SndWscale,
		// RcvWscale: Window scaling приёма.
		RcvWscale: tcpMetrics.RcvWscale,
		// === НОВЫЕ ПОЛЯ СПРИНТ 1 (ЗАДАЧИ B, C, D) ===
		// CloseReason: Причина завершения сессии (FIN / RST / TIMEOUT / OUR_CLOSE).
		CloseReason: closeReason,
		// IATCount: Число успешных чтений (база для статистики IAT).
		IATCount: iatCount,
		// IATSumMs: Сумма интервалов между чтениями (мс).
		IATSumMs: iatSumMs,
		// IATMinMs: Минимальный интервал между чтениями (мс).
		IATMinMs: iatMinMs,
		// IATMaxMs: Максимальный интервал между чтениями (мс).
		IATMaxMs: iatMaxMs,
		// IATSumSqMs: Сумма квадратов интервалов (мс²) для дисперсии.
		IATSumSqMs: iatSumSqMs,
		// ReadTiny: Чтений размером 0–64 байта.
		ReadTiny: readTiny,
		// ReadSmall: Чтений размером 65–256 байт.
		ReadSmall: readSmall,
		// ReadMedium: Чтений размером 257–1024 байта.
		ReadMedium: readMedium,
		// ReadLarge: Чтений размером >1024 байта (резерв: буфер 1024).
		ReadLarge: readLarge,
	})
}
