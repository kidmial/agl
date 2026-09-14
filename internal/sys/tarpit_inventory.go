// internal/sys/tarpit_inventory.go
// ============================================================================
// КОМПОНЕНТ: Контракт данных инвентаря ловушек Дегтярной Ямы
// ВЕРСИЯ: v8.0.1
// РОЛЬ: Единый источник правды для декларативного состояния всех ловушек.
//
//	Читается демоном (исполнитель) и клиентами (TUI, Web, API).
//
// ============================================================================
//
// ### [2026-08-22] — Мажорный рефакторинг контракта данных (v8.0.1)
//   - Переименование поля `Jitter` → `DelayVariance` для консистентности
//     с глобальным конфигом `config.C.DelayVariance`. Устранение двойной
//     терминологии.
//   - Добавление поля `MaxConn` в `TarpitConfig` — индивидуальный лимит
//     параллельных ботов для каждой ловушки.
//   - Добавление поля `IsCore` в `TarpitDefinition` — флаг особой ловушки
//     порта 22, связанной с рокировкой портов.
//   - Добавление поля `ActivatedAt` в `TarpitDefinition` — рантайм-метаданная,
//     фиксирующая момент, когда демон реально поднял слушателя.
//   - Добавление поля `WorkerPID` в `TarpitDefinition` — PID процесса-слушателя
//     для детекции мёртвых маркеров в рамках Self-Healing.
//   - Обновление JSON-тегов для полного соответствия контракту v8.0.1.
//
// ============================================================================
package sys

