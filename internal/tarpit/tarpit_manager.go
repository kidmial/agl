// internal/tarpit/tarpit_manager.go
// ============================================================================
// КОМПОНЕНТ: Центральный Диспетчер Демона Ловушек (TarpitManager)
// ВЕРСИЯ: v8.0.1
// РОЛЬ: Демон-исполнитель. Читает декларацию из `tarpit_inventory.json`
//
//	и приводит систему к нужному состоянию через Reconciliation Loop.
//
// ============================================================================
//
// АРХИТЕКТУРНАЯ ПОЗИЦИЯ:
//
//	Этот файл — ИСПОЛНИТЕЛЬНЫЙ контур демона.
//	Он запускается через `agl --tarpit-daemon` и живёт как systemd-сервис.
//
//	Клиенты (TUI, Web, API) → пишут в tarpit_inventory.json (декларация)
//	Демон (ЭТОТ ФАЙЛ)       → читает декларацию, управляет слушателями,
//	                           создаёт/удаляет маркеры, заполняет статусы
//
// КЛЮЧЕВОЙ ПРИНЦИП:
//
//	Демон — ЕДИНСТВЕННЫЙ кто создаёт и удаляет файлы-маркеры.
//	Клиенты могут только ЧИТАТЬ маркеры для отображения статуса.
//
// ### [2026-08-22] — Мажорный рефакторинг демона (v8.0.1)
// - Демон теперь управляет ВСЕМИ ловушками, включая базовую на порту 22.
// - Добавлено заполнение рантайм-метаданных: `activated_at`, `worker_pid`.
// - Добавлено создание/удаление маркеров с JSON-наполнением.
// - Добавлено обновление поля `status` в инвентаре (подтверждение демона).
// - Добавлен семафор `MaxConn` для индивидуального лимита каждого воркера.
// - Переименование `cfg.Jitter` → `cfg.DelayVariance`.
// - Добавлена проверка `is_core` для предохранителя рокировки порта 22.
// - Первый тик Reconciliation Loop выполняется немедленно (без задержки 15 сек).
// ============================================================================
package tarpit

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	// "strings"

	"agl/config"
	"agl/i18n"
	"agl/internal/sys"
)

// ============================================================================
// КОНСТАНТЫ
// ============================================================================

// CastlingMarkerPath — персистентный маркер рокировки портов.
// Используется как предохранитель для базовой ловушки порта 22.
// Если файл существует → рокировка выполнена → порт 22 свободен.
const CastlingMarkerPath = config.MarkerSshMoved

// ReconciliationInterval — период цикла сверки декларации с фактом.
// Каждые 15 секунд демон перечитывает инвентарь и устраняет расхождения.
const ReconciliationInterval = 15 * time.Second

// ============================================================================
// СТРУКТУРЫ ДАННЫХ
// ============================================================================

// WorkerState описывает рантайм-состояние запущенной горутины-ловушки.
// Каждый воркер — это один слушатель на одном порту.
type WorkerState struct {
	// ID — уникальный идентификатор ловушки.
	// Совпадает с ключом в `tarpit_inventory.json` и именем маркера.
	// Пример: "agl_tarpit_listener_port.22"
	ID string

	// Port — номер TCP-порта, на котором воркер слушает соединения.
	Port int

	// IsCore — флаг особой ловушки порта 22 (связана с рокировкой).
	// Если true, демон проверяет маркер рокировки перед запуском.
	IsCore bool

	// Status — рантайм-статус воркера в памяти демона.
	// Значения: "RUNNING" / "STOPPED"
	Status string

	// Context — контекст для мягкой отмены горутины воркера.
	// При вызове Cancel() горутина завершается.
	Context context.Context

	// Cancel — функция отмены контекста.
	// Вызывается при остановке воркера.
	Cancel context.CancelFunc

	// Listener — указатель на сетевой слушатель.
	// Нужен для внешнего закрытия сокета при остановке воркера.
	Listener net.Listener

	// Semaphore — канал-семафор для ограничения параллельных соединений.
	// Ёмкость канала = cfg.MaxConn.
	// Когда все слоты заняты, новые соединения ждут освобождения.
	// НОВОЕ ПОЛЕ v8.0.1.
	Semaphore chan struct{}

	// MarkerPath — полный путь к файлу-маркеру этой ловушки.
	// Пример: "config.TarpitMarkerPath(22)"
	// НОВОЕ ПОЛЕ v8.0.1.
	MarkerPath string
}

