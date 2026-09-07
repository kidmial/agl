// internal/tarpitctl/tarpit_controller.go
// ============================================================================
// КОМПОНЕНТ: Единый Контроллер Ловушек Дегтярной Ямы (TrapController)
// ВЕРСИЯ: v8.0.1
// РОЛЬ: Единственная точка управления ловушками для всех клиентов.
//
//	Реализует декларативную модель: клиент пишет в инвентарь,
//	демон читает инвентарь и исполняет.
//
// ============================================================================
//
// АРХИТЕКТУРНАЯ ПОЗИЦИЯ:
//
//	Клиенты (TUI, Web, API, CLI)
//	    │
//	    ▼
//	tarpitctl (ЭТОТ ФАЙЛ) ← единственный слой управления
//	    │
//	    ├── пишет в /etc/amberguard/tarpit_inventory.json (декларация)
//	    ├── проверяет предохранители (периметр, рокировка)
//	    ├── управляет демоном agld_tarpit.service (systemd)
//	    └── читает маркеры из /var/lib/amberguard/states/ (только чтение!)
//	    │
//	    ▼
//	internal/tarpit (демон-исполнитель)
//	    ├── читает инвентарь
//	    ├── поднимает/снимает слушателей
//	    └── создаёт/удаляет маркеры (ЕДИНСТВЕННЫЙ кто пишет маркеры)
//
// КЛЮЧЕВОЙ ПРИНЦИП:
//
//	Этот контроллер НЕ создаёт и НЕ удаляет файлы-маркеры.
//	Маркеры — это зона ответственности демона.
//	Контроллер только ЧИТАЕТ маркеры для отображения статуса.
//
// ### [2026-08-22] — Создание контроллера (v8.0.1)
//   - Создан новый пакет `internal/tarpitctl` как единая точка управления.
//   - Реализованы функции: ActivateTrap, DeactivateTrap, ActivateCoreTarpit,
//     DeactivateCoreTarpit, GetAllTrapsStatus, EnsureDaemonRunning,
//     StopDaemonIfIdle, IsDaemonAlive.
//   - Интеграция предохранителей: инвентаризация периметра + маркер рокировки.
//   - Генерация и установка systemd-юнита `agld_tarpit.service`.
//
// ============================================================================
package tarpitctl

import (
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"agl/config"
	"agl/internal/sys"
)

// ============================================================================
// КОНСТАНТЫ: Пути и идентификаторы демона
// ============================================================================

const (
	// ServiceName — имя systemd-юнита демона ловушек.
	// Этот юнит управляет ВСЕМИ ловушками (порт 22 + мульти-ловушки).
	ServiceName = "agld_tarpit.service"

	// ServicePath — полный путь к файлу юнита в системе.
	ServicePath = "/etc/systemd/system/agld_tarpit.service"

	// BinaryPath — путь к исполняемому бинарнику комплекса.
	// Используется при генерации юнита в секции [Service] ExecStart.
	BinaryPath = config.FileBinary

	// CastlingMarkerPath — персистентный маркер рокировки портов.
	// Если файл существует → рокировка выполнена → порт 22 свободен.
	// Используется как предохранитель для активации базовой ловушки.
	CastlingMarkerPath = config.MarkerSshMoved
)

// ============================================================================
// СТРУКТУРА: Статус ловушки (возвращается клиенту)
// ============================================================================

// TrapStatus описывает полный статус одной ловушки для отображения в клиенте.
// Собирается из трёх источников:
//  1. Декларация (что админ ХОЧЕТ) — из `tarpit_inventory.json`
//  2. Подтверждение демона (что реально ПРОИЗОШЛО) — из `status` в инвентаре
//  3. Факт на диске (маркер существует или нет) — из `/var/lib/amberguard/states/`
type TrapStatus struct {
	// TarpitID — уникальный идентификатор ловушки.
	// Совпадает с именем файла-маркера.
	TarpitID string

	// Port — номер TCP-порта.
	Port int

	// IsCore — флаг особой ловушки порта 22 (связана с рокировкой).
	IsCore bool

	// DeclaredState — декларация админа: что он ХОЧЕТ.
	// Значения: "RUNNING" / "STOPPED".
	// Источник: поле `state` в `tarpit_inventory.json`.
	DeclaredState string

	// DaemonStatus — подтверждение демона: что реально произошло.
	// Значения: "ACTIVE" / "DISABLED".
	// Источник: поле `status` в `tarpit_inventory.json`.
	DaemonStatus string

	// MarkerExists — существует ли файл-маркер на диске.
	// Это фактическое состояние: слушатель реально поднят или нет.
	// Источник: проверка `os.Stat()` на `marker_file_path`.
	MarkerExists bool

	// WorkerPID — PID процесса-слушателя (из рантайм-метаданных демона).
	// 0 если демон ещё не заполнил.
	WorkerPID int

	// ActivatedAt — момент, когда демон реально поднял слушателя.
	// Пустая строка если демон ещё не активировал.
	ActivatedAt string

	// Config — слепок параметров воркера для отображения.
	Config sys.TarpitConfig
}