import (
	"agl/config"
	"agl/i18n"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// ============================================================================
// КОНСТАНТЫ И ПУТИ
// ============================================================================

// Каноничный путь к декларативному инвентарю ловушек.
// Это ЕДИНСТВЕННЫЙ файл, в который пишут КЛИЕНТЫ (TUI, Web, API).
// Демон читает этот файл и приводит систему к нужному состоянию.
const TarpitInventoryPath = config.FileTarpitInventory

// Каноничная директория для персистентных маркеров состояния.
// Маркеры создаются/удаляются ИСКЛЮЧИТЕЛЬНО демоном.
// Клиенты могут только ЧИТАТЬ маркеры для отображения статуса.
const StatesDir = config.DirLib

// ============================================================================
// СТРУКТУРЫ ДАННЫХ (КОНТРАКТ v8.0.1)
// ============================================================================

// TarpitConfig описывает индивидуальные параметры воркера ловушки.
// Каждая ловушка имеет СВОЙ набор параметров, не зависящий от глобального конфига.
//
// ИМЕНОВАНИЕ ПОЛЕЙ (консистентность с config.go):
//   - DelayStep     → базовый интервал удержания (сек)
//   - DelayVariance → вариативность разброса (сек), БЫВШЕЕ "Jitter"
//   - MaxConn       → лимит параллельных ботов в ОЗУ данной ловушки
type TarpitConfig struct {
	// SSHVersion — текстовый баннер, который ловушка отдаёт боту после Handshake.
	// Имитирует легитимный сервис для максимальной правдоподобности.
	SSHVersion string `json:"ssh_version"`

	// DelayStep — базовый интервал удержания одного TCP-соединения (в секундах).
	// Чем выше значение, тем дольше атакующий скрипт находится в ожидании.
	// Рекомендуемый диапазон: от 5 до 60 секунд.
	DelayStep int `json:"delay_step"`

	// DelayVariance — вариативность разброса джиттера (в секундах).
	// Итоговый сон вычисляется как: DelayStep ± DelayVariance.
	// Защищает капкан от обнаружения по таймингам (Timing Attacks).
	// ПЕРЕИМЕНОВАНО из `Jitter` для консистентности с config.C.DelayVariance.
	DelayVariance int `json:"delay_variance"`

	// MaxConn — максимальное количество одновременных горутин-потоков
	// для данной ловушки. Защищает сервер от DDoS-атак, направленных
	// на исчерпание дескрипторов файлов (FD) и перегрузку ОЗУ.
	// НОВОЕ ПОЛЕ v8.0.1: Каждая ловушка имеет свой индивидуальный лимит.
	MaxConn int `json:"tarpit_max_conn"`
}

// TarpitDefinition описывает полную декларацию одной ловушки в инвентаре.
// Это ЕДИНИЦА УПРАВЛЕНИЯ в модели «один демон — много ловушек».
//
// РАЗДЕЛЕНИЕ ОТВЕТСТВЕННОСТИ ПО ПОЛЯМ:
//   - Поля, которые пишет КЛИЕНТ (TUI, Web, API):
//     TarpitID, Port, IsCore, Date, Time, State, TarpitConfig, MarkerFilePath
//   - Поля, которые пишет ДЕМОН (исполнитель):
//     Status, ActivatedAt, WorkerPID
type TarpitDefinition struct {
	// ═══ ИДЕНТИФИКАЦИЯ ═══

	// TarpitID — уникальный идентификатор ловушки.
	// Совпадает с именем файла-маркера в /var/lib/amberguard/states/.
	// Формат строго по регламенту v6.3.9: agl_tarpit_listener_port.НОМЕР
	// Пример: "agl_tarpit_listener_port.22", "agl_tarpit_listener_port.8080"
	TarpitID string `json:"tarpit_id"`

	// Port — номер TCP-порта, на котором ловушка слушает соединения.
	// Дублирует порт из имени маркера для валидации и быстрого доступа.
	Port int `json:"port"`

	// IsCore — флаг особой ловушки порта 22, связанной с рокировкой портов.
	// Если true, демон перед активацией проверяет маркер рокировки `agl_ssh_moved`.
	// Если рокировка не выполнена — активация блокируется.
	// НОВОЕ ПОЛЕ v8.0.1.
	IsCore bool `json:"is_core"`

	// ═══ МЕТАДАННЫЕ СОЗДАНИЯ ═══

	// Date — дата создания записи в инвентаре (формат: "2006-01-02").
	// Пишется клиентом при создании ловушки.
	Date string `json:"date"`

	// Time — время создания записи в инвентаре (формат: "15:04:05").
	// Пишется клиентом при создании ловушки.
	Time string `json:"time"`

	// ═══ УПРАВЛЯЕМЫЕ СОСТОЯНИЯ ═══

	// State — декларация админа: что он ХОЧЕТ.
	// Допустимые значения: "RUNNING" / "STOPPED".
	// Пишется КЛИЕНТОМ (TUI, Web, API).
	// Демон читает это поле и принимает решение о запуске/остановке слушателя.
	State string `json:"state"`

	// Status — подтверждение демона: что реально ПРОИЗОШЛО.
	// Допустимые значения: "ACTIVE" / "DISABLED".
	// Пишется ДЕМОНОМ после выполнения действия.
	// Клиент может сравнить State (что хотел) с Status (что получилось)
	// и увидеть рассинхронизацию.
	// НОВОЕ РАЗДЕЛЕНИЕ СЕМАНТИКИ v8.0.1.
	Status string `json:"status"`

	// ═══ ПАРАМЕТРЫ ВОРКЕРА ═══

	// ConfigConfig — вложенная структура индивидуальных параметров воркера.
	// Каждая ловушка имеет свой набор параметров.
	ConfigConfig TarpitConfig `json:"tarpit_config"`

	// ═══ СИСТЕМНЫЕ ПРИВЯЗКИ ═══

	// MarkerFilePath — полный путь к персистентному маркеру в /var/lib/amberguard/states/.
	// Маркер создаётся/удаляется ИСКЛЮЧИТЕЛЬНО демоном.
	// Пример: "/var/lib/amberguard/states/agl_tarpit_listener_port.22"
	MarkerFilePath string `json:"marker_file_path"`

	// ═══ РАНТАЙМ-МЕТАДАННЫЕ (пишет ДЕМОН) ═══

	// ActivatedAt — момент, когда демон реально поднял слушателя.
	// Формат: "2006-01-02 15:04:05".
	// НЕ то же самое, что Date+Time (время создания записи в JSON).
	// Между этими событиями может пройти до 15 секунд (период Reconciliation Loop).
	// НОВОЕ ПОЛЕ v8.0.1.
	ActivatedAt string `json:"activated_at"`

	// WorkerPID — PID процесса (горутины), который держит сокет слушателя.
	// Используется для детекции мёртвых маркеров в рамках Self-Healing:
	// если процесс с этим PID не существует в /proc/, маркер — призрак.
	// НОВОЕ ПОЛЕ v8.0.1.
	WorkerPID int `json:"worker_pid"`
}

// TarpitInventory — общий контейнер для всех ловушек.
// Ключ карты = TarpitID (совпадает с именем маркера).
type TarpitInventory struct {
	Traps map[string]TarpitDefinition `json:"traps"`
}

// ============================================================================
// КОНСТАНТЫ СОСТОЯНИЙ (Единый реестр допустимых значений)
// ============================================================================

// Допустимые значения поля State (декларация клиента)
const (
	StateRunning = "RUNNING" // Админ хочет, чтобы ловушка работала
	StateStopped = "STOPPED" // Админ хочет, чтобы ловушка стояла
)

// Допустимые значения поля Status (подтверждение демона)
const (
	StatusActive   = "ACTIVE"   // Демон поднял слушателя, он реально работает
	StatusDisabled = "DISABLED" // Демон снял слушателя
)

// ============================================================================
// ФУНКЦИИ ЧТЕНИЯ И ЗАПИСИ
// ============================================================================

// LoadTarpitInventory читает и парсит JSON-файл инвентаря ловушек.
// Возвращает указатель на структуру или ошибку, если файл не существует/повреждён.
//
// ВАЖНО: Эта функция НЕ создаёт файл, если его нет.
// Для инициализации пустого инвентаря используйте InitEmptyInventory.
func LoadTarpitInventory(filePath string) (*TarpitInventory, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf(i18n.T.TarpitInvReadFail, filePath, err)
	}

	var inv TarpitInventory
	if err := json.Unmarshal(data, &inv); err != nil {
		return nil, fmt.Errorf(i18n.T.TarpitInvParseFail, filePath, err)
	}

	// Защита от nil-карты при пустом файле
	if inv.Traps == nil {
		inv.Traps = make(map[string]TarpitDefinition)
	}

	return &inv, nil
}