// MarkerContent описывает JSON-наполнение файла-маркера.
// Маркер создаётся/удаляется ИСКЛЮЧИТЕЛЬНО демоном.
// НОВОЕ ПОЛЕ v8.0.1.
type MarkerContent struct {
	// TarpitID — совпадает с именем файла маркера.
	// Для самодостаточности и валидации.
	TarpitID string `json:"tarpit_id"`

	// Port — дублирует порт из имени маркера.
	// Избыточность для валидации.
	Port int `json:"port"`

	// IsCore — флаг особой ловушки порта 22.
	IsCore bool `json:"is_core"`

	// ActivatedAt — момент, когда демон реально поднял слушателя.
	// Формат: "2006-01-02 15:04:05"
	ActivatedAt string `json:"activated_at"`

	// WorkerPID — PID процесса демона, который держит сокет.
	// Используется для детекции мёртвых маркеров в Self-Healing.
	WorkerPID int `json:"worker_pid"`
}

// ============================================================================
// ГЛОБАЛЬНЫЕ ПЕРЕМЕННЫЕ
// ============================================================================

// TarpitPool — глобальный реестр активных воркеров в памяти демона.
// Ключ: TarpitID (совпадает с именем маркера).
// Значение: указатель на WorkerState.
var (
	TarpitPool = make(map[string]*WorkerState)
	poolMu     sync.RWMutex
)

// bindFails — счётчик подряд неудачных bind по каждой ловушке.
// v8.2.0 (2.2+): ограниченный ретрай перед терминальным отказом декларации.
var bindFails = make(map[string]int)

// deferredReject — отложенный отказ предохранителя (v8.2.3, R-5).
// reconcileOnce собирает такие записи ПОД poolMu (проверки дешёвые, os.Stat),
// но сам fuseReject вызывает ПОСЛЕ освобождения лока:
// внутри fuseReject файловые операции с flock, которые не должны
// блокировать весь пул воркеров при медленном диске/NFS.
type deferredReject struct {
	// id — идентификатор ловушки (ключ инвентаря и имя маркера).
	id string
	// event — машинный код ALERT-события для журнала.
	event string
	// reason — человеко-читаемая причина отказа.
	reason string
}

// fuseReject — единая семантика терминального отказа предохранителей.
// v8.2.0 (2.2-ext): 3 подряд отказа поглощают транзиентные гонки
// (рокировка в процессе, смена конфига), затем декларация
// переворачивается в state=STOPPED — демон перестаёт стучаться
// в закрытую дверь каждые 15 секунд.
//
// v8.2.3 (R-5): fuseReject БОЛЬШЕ не рассчитывает на то, что вызывающий
// держит poolMu. reconcileOnce вызывает её после освобождения лока.
// Поэтому:
//   - bindFails защищаем короткой критической секцией внутри функции
//     (LaunchWorker ходит в bindFails тоже под poolMu — контракт единый);
//   - файловые операции (updateInventoryStatus, WithInventoryTx с flock)
//     выполняются ВНЕ poolMu — медленный диск не морозит пул воркеров;
//   - повторный Lock здесь больше не deadlock: вызывающий лок не держит.
func fuseReject(id string, event string, reason string) {
	// Отказ этого тика: статус честный DISABLED, лог для аудита.
	sys.LogAction("TARPIT_MANAGER", event, "ALERT",
		i18n.TrEventf("tarpit_manager.fuse_reject", id, reason))
	// Файловая фиксация статуса — вне poolMu, это допустимо и желательно.
	updateInventoryStatus(id, sys.StatusDisabled)

	// Счётчик подряд отказов.
	// Короткая критическая секция: только инкремент, чтение порога и delete.
	poolMu.Lock()
	bindFails[id]++
	reached := bindFails[id] >= 3
	if reached {
		// Чистим счётчик сразу: терминальный отказ уже состоялся.
		delete(bindFails, id)
	}
	// Отпускаем лок ДО flock, чтобы транзакция инвентаря не шла под poolMu.
	poolMu.Unlock()

	if reached {
		// Терминальный отказ: декларация больше не хочет невозможного.
		// flock (WithInventoryTx) выполняется строго ВНЕ poolMu.
		_ = sys.WithInventoryTx(func(inv *sys.TarpitInventory) error {
			trap, exists := inv.Traps[id]
			if !exists {
				return fmt.Errorf(i18n.T.TarpitManagerTrapRemovedFromInventory, id)
			}
			trap.State = sys.StateStopped
			trap.Status = sys.StatusDisabled
			inv.Traps[id] = trap
			return nil
		})
		sys.LogAction("TARPIT_MANAGER", "DECLARATION_REJECTED", "ALERT",
			i18n.TrEventf("tarpit_manager.declaration_rejected_fuse", id, reason))
	}
}