// ============================================================================
// ФУНКЦИЯ: Активация ловушки (универсальная)
// ============================================================================

// ActivateTrap активирует ловушку на указанном порту.
// Это ЕДИНСТВЕННАЯ функция активации для всех клиентов (TUI, Web, API).
//
// АЛГОРИТМ:
//  1. Проверяет предохранители (порт занят? рокировка для порта 22?)
//  2. Записывает декларацию в `tarpit_inventory.json`: state = RUNNING
//  3. Убеждается, что демон запущен
//
// ВАЖНО: Эта функция НЕ создаёт маркеры. Маркеры создаёт демон.
//
// Параметры:
//   - port: номер TCP-порта для ловушки
//   - trapConfig: индивидуальные параметры воркера
//
// Возвращает ошибку, если активация заблокирована предохранителем.
func ActivateTrap(port int, trapConfig sys.TarpitConfig) error {
	// ИБ-АУДИТ: Фиксируем попытку активации
	sys.LogAction("TRAPCTL", "ACTIVATE_REQUEST", "PENDING",
		fmt.Sprintf("Клиент запросил активацию ловушки на порту %d", port))

	// -------------------------------------------------------------
	// ШАГ 1: ПРЕДОХРАНИТЕЛЬ ПЕРИМЕТРА
	// Проверяем, не занят ли целевой порт легитимным процессом.
	// Используем инвентаризацию из `perimeter_inventory.json`.
	// -------------------------------------------------------------
	if err := checkPortAvailable(port); err != nil {
		sys.LogAction("TRAPCTL", "ACTIVATE_BLOCKED", "ALERT",
			fmt.Sprintf("Активация заблокирована предохранителем периметра: %v", err))
		return err
	}

	// -------------------------------------------------------------
	// ШАГ 2: ПРЕДОХРАНИТЕЛЬ РОКИРОВКИ (только для порта 22)
	// Если целевой порт = 22, проверяем, выполнена ли рокировка.
	// Без рокировки порт 22 занят легитимным OpenSSH.
	// -------------------------------------------------------------
	if port == config.C.StandardSshPort {
		if err := checkCastlingDone(); err != nil {
			sys.LogAction("TRAPCTL", "ACTIVATE_BLOCKED", "ALERT",
				fmt.Sprintf("Активация базовой ловушки заблокирована: %v", err))
			return err
		}
	}

	// -------------------------------------------------------------
	// ШАГ 3: ФОРМИРОВАНИЕ ДЕКЛАРАЦИИ ЛОВУШКИ
	// API-forward v8.0.3: запись декларации — только через транзакцию.
	// -------------------------------------------------------------
	tarpitID := sys.GenerateTarpitID(port)
	markerPath := sys.GenerateMarkerPath(port)

	// Определяем, является ли ловушка базовой (порт 22)
	isCore := port == config.C.StandardSshPort

	// Формируем штампы времени создания записи
	currentTime := time.Now()
	dateStr := currentTime.Format("2006-01-02")
	timeStr := currentTime.Format("15:04:05")

	// Собираем декларативное описание ловушки
	// ВАЖНО: Поля `ActivatedAt` и `WorkerPID` остаются пустыми.
	// Их заполнит ДЕМОН когда реально поднимет слушателя.
	newTrapDef := sys.TarpitDefinition{
		TarpitID:       tarpitID,
		Port:           port,
		IsCore:         isCore,
		Date:           dateStr,
		Time:           timeStr,
		State:          sys.StateRunning,   // Админ ХОЧЕТ чтобы ловушка работала
		Status:         sys.StatusDisabled, // Демон ещё не подтвердил (заполнит сам)
		ConfigConfig:   trapConfig,
		MarkerFilePath: markerPath,
		ActivatedAt:    "", // Заполнит демон
		WorkerPID:      0,  // Заполнит демон
	}

	// -------------------------------------------------------------
	// ШАГ 4: ТРАНЗАКЦИЯ ИНВЕНТАРЯ (flock, Аудит №1)
	// -------------------------------------------------------------
	if err := sys.WithInventoryTx(func(inv *sys.TarpitInventory) error {
		// v8.0.3 (Аудит №7): семантика СЛИЯНИЯ вместо перезаписи.
		// Реактивация существующей ловушки сохраняет штампы создания
		// и рантайм-поля демона; обновляется только декларация.
		if existing, exists := inv.Traps[tarpitID]; exists {
			existing.State = sys.StateRunning
			existing.ConfigConfig = trapConfig
			inv.Traps[tarpitID] = existing
		} else {
			inv.Traps[tarpitID] = newTrapDef
		}
		return nil
	}); err != nil {
		sys.LogAction("TRAPCTL", "ACTIVATE_FAIL", "ERROR",
			fmt.Sprintf("Сбой записи инвентаря: %v", err))
		return fmt.Errorf("критический сбой записи инвентаря ловушек: %w", err)
	}

	sys.LogAction("TRAPCTL", "INVENTORY_UPDATED", "SUCCESS",
		fmt.Sprintf("Декларация записана: ловушка %s → state=RUNNING", tarpitID))

	// -------------------------------------------------------------
	// ШАГ 5: ОБЕСПЕЧЕНИЕ ЖИЗНИ ДЕМОНА
	// Если демон не запущен — генерируем юнит и запускаем.
	// Демон прочитает инвентарь и поднимет слушателя.
	// -------------------------------------------------------------
	if err := EnsureDaemonRunning(); err != nil {
		sys.LogAction("TRAPCTL", "DAEMON_START_FAIL", "ERROR",
			fmt.Sprintf("Сбой запуска демона: %v", err))
		return fmt.Errorf("инвентарь записан, но сбой запуска демона: %w", err)
	}

	sys.LogAction("TRAPCTL", "ACTIVATE_SUCCESS", "SUCCESS",
		fmt.Sprintf("Ловушка %s активирована. Демон подхватит декларацию.", tarpitID))

	return nil
}