// SaveTarpitInventory производит атомарную POSIX-запись инвентаря ловушек в JSON-файл.
// Реализует паттерн POSIX Atomic Write: запись во временный файл → fsync → rename.
// Это гарантирует, что при внезапном отключении питания файл не будет повреждён.
//
// ПРАВА ДОСТУПА: 0600 (только root может читать/писать).
// Это защищает инвентарь от модификации непривилегированными процессами.
func SaveTarpitInventory(filePath string, inv *TarpitInventory) error {
	// Маршалинг структуры в байты с красивыми отступами для читаемости
	data, err := json.MarshalIndent(inv, "", "    ")
	if err != nil {
		return fmt.Errorf(i18n.T.TarpitInvSerializeFail, err)
	}

	// Гарантируем существование директории
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return fmt.Errorf(i18n.T.TarpitInvDirCreateFail, err)
	}

	// Открываем временный файл для записи
	tmpFile := filePath + ".tmp"
	f, err := os.OpenFile(tmpFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf(i18n.T.TarpitInvTmpCreateFail, err)
	}

	// Запись байтового массива в дескриптор файла
	if _, err := f.Write(data); err != nil {
		f.Close()
		_ = os.Remove(tmpFile)
		return fmt.Errorf(i18n.T.TarpitInvDataWriteFail, err)
	}

	// КРИТИЧЕСКИЙ ШАГ: Принудительно вымываем кеш контроллера диска
	// ядра Linux на физические носители. Устраняет риск потери данных
	// при внезапном отключении питания.
	if err := f.Sync(); err != nil {
		f.Close()
		_ = os.Remove(tmpFile)
		return fmt.Errorf(i18n.T.TarpitInvFsyncFail, err)
	}

	// Закрываем дескриптор перед операцией переименования
	f.Close()

	// АТОМАРНАЯ СМЕНА ИМЕНИ: Подменяем старый рабочий файл новым
	// за одну инструкцию процессора VFS. Исключает состояние гонки.
	if err := os.Rename(tmpFile, filePath); err != nil {
		_ = os.Remove(tmpFile)
		return fmt.Errorf(i18n.T.TarpitInvRenameFail, err)
	}

	return nil
}

// InitEmptyInventory создаёт пустой инвентарь ловушек, если файл не существует.
// Используется при первом старте системы или при отсутствии файла.
// НЕ перезаписывает существующий файл.
func InitEmptyInventory(filePath string) error {
	// Если файл уже существует — ничего не делаем
	if _, err := os.Stat(filePath); err == nil {
		return nil
	}

	// Создаём пустой инвентарь
	emptyInv := &TarpitInventory{
		Traps: make(map[string]TarpitDefinition),
	}

	return SaveTarpitInventory(filePath, emptyInv)
}