// ============================================================================
// ТОЧКА ВХОДА ДЛЯ ДЕМОНА
// ============================================================================

// StartTarpitManager — точка входа для `main.go` в режиме `--tarpit-daemon`.
// ОБНОВЛЕНО В v8.0.1: Добавлен вызов `InitShmMaster()`.
// Инициализирует контекст и запускает Reconciliation Loop.
// Это БЛОКИРУЮЩИЙ вызов — он не возвращает управление до остановки демона.
//
// ВАЖНО: Эта функция вызывается ТОЛЬКО в режиме демона.
// В штатном режиме TUI она НЕ вызывается.
func StartTarpitManager() {
	// Инициализация межпроцессного счётчика ботов в `/dev/shm/`.
	// Вызывается ПЕРЕД запуском воркеров.
	// ОБНОВЛЕНО В v8.0.1.
	InitShmMaster()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// ИБ-АУДИТ: Фиксируем старт демона
	sys.LogAction("TARPIT_MANAGER", "DAEMON_START", "SUCCESS",
		i18n.TrEvent("tarpit_manager.daemon_start"))

	// Запускаем Reconciliation Loop (блокирующий вызов для демона)
	StartManager(ctx)
}

// ============================================================================
// RECONCILIATION LOOP — ГЛАВНЫЙ ЦИКЛ СВЕРКИ
// ============================================================================

// StartManager — главный поток демона, который циклично проверяет инвентарь
// ловушек и приводит систему к нужному состоянию.
//
// АЛГОРИТМ КАЖДОГО ТИКА:
//  1. Загрузить декларацию из `tarpit_inventory.json`
//  2. Для каждой записи со `state = RUNNING`:
//     - Если воркера нет в пуле → запустить
//  3. Для каждой записи со `state = STOPPED`:
//     - Если воркер есть в пуле → остановить
//  4. Проверить «призраков» — воркеры, исчезнувшие из инвентаря
//
// ВАЖНО: Первый тик выполняется НЕМЕДЛЕННО, без задержки.
// Это обеспечивает быстрый старт ловушек при запуске демона.
// НОВОЕ В v8.0.1.
func StartManager(ctx context.Context) {
	ticker := time.NewTicker(ReconciliationInterval)
	defer ticker.Stop()

	// ПЕРВЫЙ ТИК НЕМЕДЛЕННО: Не ждём 15 секунд до первой сверки.
	// Это обеспечивает быстрый старт ловушек при запуске демона.
	reconcileOnce()

	// Последующие тики каждые 15 секунд
	for {
		select {
		case <-ctx.Done():
			sys.LogAction("TARPIT_MANAGER", "DAEMON_SHUTDOWN", "SUCCESS",
				i18n.TrEvent("tarpit_manager.daemon_shutdown"))
			// Останавливаем все активные воркеры при завершении демона
			stopAllWorkers()
			return
		case <-ticker.C:
			reconcileOnce()
		}
	}
}