// ============================================================================
// ФУНКЦИЯ: Деактивация ловушки (универсальная)
// ============================================================================

// DeactivateTrap деактивирует ловушку на указанном порту.
// Записывает декларацию `state = STOPPED` в инвентарь.
// Демон прочитает декларацию и снимет слушателя.
//
// ВАЖНО: Эта функция НЕ удаляет маркеры. Маркеры удаляет демон.
// ВАЖНО: Эта функция НЕ останавливает демон. Демон продолжает жить
//
//	если есть другие активные ловушки.
//
// Параметры:
//   - port: номер TCP-порта ловушки для деактивации
func DeactivateTrap(port int) error {
	tarpitID := sys.GenerateTarpitID(port)

	// ИБ-АУДИТ: Фиксируем попытку деактивации
	sys.LogAction("TRAPCTL", "DEACTIVATE_REQUEST", "PENDING",
		fmt.Sprintf("Клиент запросил деактивацию ловушки на порту %d", port))

	// Транзакция: чтение → изменение → запись под flock (Аудит №1)
	if err := sys.WithInventoryTx(func(inv *sys.TarpitInventory) error {
		trap, exists := inv.Traps[tarpitID]
		if !exists {
			return fmt.Errorf("ловушка %s не найдена в инвентаре", tarpitID)
		}
		trap.State = sys.StateStopped
		inv.Traps[tarpitID] = trap
		return nil
	}); err != nil {
		return err
	}

	sys.LogAction("TRAPCTL", "INVENTORY_UPDATED", "SUCCESS",
		fmt.Sprintf("Декларация записана: ловушка %s → state=STOPPED", tarpitID))

	// -------------------------------------------------------------
	// ШАГ: ПРОВЕРКА — ОСТАЛИСЬ ЛИ ДРУГИЕ АКТИВНЫЕ ЛОВУШКИ?
	// Если нет — останавливаем демон.
	// -------------------------------------------------------------
	StopDaemonIfIdle()

	sys.LogAction("TRAPCTL", "DEACTIVATE_SUCCESS", "SUCCESS",
		fmt.Sprintf("Ловушка %s деактивирована. Демон подхватит декларацию.", tarpitID))

	return nil
}