// ============================================================================
// ВСПОМОГАТЕЛЬНЫЕ ФУНКЦИИ
// ============================================================================

// GenerateTarpitID формирует эталонный идентификатор ловушки по регламенту v6.3.9.
// Формула: agl_tarpit_listener_port.НОМЕР
// Точка отделяет контролируемый сокет.
// Пример: GenerateTarpitID(22) → "agl_tarpit_listener_port.22"
func GenerateTarpitID(port int) string {
	return fmt.Sprintf("agl_tarpit_listener_port.%d", port)
}

// GenerateMarkerPath формирует полный путь к персистентному маркеру ловушки.
// Маркер размещается в гарантированной зоне постоянного диска.
// Пример: GenerateMarkerPath(22) → "/var/lib/amberguard/states/agl_tarpit_listener_port.22"
func GenerateMarkerPath(port int) string {
	return fmt.Sprintf("%s/%s", StatesDir, GenerateTarpitID(port))
}

// CountActiveTraps подсчитывает количество ловушек со статусом RUNNING в инвентаре.
// Используется для принятия решения о запуске/остановке демона:
//   - Если > 0 → демон должен быть запущен
//   - Если == 0 → демон может быть остановлен
func CountActiveTraps(inv *TarpitInventory) int {
	if inv == nil || inv.Traps == nil {
		return 0
	}
	count := 0
	for _, trap := range inv.Traps {
		if trap.State == StateRunning {
			count++
		}
	}
	return count
}

// FindTrapByPort ищет ловушку по номеру порта в инвентаре.
// Возвращает указатель на определение и true, если найдено.
// Возвращает nil и false, если ловушка с таким портом не найдена.
func FindTrapByPort(inv *TarpitInventory, port int) (*TarpitDefinition, bool) {
	if inv == nil || inv.Traps == nil {
		return nil, false
	}
	for id := range inv.Traps {
		if inv.Traps[id].Port == port {
			trap := inv.Traps[id]
			return &trap, true
		}
	}
	return nil, false
}

// ============================================================================
// ТРАНЗАКЦИОННЫЙ СЛОЙ ИНВЕНТАРЯ (v8.0.3, Аудит №1)
// ============================================================================
// TarpitInventoryLockPath — сайдкар-файл блокировки.
// НИКОГДА не переименовывается, поэтому flock держится за стабильный inode.
const TarpitInventoryLockPath = TarpitInventoryPath + ".lock"

// WithInventoryTx выполняет транзакцию «чтение-изменение-запись» над инвентарём
// под эксклюзивной advisory-блокировкой (flock).
//
// АРХИТЕКТУРНАЯ ПОЗИЦИЯ (API-forward):
//
//	Примитив стоит в `sys`, потому что инвентарь пишут И клиенты (tarpitctl),
//	И демон (tarpit). Любой будущий клиент (Web/API) получает защиту автоматически.
//
// СВОЙСТВА:
//   - flock снимается ОС при смерти процесса — вечная блокировка невозможна;
//   - lock-файл не участвует в rename — блокировка не слетает при подмене inode;
//   - чтения без блокировки остаются безопасными: rename даёт консистентный снимок.
func WithInventoryTx(fn func(inv *TarpitInventory) error) error {
	lockFile, err := os.OpenFile(TarpitInventoryLockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf(i18n.T.TarpitInvLockOpenFail, err)
	}
	defer lockFile.Close()

	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf(i18n.T.TarpitInvLockAcquireFail, err)
	}
	defer syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)

	inv, err := LoadTarpitInventory(TarpitInventoryPath)
	if err != nil {
		// Файла нет — стартуем с пустого инвентаря (первый запуск)
		if _, statErr := os.Stat(TarpitInventoryPath); os.IsNotExist(statErr) {
			inv = &TarpitInventory{Traps: make(map[string]TarpitDefinition)}
		} else {
			return fmt.Errorf(i18n.T.TarpitInvTxCurrupted, err)
		}
	}

	if err := fn(inv); err != nil {
		return err
	}

	return SaveTarpitInventory(TarpitInventoryPath, inv)
}