// reconcileOnce выполняет ОДИН цикл сверки декларации с фактом.
// Вызывается из `StartManager` на каждом тике.
// НОВОЕ В v8.0.1: Вынесено в отдельную функцию для немедленного первого тика.
func reconcileOnce() {
	// -------------------------------------------------------------
	// ШАГ 1: ЗАГРУЗКА ДЕКЛАРАЦИИ
	// Читаем текущее состояние инвентаря с диска.
	// -------------------------------------------------------------
	inventory, err := sys.LoadTarpitInventory(sys.TarpitInventoryPath)
	if err != nil {
		// Если файла нет — это нормально для первого старта.
		// Демон просто ждёт, пока клиент создаст первую ловушку.
		return
	}

	// v8.2.3 (R-5): буфер отложенных отказов предохранителей.
	// Проверки предохранителей (os.Stat, сравнение портов) дешёвые —
	// их оставляем под локом. А вот fuseReject ходит в flock,
	// поэтому вызывается только после poolMu.Unlock().
	var deferredRejects []deferredReject

	poolMu.Lock()
	// ВНИМАНИЕ (R-5): defer poolMu.Unlock() УБРАН.
	// Разблокировка явная, в конце шага 3. Если оставить defer И добавить
	// явный Unlock — получим double unlock и runtime-панику.
	// Ранних return между Lock и Unlock в этой функции нет,
	// поэтому явная разблокировка безопасна.

	// C8 (v8.1.3): хот-релоад конфига для предохранителя спасательного порта.
	//
	// v8.2.3 (R-4b): слепое игнорирование ошибки (_ =) заменено на явное логирование.
	// Поведение при ошибке (fail-safe) не меняется: демон продолжает работу
	// с предыдущим успешным снапшотом, чтобы не обрушить ловушки из-за опечатки.
	if err := config.LoadConfig(config.DirEtc + "/config.json"); err != nil {
		sys.LogAction("TARPIT_MANAGER", "CONFIG_RELOAD_FAIL", "WARNING",
			i18n.TrEventf("tarpit_manager.config_reload_fail", err))
	}

	// -------------------------------------------------------------
	// ШАГ 2: СВЕРКА ДЕКЛАРАЦИИ С ФАКТОМ (ЗАПУСК)
	// Для каждой записи со `state = RUNNING`:
	//   - Если воркера нет в пуле → запускаем
	//   - Если воркер уже есть → пропускаем
	// -------------------------------------------------------------
	for id, required := range inventory.Traps {
		currentState, exists := TarpitPool[id]
		// ЗАПУСК: В декларации `RUNNING`, а в пуле воркера нет
		if required.State == sys.StateRunning && (!exists || currentState.Status != "RUNNING") {
			// ПРЕДОХРАНИТЕЛЬ РОКИРОВКИ: Для базовой ловушки порта 22
			// проверяем, что рокировка выполнена.
			if required.IsCore {
				if _, errStat := os.Stat(CastlingMarkerPath); os.IsNotExist(errStat) {
					// v8.2.3 (R-5): fuseReject под локом НЕ вызываем —
					// кладём отказ в буфер, обработаем после Unlock.
					deferredRejects = append(deferredRejects, deferredReject{
						id:     id,
						event:  "CORE_BLOCKED",
						reason: i18n.TrEvent("tarpit_manager.reject_reason_core_blocked"),
					})
					continue
				}
			}
			// C8 (v8.1.3): защита глубины — ловушка на канале спасения запрещена,
			// даже если декларация прописана вручную в обход мастера.
			if required.Port == config.C.WebRescuePort {
				deferredRejects = append(deferredRejects, deferredReject{
					id:     id,
					event:  "RESCUE_PORT_BLOCKED",
					reason: i18n.TrEventf("tarpit_manager.reject_reason_rescue_port", required.Port),
				})
				continue
			}
			// C8: активный скрытый порт SSH тоже не может быть капканом.
			if _, errCast := os.Stat(CastlingMarkerPath); errCast == nil && required.Port == config.C.RealSshPort {
				deferredRejects = append(deferredRejects, deferredReject{
					id:     id,
					event:  "SSH_PORT_BLOCKED",
					reason: i18n.TrEventf("tarpit_manager.reject_reason_ssh_port", required.Port),
				})
				continue
			}
			// Запускаем воркер в отдельной горутине
			// ВАЖНО: `go LaunchWorker(...)` не блокирует текущий цикл
			go LaunchWorker(id, required)
		}
		// ОСТАНОВКА: В декларации `STOPPED`, а воркер до сих пор жив
		// StopWorker по контракту вызывается ТОЛЬКО под poolMu — оставляем внутри.
		if required.State == sys.StateStopped && exists && currentState.Status == "RUNNING" {
			StopWorker(id)
		}
	}

	// -------------------------------------------------------------
	// ШАГ 3: ЗАЧИСТКА «ПРИЗРАКОВ» (ORPHANS)
	// Воркеры, которые есть в пуле, но исчезли из инвентаря.
	// Это может произойти, если админ вручную удалил запись из JSON.
	// -------------------------------------------------------------
	for id := range TarpitPool {
		if _, required := inventory.Traps[id]; !required {
			sys.LogAction("TARPIT_MANAGER", "ORPHAN_CLEANUP", "WARNING",
				i18n.TrEventf("tarpit_manager.orphan_cleanup", id))
			StopWorker(id)
		}
	}

	// Явная разблокировка ПЕРЕД обработкой отказов (R-5).
	poolMu.Unlock()

	// -------------------------------------------------------------
	// ШАГ 4 (R-5): ОБРАБОТКА ОТЛОЖЕННЫХ ОТКАЗОВ ВНЕ poolMu.
	// fuseReject пишет в инвентарь под flock; медленный диск или NFS
	// больше не блокируют цикл сверки и весь пул воркеров.
	// -------------------------------------------------------------
	for _, rej := range deferredRejects {
		fuseReject(rej.id, rej.event, rej.reason)
	}
}