// ============================================================================
// ФУНКЦИЯ: Удаление ловушки (промышленно-безопасный контур)
// ============================================================================
// DeleteTrap полностью удаляет ловушку из инвентаря.
// Требование: ловушка должна быть остановлена, а маркер — снят демоном.
// Это исключает гонки, призрачные маркеры и неожиданное поведение демона.
//
// АЛГОРИТМ:
//  1. Загружает инвентарь
//  2. Предохранитель: отказ, если ловушка активна (state = RUNNING)
//  3. Предохранитель: отказ, если маркер ещё существует
//  4. Удаляет запись из карты
//  5. Атомарно сохраняет инвентарь (POSIX)
//  6. Останавливает демон, если активных ловушек не осталось
//
// ВАЖНО: Маркер не удаляется здесь напрямую. К моменту удаления он уже
// снят демоном. Это гарантирует консистентность трёхфакторной модели.
func DeleteTrap(port int) error {
	tarpitID := sys.GenerateTarpitID(port)

	// ИБ-АУДИТ: Фиксируем попытку удаления
	sys.LogAction("TRAPCTL", "DELETE_REQUEST", "PENDING",
		fmt.Sprintf("Администратор инициировал удаление ловушки на порту %d", port))

	// Транзакция удаления под flock (Аудит №1)
	if err := sys.WithInventoryTx(func(inv *sys.TarpitInventory) error {
		trap, exists := inv.Traps[tarpitID]
		if !exists {
			return fmt.Errorf("ловушка %s не найдена в инвентаре", tarpitID)
		}
		if trap.State == sys.StateRunning {
			return fmt.Errorf("ловушка %s активна. Сначала остановите её (STOP)", tarpitID)
		}
		if _, errStat := os.Stat(trap.MarkerFilePath); errStat == nil {
			return fmt.Errorf("демон ещё завершает остановку ловушки %s. Повторите через несколько секунд", tarpitID)
		}
		delete(inv.Traps, tarpitID)
		return nil
	}); err != nil {
		return err
	}

	sys.LogAction("TRAPCTL", "INVENTORY_UPDATED", "SUCCESS",
		fmt.Sprintf("Ловушка %s удалена из инвентаря", tarpitID))

	// Останавливаем демон, если активных ловушек не осталось
	StopDaemonIfIdle()

	sys.LogAction("TRAPCTL", "DELETE_SUCCESS", "SUCCESS",
		fmt.Sprintf("Ловушка %s полностью удалена", tarpitID))

	return nil
}

// ============================================================================
// ФУНКЦИЯ: Обновление конфигурации существующей ловушки (API-forward)
// ============================================================================
// UpdateTrapConfig — ЕДИНСТВЕННЫЙ контур редактирования параметров ловушки.
// Сейчас его вызывает TUI-редактор, в будущем — Web/API-хендлеры.
// Порт не изменяется: TarpitID, маркер и UFW-правило привязаны к порту.
func UpdateTrapConfig(port int, cfg sys.TarpitConfig) error {
	tarpitID := sys.GenerateTarpitID(port)
	sys.LogAction("TRAPCTL", "CONFIG_UPDATE_REQUEST", "PENDING",
		fmt.Sprintf("Клиент запросил обновление конфигурации ловушки на порту %d", port))
	if err := sys.WithInventoryTx(func(inv *sys.TarpitInventory) error {
		trap, exists := inv.Traps[tarpitID]
		if !exists {
			return fmt.Errorf("ловушка %s не найдена в инвентаре", tarpitID)
		}
		if trap.State == sys.StateRunning {
			return fmt.Errorf("ловушка %s активна. Остановите её перед редактированием", tarpitID)
		}
		trap.ConfigConfig = cfg
		inv.Traps[tarpitID] = trap
		return nil
	}); err != nil {
		return err
	}
	sys.LogAction("TRAPCTL", "CONFIG_UPDATE_SUCCESS", "SUCCESS",
		fmt.Sprintf("Конфигурация ловушки %s обновлена", tarpitID))
	return nil
}

// ============================================================================
// ФУНКЦИЯ: Синхронизация глобального конфига с базовой ловушкой (API-forward)
// ============================================================================
// SyncGlobalConfigToCoreTrap синхронизирует delay_step/delay_variance/
// tarpit_max_conn из config.json с базовой ловушкой порта 22.
// Вынесено из TUI (view_settings.go) в контроллер: будущий Web/API
// получит ту же точку входа без переписывания ядра.
func SyncGlobalConfigToCoreTrap() error {
	coreID := sys.GenerateTarpitID(config.C.StandardSshPort)
	return sys.WithInventoryTx(func(inv *sys.TarpitInventory) error {
		trap, exists := inv.Traps[coreID]
		if !exists {
			return nil // Базовая ловушка ещё не создана — синхронизировать нечего
		}
		trap.ConfigConfig.DelayStep = config.C.DelayStep
		trap.ConfigConfig.DelayVariance = config.C.DelayVariance
		trap.ConfigConfig.MaxConn = config.C.TarpitMaxConn
		inv.Traps[coreID] = trap
		return nil
	})
}

// ============================================================================
// ФУНКЦИИ: Управление базовой ловушкой порта 22
// ============================================================================

// ActivateCoreTarpit активирует базовую ловушку на порту 22.
// Это обёртка над `ActivateTrap` с дополнительным предохранителем рокировки.
//
// ЛОГИКА: Порт 22 — особый. Он связан с рокировкой портов (кастлингом).
// Перед активацией проверяем, что рокировка выполнена и порт 22 свободен.
//
// Возвращает ошибку, если рокировка не выполнена.
func ActivateCoreTarpit(cfg sys.TarpitConfig) error {
	// Проверка рокировки выполняется внутри ActivateTrap для порта 22,
	// но дублируем её здесь для явного аудита.
	if err := checkCastlingDone(); err != nil {
		return err
	}

	return ActivateTrap(config.C.StandardSshPort, cfg)
}

// DeactivateCoreTarpit деактивирует базовую ловушку на порту 22.
// Снимает только слушателя, НЕ останавливает демон.
// Порт 22 освобождается и возвращается в состояние «быстрый отказ».
func DeactivateCoreTarpit() error {
	return DeactivateTrap(config.C.StandardSshPort)
}

// ============================================================================
// ФУНКЦИЯ: Статус всех ловушек
// ============================================================================

// GetAllTrapsStatus возвращает статус всех ловушек из инвентаря.
// Для каждой ловушки проверяется:
//  1. Декларация (что админ ХОЧЕТ)
//  2. Подтверждение демона (что реально произошло)
//  3. Маркер на диске (факт: слушатель поднят или нет)
//
// Эта функция вызывается клиентами для отображения статуса.
// Она НЕ изменяет ничего — только читает.
func GetAllTrapsStatus() ([]TrapStatus, error) {
	inv, err := sys.LoadTarpitInventory(sys.TarpitInventoryPath)
	if err != nil {
		// Если инвентарь не существует — возвращаем пустой список
		return []TrapStatus{}, nil
	}

	var statuses []TrapStatus

	for _, trap := range inv.Traps {
		// Проверяем существование маркера на диске (фактическое состояние)
		markerExists := false
		if _, errStat := os.Stat(trap.MarkerFilePath); errStat == nil {
			markerExists = true
		}

		status := TrapStatus{
			TarpitID:      trap.TarpitID,
			Port:          trap.Port,
			IsCore:        trap.IsCore,
			DeclaredState: trap.State,
			DaemonStatus:  trap.Status,
			MarkerExists:  markerExists,
			WorkerPID:     trap.WorkerPID,
			ActivatedAt:   trap.ActivatedAt,
			Config:        trap.ConfigConfig,
		}

		statuses = append(statuses, status)
	}

	return statuses, nil
}

// ============================================================================
// ФУНКЦИЯ: Статус одной ловушки по порту
// ============================================================================

// GetTrapStatusByPort возвращает статус конкретной ловушки по номеру порта.
// Возвращает ошибку, если ловушка с таким портом не найдена в инвентаре.
func GetTrapStatusByPort(port int) (*TrapStatus, error) {
	allStatuses, err := GetAllTrapsStatus()
	if err != nil {
		return nil, err
	}

	for i := range allStatuses {
		if allStatuses[i].Port == port {
			return &allStatuses[i], nil
		}
	}

	return nil, fmt.Errorf("ловушка на порту %d не найдена в инвентаре", port)
}

// ============================================================================
// ФУНКЦИИ: Управление жизненным циклом демона
// ============================================================================