// ============================================================================
// УПРАВЛЕНИЕ ВОРКЕРАМИ
// ============================================================================

// LaunchWorker инициализирует стейт воркера и запускает изолированный
// поток прослушивания на выделенном порту.
//
// АЛГОРИТМ:
//   1. Создать контекст и семафор для воркера
//   2. Зарегистрировать воркер в пуле
//   3. Заполнить рантайм-метаданные в инвентаре (activated_at, worker_pid)
//   4. Обновить статус в инвентаре: status = ACTIVE
//   5. Создать файл-маркер с JSON-наполнением
//   6. Запустить сетевой контур (блокирующий вызов)
//
// ВАЖНО: Эта функция вызывается в отдельной горутине через `go LaunchWorker(...)`.
// Блокирующий вызов `WorkerLoop` не блокирует Reconciliation Loop.
//
// НОВОЕ В v8.0.1: Заполнение рантайм-метаданных и создание маркера.
// internal/tarpit/tarpit_manager.go

// ... (other imports and structs) ...

// LaunchWorker запускает горутину, которая будет слушать порты ловушки,
// и регистрирует воркер в пуле.
func LaunchWorker(id string, required sys.TarpitDefinition) {
	// НОВОЕ В v8.0.1 Hotfix: Получаем и санитизируем конфигурацию
	// Это защищает от некорректных полей (0, пустые строки) перед запуском.
	cfg := required.ConfigConfig
	cfg = sanitizeConfig(cfg)

	// Создаём контекст для мягкой отмены горутины
	ctx, cancel := context.WithCancel(context.Background())

	// Создаём семафор для ограничения параллельных соединений.
	// Ёмкость канала = MaxConn из конфигурации воркера.
	// НОВОЕ В v8.0.1.
	maxConn := cfg.MaxConn
	if maxConn <= 0 {
		maxConn = 100 // Безопасный дефолт
	}
	semaphore := make(chan struct{}, maxConn)

	// Формируем путь к маркеру
	markerPath := required.MarkerFilePath
	if markerPath == "" {
		markerPath = sys.GenerateMarkerPath(required.Port)
	}

	// v8.2.0 (2.2): сокет bind ДО регистрации в пуле и записи маркера.
	// Демон подтверждает ФАКТ, а не намерение: маркер и status=ACTIVE
	// появляются только после реального занятия порта. Зомби-состояние
	// «маркер есть, сокета нет» становится невозможным.
	addr := fmt.Sprintf("0.0.0.0:%d", required.Port)
	listener, errListen := net.Listen("tcp", addr)
	if errListen != nil {
		sys.LogAction("TARPIT_MANAGER", "SOCKET_BIND_FAIL", "CRITICAL",
			i18n.TrEventf("tarpit_manager.socket_bind_fail", required.Port, id, errListen))
		updateInventoryStatus(id, sys.StatusDisabled)
		// v8.2.0 (2.2+): ограниченный ретрай, затем терминальный отказ.
		// 3 попытки поглощают транзиентные гонки (рокировка, рестарт демона);
		// перманентный конфликт получает явный отказ вместо вечного шума.
		poolMu.Lock()
		bindFails[id]++
		fails := bindFails[id]
		poolMu.Unlock()
		if fails >= 3 {
			_ = sys.WithInventoryTx(func(inv *sys.TarpitInventory) error {
				trap, exists := inv.Traps[id]
				if !exists {
					return fmt.Errorf(i18n.T.TarpitManagerTrapRemovedFromInventory, id)
				}
				trap.State = sys.StateStopped // декларация отклонена демоном
				trap.Status = sys.StatusDisabled
				inv.Traps[id] = trap
				return nil
			})
			poolMu.Lock()
			delete(bindFails, id)
			poolMu.Unlock()
			sys.LogAction("TARPIT_MANAGER", "DECLARATION_REJECTED", "ALERT",
				i18n.TrEventf("tarpit_manager.declaration_rejected_bind", id, required.Port, fails))
		}
		cancel()
		return
	}
	// bind успешен — сбрасываем счётчик неудач
	poolMu.Lock()
	delete(bindFails, id)
	poolMu.Unlock()

	// Собираем стейт воркера
	worker := &WorkerState{
		ID:         id,
		Port:       required.Port,
		IsCore:     required.IsCore,
		Status:     "RUNNING",
		Context:    ctx,
		Cancel:     cancel,
		Semaphore:  semaphore,
		MarkerPath: markerPath,
		Listener:   listener, // v8.2.0 (2.2): сокет занят до регистрации
	}
	// Регистрируем воркер в пуле
	poolMu.Lock()
	TarpitPool[id] = worker
	poolMu.Unlock()

	// -------------------------------------------------------------
	// ОБНОВЛЕНИЕ ИНВЕНТАРЯ И СОЗДАНИЕ МАРКЕРА (ФИКС v8.0.2)
	// Демон фиксирует момент, когда он реально поднял слушателя,
	// и PID процесса для детекции мёртвых маркеров.
	// -------------------------------------------------------------
	activatedAt := time.Now().Format("2006-01-02 15:04:05")
	workerPID := os.Getpid()

	// 1. Обновляем JSON: status=ACTIVE, activated_at, worker_pid
	updateInventoryRuntime(id, sys.StatusActive, activatedAt, workerPID)

	// 2. Создаём маркер на диске (если его нет или он устарел)
	markerContent := MarkerContent{
		TarpitID:    id,
		Port:        required.Port,
		IsCore:      required.IsCore,
		ActivatedAt: activatedAt,
		WorkerPID:   workerPID,
	}
	if err := writeMarkerFile(markerPath, markerContent); err != nil {
		sys.LogAction("TARPIT_MANAGER", "MARKER_WRITE_FAIL", "ERROR",
			i18n.TrEventf("tarpit_manager.marker_write_fail", id, err))
	}

	// ИБ-АУДИТ: Фиксируем успешный запуск воркера
	sys.LogAction("TARPIT_MANAGER", "WORKER_START", "SUCCESS",
		i18n.TrEventf("tarpit_manager.worker_start", id, required.Port, workerPID, maxConn))

	// Запуск сетевого контура ловушки (БЛОКИРУЮЩИЙ вызов)
	// Передаём САНІТИЗИРОВАННУЮ конфигурацию воркера
	WorkerLoop(worker, cfg)
}