// IsDaemonAlive проверяет, жив ли демон ловушек.
// Использует `systemctl is-active` для проверки статуса юнита.
// Возвращает true если демон активен, иначе false.
//
// v8.2.3 (R-2b): прямой exec.Command заменяем на sys.RunTimeout.
// Это даёт таймаут 5 секунд и единый стиль работы с внешними командами.
// Если systemctl упадёт или зависнет, мы не повесим горутину контроллера.
func IsDaemonAlive() bool {
	// Запрашиваем статус юнита с таймаутом 5 секунд.
	res := sys.RunTimeout(5*time.Second, "systemctl", "is-active", ServiceName)

	// Если команда завершилась с ошибкой (например, юнит не найден или неактивен),
	// res.Stdout будет пустым или содержать "inactive", и сравнение вернёт false.
	// Это полностью сохраняет старую семантику: любая ошибка означает "демон мёртв".
	return strings.TrimSpace(res.Stdout) == "active"
}

// EnsureDaemonRunning гарантирует, что демон ловушек запущен.
// Если демон уже активен — ничего не делает.
// Если юнит не установлен — генерирует его и устанавливает.
// Если юнит установлен но не запущен — запускает.
//
// ВАЖНО: Эта функция вызывается контроллером при активации ПЕРВОЙ ловушки.
// При последующих активациях демон уже жив, и функция просто проверяет.
func EnsureDaemonRunning() error {
	// Быстрая проверка: демон уже жив?
	if IsDaemonAlive() {
		sys.LogAction("TRAPCTL", "DAEMON_CHECK", "SUCCESS",
			"Демон ловушек уже активен. Дополнительный запуск не требуется.")
		return nil
	}

	// Демон не жив. Проверяем, установлен ли юнит.
	if _, err := os.Stat(ServicePath); os.IsNotExist(err) {
		// Юнит не существует — генерируем и устанавливаем
		if err := generateAndInstallUnit(); err != nil {
			return fmt.Errorf("сбой генерации юнита демона: %w", err)
		}
		sys.LogAction("TRAPCTL", "UNIT_GENERATED", "SUCCESS",
			"Юнит agld_tarpit.service успешно сгенерирован и установлен.")
	}

	// Запускаем демон через systemctl.
	//
	// v8.2.3 (R-2b): заменяем три прямых exec.Command на sys.RunTimeout.
	//
	// daemon-reload (10 сек): перечитывает юниты. Ошибка не фатальна,
	// но должна быть видна в журнале (если systemd сломан, мы должны знать).
	sys.RunTimeout(10*time.Second, "systemctl", "daemon-reload").
		LogWarnIfErr("TRAPCTL", "DAEMON_RELOAD", "event=systemd.daemon_reload_failed")

	// enable (10 сек): добавляет в автозагрузку. Ошибка не фатальна,
	// но без автозагрузки демон не поднимется после ребута хоста.
	sys.RunTimeout(10*time.Second, "systemctl", "enable", ServiceName).
		LogWarnIfErr("TRAPCTL", "DAEMON_ENABLE", "event=systemd.enable_failed")

	// start (20 сек): непосредственный запуск процесса.
	// Таймаут 20 секунд выбран с запасом: старт может включать инициализацию
	// инвентаря, открытие сокетов и поднятие UFW-правил.
	// В отличие от reload/enable, ошибка старта фатальна: без живого демона
	// ловушки не поднимутся, поэтому возвращаем ошибку наверх.
	startRes := sys.RunTimeout(20*time.Second, "systemctl", "start", ServiceName)
	if startRes.Err != nil {
		// Оборачиваем ошибку и добавляем stderr для диагностики.
		// В stderr systemd часто пишет реальную причину: "port already in use",
		// "permission denied", "binary not found".
		return fmt.Errorf("критический сбой запуска демона ловушек: %w; stderr=%s",
			startRes.Err, strings.TrimSpace(startRes.Stderr))
	}

	sys.LogAction("TRAPCTL", "DAEMON_STARTED", "SUCCESS",
		"Демон ловушек успешно запущен: systemctl start "+ServiceName)

	return nil
}

// StopDaemonIfIdle останавливает демон, если в инвентаре не осталось
// ни одной активной ловушки (все записи имеют state = STOPPED).
//
// АЛГОРИТМ:
//  1. Читает инвентарь
//  2. Подсчитывает количество ловушек с state = RUNNING
//  3. Если 0 → останавливает и деинсталлирует демон
//
// ВАЖНО: Эта функция вызывается контроллером при деактивации ПОСЛЕДНЕЙ ловушки.
// Демон не убивается, пока есть хотя бы одна активная ловушка.
func StopDaemonIfIdle() {
	// Загружаем инвентарь
	inv, err := sys.LoadTarpitInventory(sys.TarpitInventoryPath)
	if err != nil {
		// Если инвентарь не читается — не рискуем останавливать демон
		sys.LogAction("TRAPCTL", "STOP_CHECK_SKIP", "WARNING",
			"Не удалось прочитать инвентарь. Остановка демона пропущена.")
		return
	}

	// Подсчитываем активные ловушки
	activeCount := sys.CountActiveTraps(inv)

	if activeCount > 0 {
		// Есть активные ловушки — демон должен продолжать работать
		sys.LogAction("TRAPCTL", "STOP_CHECK_SKIP", "SUCCESS",
			fmt.Sprintf("Активных ловушек: %d. Демон продолжает работать.", activeCount))
		return
	}

	// Активных ловушек не осталось. Останавливаем демон.
	// ЛИКВИДАЦИЯ ПРИЗРАКА: Сначала снимаем автозапуск через disable.
	// Это приказывает systemd игнорировать Restart=always.
	//
	// v8.2.3 (R-2b): заменяем прямые exec.Command на sys.RunTimeout.
	//
	// disable (10 сек): снимает с автозагрузки. Ошибка не останавливает ликвидацию,
	// но должна быть зафиксирована, иначе после ребута демон воскреснет.
	sys.RunTimeout(10*time.Second, "systemctl", "disable", ServiceName).
		LogWarnIfErr("TRAPCTL", "DAEMON_DISABLE", "event=systemd.disable_failed")

	// stop (10 сек): отправляет SIGTERM процессу. systemd корректно завершит
	// все дочерние горутины и воркеры ловушек.
	sys.RunTimeout(10*time.Second, "systemctl", "stop", ServiceName).
		LogWarnIfErr("TRAPCTL", "DAEMON_STOP", "event=systemd.stop_failed")

	sys.LogAction("TRAPCTL", "DAEMON_STOPPED", "SUCCESS",
		"Все ловушки деактивированы. Демон остановлен.")

	// Удаляем юнит из системы
	_ = os.Remove(ServicePath)

	// daemon-reload (10 сек): заставляем systemd забыть о удалённом файле юнита.
	// Без этого systemd будет пытаться запустить несуществующий бинарник
	// при ручном `systemctl start agld_tarpit.service`.
	sys.RunTimeout(10*time.Second, "systemctl", "daemon-reload").
		LogWarnIfErr("TRAPCTL", "DAEMON_RELOAD_AFTER_REMOVAL", "event=systemd.daemon_reload_failed")

	sys.LogAction("TRAPCTL", "UNIT_REMOVED", "SUCCESS",
		"Юнит agld_tarpit.service деинсталлирован из системы.")
}

// ============================================================================
// ФУНКЦИЯ: Генерация и установка systemd-юнита
// ============================================================================

// generateAndInstallUnit генерирует файл юнита для демона ловушек
// и записывает его в /etc/systemd/system/.
//
// Юнит запускает бинарник с флагом --tarpit-daemon, который
// запускает StartTarpitManager() — менеджер всех ловушек.
//
// ПАРАМЕТРЫ ЮНИТА:
//
//	Type=exec      — systemd ждёт, пока процесс реально стартует
//	User=root      — демон требует привилегий для привязки портов < 1024
//	Restart=always — самовосстановление при падении (кроме случая
//	                  когда демон сам решает остановиться через disable)
//	RestartSec=5   — пауза 5 секунд перед самовосстановлением
func generateAndInstallUnit() error {
	serviceContent := fmt.Sprintf(`[Unit]
Description=Amber Guard Lab. - Autonomous Cyber Threat Intelligence Tarpit Engine
After=network-online.target ufw.service
Wants=network-online.target

[Service]
Type=exec
User=root
ExecStart=%s --tarpit-daemon
Restart=always
RestartSec=5
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
`, BinaryPath)

	// Записываем юнит на диск
	if err := os.WriteFile(ServicePath, []byte(serviceContent), 0644); err != nil {
		return fmt.Errorf("сбой записи юнита на диск: %w", err)
	}

	return nil
}

// ============================================================================
// ПРЕДОХРАНИТЕЛИ: Вспомогательные функции проверки
// ============================================================================