// sanitizeConfig гарантирует, что все параметры ловушки имеют безопасные значения.
func sanitizeConfig(cfg sys.TarpitConfig) sys.TarpitConfig {
	if cfg.MaxConn <= 0 {
		cfg.MaxConn = 100
	}
	if cfg.DelayStep <= 0 {
		cfg.DelayStep = 13
	}
	if cfg.DelayVariance < 0 { // Jitter переименован в DelayVariance
		cfg.DelayVariance = 3
	}
	// v8.2.0 (2.1): вариативность не может съесть базовый сон —
	// иначе minDelay уходит в отрицательную зону.
	if cfg.DelayVariance >= cfg.DelayStep {
		cfg.DelayVariance = cfg.DelayStep - 1
	}
	if cfg.SSHVersion == "" {
		cfg.SSHVersion = "SSH-2.0-OpenSSH_9.6p1 Ubuntu-3ubuntu13"
	}
	return cfg
}

// StopWorker безопасно закрывает сетевой сокет ловушки и завершает горутину.
//
// АЛГОРИТМ:
//  1. Обновить статус воркера в памяти
//  2. Отправить сигнал отмены в контекст
//  3. Закрыть сетевой слушатель
//  4. Обновить статус в инвентаре: status = DISABLED
//  5. Удалить файл-маркер
//  6. Удалить воркер из пула
//
// КОНТРАКТ ПОТОКОБЕЗОПАСНОСТИ (п. 4.5 аудита):
//   - Вызов ДОПУСТИМ ТОЛЬКО при удерживаемом poolMu.Lock().
//   - Функция НЕ реентерабельна: повторный захват poolMu внутри = deadlock.
//   - Все текущие вызовы (reconcileOnce, stopAllWorkers) удовлетворяют контракту.
//   - При добавлении новых вызовов извне ОБЯЗАТЕЛЬНО захватить poolMu.Lock() ДО вызова.
//
// НОВОЕ В v8.0.1: Обновление статуса в инвентаре и удаление маркера.
// ОБНОВЛЕНО В v8.2.2 (п. 4.5): Формализован контракт потокобезопасности.
func StopWorker(id string) {
	worker, exists := TarpitPool[id]
	if !exists {
		return
	}

	// Обновляем статус в памяти
	worker.Status = "STOPPED"

	// Отправляем сигнал отмены в контекст горутины
	worker.Cancel()

	// Закрываем сетевой слушатель, чтобы разблокировать `Accept()`
	if worker.Listener != nil {
		_ = worker.Listener.Close()
	}

	// -------------------------------------------------------------
	// ОБНОВЛЕНИЕ СТАТУСА В ИНВЕНТАРЕ
	// Демон подтверждает, что слушатель снят.
	// НОВОЕ В v8.0.1.
	// -------------------------------------------------------------
	updateInventoryStatus(id, sys.StatusDisabled)

	// -------------------------------------------------------------
	// УДАЛЕНИЕ ФАЙЛА-МАРКЕРА
	// Демон — ЕДИНСТВЕННЫЙ кто удаляет маркеры.
	// НОВОЕ В v8.0.1.
	// -------------------------------------------------------------
	if worker.MarkerPath != "" {
		removeMarkerFile(worker.MarkerPath)
	}

	// Удаляем воркер из пула
	delete(TarpitPool, id)

	// ИБ-АУДИТ: Фиксируем остановку воркера
	sys.LogAction("TARPIT_MANAGER", "WORKER_STOP", "SUCCESS",
		i18n.TrEventf("tarpit_manager.worker_stop", id, worker.Port))
}