// checkPortAvailable проверяет, не занят ли целевой порт легитимным процессом.
// Использует инвентаризацию периметра из `perimeter_inventory.json`.
//
// ЛОГИКА:
//   1. Загружает список обнаруженных приложений (DetectedApps)
//   2. Ищет приложение, слушающее на целевом порту
//   3. Если найдено → порт занят → блокируем активацию
//
// Это защищает админа от:
//   - Случайного ввода порта, на котором сидит легитимный сервис
//   - Намеренной попытки развернуть ловушку на занятом порту

// checkPortAvailable проверяет, не занят ли целевой порт легитимным процессом.
// Использует инвентаризацию периметра из `perimeter_inventory.json`.
//
// ОБНОВЛЕНО В v8.0.1: Добавлен фильтр для игнорирования собственных процессов AGL.
// Наши ловушки не должны блокировать сами себя.
func checkPortAvailable(port int) error {
	apps, err := config.LoadDetectedApps()
	if err != nil {
		sys.LogAction("TRAPCTL", "PERIMETER_CHECK_SKIP", "WARNING",
			fmt.Sprintf("Инвентарь периметра недоступен: %v. Проверка порта пропущена.", err))
		return nil
	}

	for _, app := range apps {
		if app.Port == port {
			// ИБ-ФИЛЬТР: Игнорируем наши собственные процессы AGL
			// Это предотвращает ситуацию, когда ловушка блокирует саму себя
			appNameUpper := strings.ToUpper(app.Name)
			if appNameUpper == "AGL" || strings.Contains(appNameUpper, "AGL") {
				continue
			}

			return fmt.Errorf(
				"порт %d занят легитимным процессом %s. Развертывание ловушки невозможно",
				port, app.Name)
		}
	}

	// v8.2.0 (2.7): реальная проверка ядра через net.Listen.
	// Инвентарь — это слепок между сканами, он устаревает.
	// net.Listen — ground truth: если порт уже слушается кем-то
	// (легитимный сервис, наш демон, веб-шлюз) — bind упадёт,
	// и ловушка не сможет подняться. Блокируем активацию заранее,
	// не оставляя демону только постфактумный SOCKET_BIND_FAIL.
	//
	// Почему безопасно закрывать conn сразу:
	// мы не держим соединение, а лишь занимаем сокет на микросекунды,
	// чтобы проверить, что ядро разрешает bind.
	probeAddr := fmt.Sprintf(":%d", port)
	probeListener, errProbe := net.Listen("tcp", probeAddr)
	if errProbe != nil {
		return fmt.Errorf("порт %d уже занят в ядре ОС (ground truth): %w", port, errProbe)
	}
	// Мгновенно освобождаем — реальный bind произойдёт в демоне позже.
	_ = probeListener.Close()

	return nil
}

// checkCastlingDone проверяет, выполнена ли рокировка портов (кастлинг).
// Рокировка — это перенос легитимного OpenSSH с порта 22 на скрытый порт.
// Без рокировки порт 22 занят легитимным OpenSSH, и ловушка не может его занять.
//
// ЛОГИКА:
//  1. Проверяет наличие маркера `agl_ssh_moved` на диске
//  2. Если маркера нет → рокировка не выполнена → блокируем активацию
//  3. Если маркер есть → дополнительно проверяем, что реальный порт
//     не совпадает со стандартным (защита от рассинхронизации)
//
// Возвращает ошибку, если рокировка не выполнена.
func checkCastlingDone() error {
	// Фактор 1: Наличие маркера рокировки на диске
	if _, err := os.Stat(CastlingMarkerPath); os.IsNotExist(err) {
		return fmt.Errorf(
			"блокировка запуска: рокировка портов не выполнена. " +
				"Легитимный OpenSSH всё ещё удерживает порт 22. " +
				"Сначала выполните рокировку (Кастлинг)")
	}

	// Фактор 2: Проверяем, что реальный порт не совпадает со стандартным
	activePort := sys.DetectActiveSshPort()
	standardPortStr := fmt.Sprintf("%d", config.C.StandardSshPort)
	if activePort == standardPortStr {
		return fmt.Errorf(
			"блокировка запуска: маркер рокировки существует, но детектор "+
				"показывает, что OpenSSH всё ещё на порту %s. Рассинхронизация состояния",
			standardPortStr)
	}

	// Рокировка выполнена — проверка пройдена
	return nil
}