// stopAllWorkers останавливает все активные воркеры.
// Вызывается при завершении демона (получен сигнал отмены контекста).
// ОБНОВЛЕНО В v8.0.1: Добавлен вызов `RemoveShm()` для зачистки `/dev/shm/`.
func stopAllWorkers() {
	poolMu.Lock()
	defer poolMu.Unlock()

	for id := range TarpitPool {
		StopWorker(id)
	}

	// Зачистка межпроцессного счётчика ботов.
	// ОБНОВЛЕНО В v8.0.1.
	RemoveShm()

	sys.LogAction("TARPIT_MANAGER", "ALL_WORKERS_STOPPED", "SUCCESS",
		i18n.TrEvent("tarpit_manager.all_workers_stopped"))
}

// ============================================================================
// СЕТЕВОЙ КОНТУР ВОРКЕРА
// ============================================================================

// WorkerLoop разворачивает нативный TCP-слушатель на выделенном порту ловушки.
// Это БЛОКИРУЮЩИЙ вызов — он не возвращает управление до остановки воркера.
//
// АЛГОРИТМ:
//  1. Открыть сокет на `0.0.0.0:ПОРТ`
//  2. Вечный цикл `Accept()` для приёма соединений
//  3. Для каждого соединения: проверить семафор → запустить горутину-обработчик
//  4. При остановке воркера: выйти из цикла
//
// НОВОЕ В v8.0.1: Интеграция семафора `MaxConn` для ограничения
// параллельных соединений.
func WorkerLoop(state *WorkerState, cfg sys.TarpitConfig) {
	// v8.2.0 (2.2): сокет уже занят в LaunchWorker ДО регистрации в пуле.
	// Здесь только принимаем слушателя в вечный цикл Accept.
	listener := state.Listener
	defer listener.Close()

	// Вечный нативный цикл Accept
	for {
		conn, err := listener.Accept()
		if err != nil {
			// Проверяем, не вызвано ли закрытие сокета остановкой ловушки
			select {
			case <-state.Context.Done():
				return // Безопасный и мягкий выход из горутины воркера
			default:
				continue // Временная ошибка — продолжаем цикл
			}
		}

		// -------------------------------------------------------------
		// СЕМАФОР МАКСИМАЛЬНЫХ СОЕДИНЕНИЙ
		// Проверяем, есть ли свободный слот в семафоре.
		// Если все слоты заняты — блокируем до освобождения.
		// Это защищает сервер от перегрузки при DDoS.
		// НОВОЕ В v8.0.1.
		// -------------------------------------------------------------
		select {
		case state.Semaphore <- struct{}{}:
			// Слот получен — запускаем обработчик
			go func() {
				// Освобождаем слот по завершении обработки
				defer func() { <-state.Semaphore }()
				// Передаём бота в асинхронный кастомный воркер
				// ВАЖНО: `cfg.DelayVariance` вместо `cfg.Jitter` (переименование v8.0.1)
				handleIncomingBotCustom(conn, state.ID, state.Port, cfg)
			}()
		case <-state.Context.Done():
			// Воркер останавливается — закрываем соединение и выходим
			_ = conn.Close()
			return
		}
	}
}

// ============================================================================
// ВСПОМОГАТЕЛЬНЫЕ ФУНКЦИИ: ИНВЕНТАРЬ
// ============================================================================

// updateInventoryRuntime обновляет рантайм-метаданные ловушки в инвентаре.
// Вызывается ДЕМОНОМ при активации воркера.
// Заполняет: `status`, `activated_at`, `worker_pid`.
// v8.0.3 (Аудит №1): запись под транзакцией flock.
// ВАЖНО: если ловушка удалена клиентом, пока демон поднимал слушатель,
// запись НЕ воскрешается — ошибка транзакции гасится в аудит.
func updateInventoryRuntime(tarpitID string, status string, activatedAt string, workerPID int) {
	err := sys.WithInventoryTx(func(inv *sys.TarpitInventory) error {
		trap, exists := inv.Traps[tarpitID]
		if !exists {
			return fmt.Errorf(i18n.T.TarpitManagerTrapMissingFromInventory, tarpitID)
		}
		trap.Status = status
		trap.ActivatedAt = activatedAt
		trap.WorkerPID = workerPID
		inv.Traps[tarpitID] = trap
		return nil
	})
	if err != nil {
		sys.LogAction("TARPIT_MANAGER", "INVENTORY_UPDATE_FAIL", "ERROR",
			i18n.TrEventf("tarpit_manager.inventory_update_fail", err))
	}
}

// updateInventoryStatus обновляет только поле `status` ловушки в инвентаре.
// Вызывается ДЕМОНОМ при активации/деактивации воркера.
// v8.0.3 (Аудит №1): запись под транзакцией flock; удалённая ловушка не воскрешается.
func updateInventoryStatus(tarpitID string, status string) {
	err := sys.WithInventoryTx(func(inv *sys.TarpitInventory) error {
		trap, exists := inv.Traps[tarpitID]
		if !exists {
			return fmt.Errorf(i18n.T.TarpitManagerTrapMissingFromInventory, tarpitID)
		}
		trap.Status = status
		inv.Traps[tarpitID] = trap
		return nil
	})

	// БЛОК 3.6: Обработка ошибок записи в инвентаре.
	// Ошибка больше не игнорируется. Сбой фиксации состояния на диск
	// логируется через событийную модель для последующего анализа.
	if err != nil {
		sys.LogAction("TARPIT_MANAGER", "INVENTORY_UPDATE", "WARNING",
			i18n.TrEventf("inventory.update_failed", tarpitID, err))
	}
}

// ============================================================================
// ВСПОМОГАТЕЛЬНЫЕ ФУНКЦИИ: МАРКЕРЫ
// ============================================================================

// writeMarkerFile создаёт файл-маркер с JSON-наполнением.
// Демон — ЕДИНСТВЕННЫЙ кто создаёт маркеры.
// НОВОЕ В v8.0.1.
func writeMarkerFile(markerPath string, content MarkerContent) error {
	// Гарантируем существование директории
	_ = os.MkdirAll(filepath.Dir(markerPath), 0755)

	// Маршалим содержимое маркера в JSON
	data, err := json.MarshalIndent(content, "", "  ")
	if err != nil {
		return fmt.Errorf(i18n.T.TarpitManagerMarkerSerializeFail, err)
	}

	// Записываем на диск
	if err := sys.WriteAtomic(markerPath, data, 0644); err != nil {
		return fmt.Errorf(i18n.T.TarpitManagerMarkerWriteDiskFail, err)
	}

	return nil
}

// removeMarkerFile удаляет файл-маркер.
// Демон — ЕДИНСТВЕННЫЙ кто удаляет маркеры.
// НОВОЕ В v8.0.1.
func removeMarkerFile(markerPath string) {
	_ = os.Remove(markerPath)
}
