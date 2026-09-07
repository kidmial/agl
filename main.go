// main.go
// ============================================================================
// КОМПОНЕНТ: Точка входа комплекса кибербезопасности «Амберная Гвардия Лаборатория»
// ВЕРСИЯ: v8.0.1
// РОЛЬ: Инициализация окружения, маршрутизация режимов запуска (демон / TUI),
//
//	реанимация состояний при старте.
//
// ============================================================================
//
// АРХИТЕКТУРНАЯ ПОЗИЦИЯ:
//
//	Этот файл — точка входа бинарника `agl`.
//	Он определяет, в каком режиме запущен процесс:
//
//	1. Режим ДЕМОНА:  `agl --tarpit-daemon`
//	   → Запускает централизованный менеджер ловушек.
//	   → Управляет ВСЕМИ ловушками (порт 22 + мульти-ловушки).
//	   → Живёт как systemd-сервис `agld_tarpit.service`.
//
//	2. Режим TUI:     `agl` (без аргументов)
//	   → Запускает интерактивную панель управления.
//	   → НЕ запускает слушателей и НЕ управляет демоном напрямую.
//	   → Вызывает `tarpitctl` для управления ловушками.
//
// КЛЮЧЕВОЕ ИЗМЕНЕНИЕ v8.0.1:
//
//	В режиме TUI больше НЕ запускаются слушатели и менеджер ловушек.
//	Все ловушки управляются исключительно демоном через `tarpitctl`.
//	Это устраняет Проблему 2 (потеря состояния при перезаходе).
//
// ### [2026-08-22] — Мажорный рефакторинг точки входа (v8.0.1)
//   - Убран преждевременный `go StartTarpitCoreListener()` из Этапа 3.
//   - Убран `go StartTarpitManager()` из Этапа 3 (режим TUI).
//   - Добавлена инициализация IPC-счётчика: `InitShmMaster()` в демоне,
//     `InitShmSlave()` в TUI.
//   - Добавлена обработка сигналов (SIGINT, SIGTERM) в режиме демона.
//   - Полностью переписан `ReconcileSystemStates()`:
//     источник правды = инвентарь + маркеры, а не `net.Listen`.
//   - Убраны импорты `net` и `agl/internal/armor` из `ReconcileSystemStates()`.
//   - Добавлен импорт `agl/internal/tarpitctl` для управления демоном.
//
// ============================================================================
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"agl/config"
	"agl/i18n"
	"agl/internal/armor"
	"agl/internal/buildinfo"
	"agl/internal/sys"
	"agl/internal/tarpit"
	"agl/internal/tarpitctl"
	"agl/tui"
)

// ============================================================================
// ГЕНЕРАЦИЯ КОНФИГУРАЦИИ ПРИ ПЕРВОМ СТАРТЕ
// ============================================================================

// ensureDefaultConfig проверяет наличие конфигурации и автоматически
// генерирует её при первом старте. Вызывается ОДИН раз в `main()`
// до загрузки конфига.
//
// АЛГОРИТМ:
//  1. Проверить, существует ли файл конфигурации
//  2. Если существует → ничего не делать
//  3. Если не существует → сгенерировать эталонную конфигурацию
//  4. Записать через POSIX Atomic Write (временный файл → rename)
//
// ВАЖНО: Функция НЕ перезаписывает существующий файл.
// Она срабатывает ТОЛЬКО при отсутствии файла.
func ensureDefaultConfig(configPath string) {
	if _, err := os.Stat(configPath); err == nil {
		return // Файл уже существует — ничего не делаем
	} else if !errors.Is(err, os.ErrNotExist) {
		log.Fatalf("[КРИТИЧЕСКИЙ СБОЙ ИНИЦИАЛИЗАЦИИ]: Ошибка проверки путей: %v", err)
	}

	// Файл отсутствует. Порождаем эталонную конфигурацию мажорной версии.
	// ОБНОВЛЕНО В v8.0.1: Структура соответствует обновлённому `config.go`.
	defaultConfig := config.Config{
		Lang:             "en", // v8.3.0 (6.3): дефолт генерации — английский
		StandardSshPort:  22,
		RealSshPort:      2244,
		DelayStep:        13,
		DelayVariance:    8,
		TarpitMaxConn:    100,
		TarpitBacklog:    1024,
		TarpitConnLog:    config.FileTarpitConnLog, // БЛОК 4.4: Константа пути
		TarpitRptLog:     config.FileTarpitRptLog,  // БЛОК 4.4: Константа пути
		AglLogarchiveDir: config.DirLog,            // БЛОК 4.4: Константа пути
		BackupRootDir:    "/backup",
		GeoTool:          "geoiplookup",
		SysctlCore: config.SysctlCoreConfig{
			// Default (заводские эталоны)
			DefaultNetIpv4TcpSyncookies:       1,
			DefaultNetIpv4TcpFinTimeout:       15,
			DefaultNetIpv4TcpSynRetries:       2,
			DefaultNetIpv4TcpSynackRetries:    2,
			DefaultNetCoreSomaxconn:           4096,
			DefaultNetIpv4TcpMaxSynBacklog:    4096,
			DefaultNetIpv4ConfAllRpFilter:     1,
			DefaultNetIpv4ConfDefaultRpFilter: 1,
			// Armor (боевые параметры администратора)
			ArmorNetIpv4TcpSyncookies:       1,
			ArmorNetIpv4TcpFinTimeout:       15,
			ArmorNetIpv4TcpSynRetries:       2,
			ArmorNetIpv4TcpSynackRetries:    2,
			ArmorNetCoreSomaxconn:           4096,
			ArmorNetIpv4TcpMaxSynBacklog:    4096,
			ArmorNetIpv4ConfAllRpFilter:     1,
			ArmorNetIpv4ConfDefaultRpFilter: 1,
		},
	}

	// Гарантируем транзакционное создание всей цепочки директорий хоста
	configDir := filepath.Dir(configPath)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		log.Fatalf("[КРИТИЧЕСКИЙ СБОЙ СЕРВЕРА]: Не удалось создать директорию конфигурации %s: %v", configDir, err)
	}

	// Маршалинг структуры в красивый текстовый JSON-формат
	jsonData, err := json.MarshalIndent(defaultConfig, "", "  ")
	if err != nil {
		log.Fatalf("[ОШИБКА МАРШАЛИНГА]: Сбой генерации эталонного JSON: %v", err)
	}

	// Применяем стандарт безопасной фиксации на диск (POSIX Atomic Write)
	tmpFile := configPath + ".tmp"
	if err := os.WriteFile(tmpFile, jsonData, 0644); err != nil {
		log.Fatalf("[ОШИБКА ЗАПИСИ]: Не удалось сохранить временный файл конфигурации: %v", err)
	}
	// Атомарно переименовываем файл, исключая состояние гонки (Race Condition)
	if err := os.Rename(tmpFile, configPath); err != nil {
		log.Fatalf("[ОШИБКА АТОМАРНОГО ПЕРЕИМЕНОВАНИЯ]: Сбой фиксации config.json: %v", err)
	}
}

// ============================================================================
// R-6 (v8.2.3): ПРЕДОХРАНИТЕЛЬ ЕДИНСТВЕННОГО ЭКЗЕМПЛЯРА ДЕМОН-РЕЖИМОВ
// ============================================================================
// singletonLocks держит открытые fd блокировок на весь жизненный цикл процесса.
// Ссылка обязана жить вечно: если *os.File станет недостижимым,
// финализатор GC закроет fd — и flock исчезнет посреди работы демона.
var singletonLocks []*os.File

// acquireSingletonLock — предохранитель единственного экземпляра демон-режима (R-6).
//
// Захватывает неблокирующий эксклюзивный flock на <DirRun>/<role>.lock.
//   - удалось  → fd сохраняется глобально и живёт до выхода процесса;
//   - не удалось (EWOULDBLOCK) → роль уже выполняется другим процессом:
//     пишем ALERT в журнал, печатаем причину в stderr и выходим с кодом 1.
//
// Почему flock, а не pidfile: ядро освобождает блокировку автоматически
// при любом завершении, включая kill -9 и panic — мёртвых локов не бывает.
// Почему файл не удаляется при выходе: удаление создаёт гонку двух inode,
// при которой два процесса «оба единственные». Источник правды — flock на inode.
func acquireSingletonLock(role string) {
	// Lock-файл живёт в оперативной памяти /var/run/amberguard (tmpfs).
	lockPath := filepath.Join(config.DirRun, role+".lock")
	// tmpfs-директория может отсутствовать после ребута — воссоздаём.
	_ = os.MkdirAll(config.DirRun, 0755)

	// Открываем/создаём lock-файл с правами только для владельца.
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		// Не можем даже открыть lock-файл — не рискуем стартовать дублем вслепую.
		fmt.Fprintf(os.Stderr, "AGL: сбой открытия lock-файла %s: %v\n", lockPath, err)
		os.Exit(1)
	}

	// Неблокирующий эксклюзивный захват. EWOULDBLOCK = второй экземпляр уже жив.
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		sys.LogAction("AGL_CORE", "SINGLETON_LOCK_BUSY", "ALERT",
			fmt.Sprintf("event=singleton.lock_busy role=%s pid=%d. Второй экземпляр демон-режима отклонён.", role, os.Getpid()))
		fmt.Fprintf(os.Stderr, "AGL: роль [%s] уже выполняется другим процессом. Запуск отклонён предохранителем R-6.\n", role)
		os.Exit(1)
	}

	// Успех: fd уходит в глобальную ссылку, блокировка живёт до выхода процесса.
	singletonLocks = append(singletonLocks, f)
	sys.LogAction("AGL_CORE", "SINGLETON_LOCK_HELD", "SUCCESS",
		fmt.Sprintf("event=singleton.lock_held role=%s pid=%d. Экземпляр демон-режима единственный.", role, os.Getpid()))
}

// ============================================================================
// ТОЧКА ВХОДА
// ============================================================================
// daemonMode — процесс запущен в демон-режиме (--tarpit-daemon / --watcher).
// v8.3.0 (5.4): заполняется после flag.Parse(); используется
// checkAndInstallGeoTool() для запрета интерактива в демонах:
// у демона нет терминала админа, запрос [Y/n] там = зависший процесс.
var daemonMode bool

func main() {
	// 5.3: root-проверка стоит ПЕРВОЙ, до любых других операций,
	// включая flag.Parse и --version. Порядок диктует аудит.
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "Amber Guard Laboratory requires root privileges.")
		os.Exit(1)
	}
	// v8.3.0 (5.2): декларативные флаги вместо ручного парсинга os.Args.
	// Пакет flag принимает и -flag, и --flag, поэтому systemd-юниты
	// с `agl --tarpit-daemon` и `agl --watcher` продолжают работать без правок.
	// TUI = запуск без флагов: flag.Args() игнорируем, как раньше игнорировали хвост os.Args.
	tarpitDaemon := flag.Bool("tarpit-daemon", false, "Запуск демона ловушек")
	watcher := flag.Bool("watcher", false, "Запуск наблюдателя простоя")
	version := flag.Bool("version", false, "Показать версию и выйти")
	info := flag.Bool("info", false, "Показать служебную информацию")
	// v8.3.0 (6.3): разовый переопределитель языка. Не пишется в config.json (А.1).
	// Пустая строка = «не задан», тогда идём дальше по приоритетам.
	langFlag := flag.String("lang", "", "Язык интерфейса на этот запуск (ru|en). Не сохраняется.")
	flag.Parse()

	// --version: человеко-читаемое доказательство ldflags-инъекции (5.1).
	// Технический вывод вне i18n (прецедент: собственный сниппет аудита 5.3).
	if *version {
		// v8.3.0 (5.2-fix): брендовое правило имени — только полное слово
		// или усечение с точкой. Усечённое "Lab" без точки запрещено.
		fmt.Printf("Amber Guard Laboratory %s (build %s)\n", buildinfo.Version, buildinfo.BuildTime)
		os.Exit(0)
	}
	// --info: служебная геометрия из констант config/paths.go.
	// Загрузка config.json НЕ требуется: пути — compile-time константы,
	// поэтому --info безопасен даже при отсутствующем конфиге.
	if *info {
		fmt.Printf("Amber Guard Laboratory %s (build %s)\n", buildinfo.Version, buildinfo.BuildTime)
		fmt.Printf("Бинарник:   %s\n", config.FileBinary)
		fmt.Printf("Конфиг:     %s\n", config.FileConfig)
		fmt.Printf("Журнал:     %s\n", config.FileMainLog)
		fmt.Printf("Состояния:  %s\n", config.DirLib)
		fmt.Printf("Runtime:    %s\n", config.DirRun)
		fmt.Printf("Shm-счётчик: %s\n", config.FileShm)
		fmt.Println("Юниты:      agld_tarpit.service, agld_watcher.service")
		os.Exit(0)
	}
	// =========================================================================
	//   ЭТАП 0: ИНИЦИАЛИЗАЦИЯ СИСТЕМНЫХ ПРЕДОХРАНИТЕЛЕЙ И ОКРУЖЕНИЯ
	// =========================================================================

	// Автоматически проверяем и разворачиваем промышленный конфиг logrotate
	if err := sys.EnsureLogrotate(); err != nil {
		// Не падаем, но выводим предупреждение в stderr, так как это критично для ИБ
		fmt.Fprintf(os.Stderr, "[WARN] Failed to setup logrotate sub-system: %v\n", err)
	}

	// Инициализация дефолтного лог-файла (изолированная папка бренда)
	// БЛОК 4.4: Используем константу из config/paths.go
	appLogPath := config.FileMainLog
	_ = os.MkdirAll(filepath.Dir(appLogPath), 0755)
	isNewLog := false
	if _, err := os.Stat(appLogPath); os.IsNotExist(err) {
		isNewLog = true
	}
	logFile, err := os.OpenFile(appLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		log.SetOutput(logFile) // Теперь стандартный log пишет в /var/log/amberguard/amberguard.log
		if isNewLog {
			log.Printf("[%s] [INFO] Системный журнал Amber Guard Lab. успешно инициализирован.\n",
				time.Now().Format("2006-01-02 15:04:05"))
		}
	}

	// =========================================================================
	//   ЭТАП 1: МОМЕНТАЛЬНАЯ АВТОНОМНАЯ СТАБИЛИЗАЦИЯ ДИРЕКТОРИИ
	// =========================================================================
	execPath, err := os.Executable()
	if err == nil {
		_ = os.Chdir(filepath.Dir(execPath))
	}

	// СТРОГО ДЕТЕРМИНИРОВАННЫЙ ГЛОБАЛЬНЫЙ ПУТЬ КОНФИГУРАЦИИ БРЕНДА
	// Соответствует мировому стандарту Filesystem Hierarchy Standard (FHS).
	// БЛОК 4.4: Используем константу из config/paths.go
	configPath := config.FileConfig

	// Контур автоматического порождения (Generative Bootstrapping)
	// Проверяем, существует ли файл до вызова бутстрапа для вывода Аларма
	isConfigRecreated := false
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		isConfigRecreated = true
		log.Printf("[%s] [ALERT] Файл config.json не был обнаружен на хосте! Запущена регенерация.",
			time.Now().Format("2006-01-02 15:04:05"))
	}
	ensureDefaultConfig(configPath)

	// =========================================================================
	//   ЭТАП 2: РЕЖИМ ДЕМОНА ЛОВУШЕК (--tarpit-daemon)
	// =========================================================================
	//
	// Если процесс запущен с флагом `--tarpit-daemon`, он работает как
	// фоновый демон, управляющий ВСЕМИ ловушками (порт 22 + мульти-ловушки).
	//
	// КЛЮЧЕВОЕ ИЗМЕНЕНИЕ v8.0.1:
	//   Демон теперь управляет ВСЕМИ ловушками через единый `StartManager()`.
	//   Базовая ловушка порта 22 — это «нулевой воркер» с флагом `is_core`.
	//   Отдельный `StartTarpitCoreListener()` БОЛЬШЕ НЕ ВЫЗЫВАЕТСЯ.
	//
	// ОБРАБОТКА СИГНАЛОВ:
	//   При получении SIGINT или SIGTERM демон корректно завершает все
	//   воркеры, удаляет маркеры и очищает IPC-память.
	// =========================================================================
	// v8.3.0 (5.4): фиксируем демон-режим ДО ветвления,
	// чтобы checkAndInstallGeoTool мог отказаться от интерактива.
	daemonMode = *tarpitDaemon || *watcher

	if *tarpitDaemon {
		// v8.2.3 (R-6): предохранитель единственного экземпляра демона.
		// Ставится ДО любых побочных эффектов: второй экземпляр обязан умереть,
		// не тронув /dev/shm (InitShmMaster) и лог-конвейеры.
		acquireSingletonLock("agld_tarpit")
		if err := config.LoadConfig(configPath); err != nil {
			os.Exit(1)
		}
		// v8.3.0 (6.3): демон тоже пишет локализованные описания событий через TrEvent.
		// Приоритет: флаг → конфиг → env → "en" (совпадает с TUI).
		daemonLang := config.C.Lang
		if *langFlag != "" {
			daemonLang = *langFlag
		} else if envLang := os.Getenv("AGL_LANG"); envLang != "" {
			daemonLang = envLang
		}
		if daemonLang != "ru" && daemonLang != "en" {
			daemonLang = "en"
		}
		i18n.InitLanguage(daemonLang)
		// Инициализация межпроцессного счётчика ботов в режиме МАСТЕРА.
		// Демон создаёт файл `/dev/shm/amberguard.counter` и записывает
		// в него количество удерживаемых ботов через атомарные операции.
		// ОБНОВЛЕНО В v8.0.1.
		tarpit.InitShmMaster()

		// Запуск централизованных лог-конвейеров демона.
		// Эти горутины читают из каналов и пишут в лог-файлы на диск.
		// Только демон пишет логи. TUI только читает файлы.
		// ОБНОВЛЕНО В v8.0.1.
		go tarpit.ConnectionLogWriter(config.C.TarpitConnLog)
		go tarpit.ReportLogWriter(config.C.TarpitRptLog)

		// Создание контекста с обработкой сигналов для корректного завершения.
		// Когда демон получает SIGINT или SIGTERM, контекст отменяется,
		// и `StartManager()` корректно останавливает все воркеры.
		// ОБНОВЛЕНО В v8.0.1.
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			sig := <-sigChan
			sys.LogAction("TARPIT_MANAGER", "SHUTDOWN_SIGNAL", "PENDING",
				fmt.Sprintf("Демон получил сигнал %v. Инициировано корректное завершение.", sig))
			cancel()
		}()

		// ИБ-АУДИТ: Фиксируем старт демона
		sys.LogAction("TARPIT_MANAGER", "DAEMON_START", "SUCCESS",
			"Демон ловушек запущен. Инициализация Reconciliation Loop.")

		// Запуск менеджера ловушек (БЛОКИРУЮЩИЙ вызов).
		// Менеджер читает `tarpit_inventory.json` и управляет ВСЕМИ воркерами.
		// Он не возвращает управление до тех пор, пока контекст не будет отменён.
		// ОБНОВЛЕНО В v8.0.1.
		tarpit.StartManager(ctx)

		// Очистка после остановки менеджера.
		// Удаляем IPC-память и логируем завершение.
		// ОБНОВЛЕНО В v8.0.1.
		tarpit.RemoveShm()
		sys.LogAction("TARPIT_MANAGER", "DAEMON_STOPPED", "SUCCESS",
			"Демон ловушек корректно завершён. Все воркеры остановлены.")
		return
	}

	// =========================================================================
	//
	//	ЭТАП: Режим наблюдателя простоя администратора (--watcher)
	//
	// =========================================================================
	//
	// Если процесс запущен с флагом `--watcher`, он работает как
	// 60-сек тикер для отслеживания простоя администратора.
	// Управляется юнитом `agld_watcher.service`.
	// Старт при создании маркера `agl_ssh_moved`, стоп при удалении.
	//
	// НОВОЕ В v8.1.2 (M2).
	// =========================================================================
	if *watcher {
		// v8.2.3 (R-6): предохранитель единственного экземпляра вотчера.
		// Ставится ДО старта веб-сервера спасения: второй экземпляр
		// не должен даже пытаться биндить 55111 и двойнить счёт простоя.
		acquireSingletonLock("agld_watcher")
		if err := config.LoadConfig(configPath); err != nil {
			os.Exit(1)
		}
		// v8.3.0 (6.3): вотчер тоже пишет логи — язык для TrEvent.
		watcherLang := config.C.Lang
		if *langFlag != "" {
			watcherLang = *langFlag
		} else if envLang := os.Getenv("AGL_LANG"); envLang != "" {
			watcherLang = envLang
		}
		if watcherLang != "ru" && watcherLang != "en" {
			watcherLang = "en"
		}
		i18n.InitLanguage(watcherLang)
		// ИБ-АУДИТ: Фиксируем старт наблюдателя
		sys.LogAction("WATCHER", "PROCESS_START", "SUCCESS",
			"Процесс наблюдателя простоя администратора запущен.")
		// Запуск 60-сек тикера (БЛОКИРУЮЩИЙ вызов)
		armor.StartWatcherProtocol()
		return
	}

	// =========================================================================
	//   ЭТАП 3: ШТАТНЫЙ ЗАПУСК ИНТЕРФЕЙСНОГО ПУЛЬТА СЕРВЕРА (РЕЖИМ TUI)
	// =========================================================================
	//
	// КЛЮЧЕВОЕ ИЗМЕНЕНИЕ v8.0.1:
	//   В режиме TUI больше НЕ запускаются:
	//   - `StartTarpitCoreListener()` — базовая ловушка порта 22
	//   - `StartTarpitManager()` — менеджер мульти-ловушек
	//   - `ConnectionLogWriter()` / `ReportLogWriter()` — лог-конвейеры
	//
	//   Все ловушки управляются ИСКЛЮЧИТЕЛЬНО демоном через `tarpitctl`.
	//   TUI только отображает статус и вызывает `tarpitctl` для управления.
	//
	//   Это устраняет Проблему 2: при выходе из TUI ловушки продолжают
	//   работать, потому что они живут в демоне, а не в процессе TUI.
	// =========================================================================
	if err := config.LoadConfig(configPath); err != nil {
		log.Fatalf("Критическая ошибка загрузки верифицированной конфигурации: %v", err)
	}

	// =====================================================================
	// ФИКС ПРОБЛЕМЫ 2 (КОРЕНЬ А): УБРАН преждевременный асинхронный запуск.
	//
	// Раньше здесь было:
	//   go tarpit.StartTarpitCoreListener(context.Background())
	//
	// Это создавало ГОНКУ: к моменту, когда `ReconcileSystemStates()`
	// проверял порт 22 через `net.Listen`, горутина могла ещё не успеть
	// занять сокет. Результат: порт видится свободным → маркер удаляется
	// → ловушка «выключена».
	//
	// ТЕПЕРЬ: Все ловушки управляются демоном. TUI не запускает слушателей.
	// =====================================================================

	// =====================================================================
	// ФИКС ПРОБЛЕМЫ 2: УБРАН запуск менеджера в режиме TUI.
	//
	// Раньше здесь было:
	//   go tarpit.StartTarpitManager()
	//
	// Это приводило к тому, что при выходе из TUI менеджер умирал вместе
	// с процессом, и все мульти-ловушки останавливались.
	//
	// ТЕПЕРЬ: Менеджер живёт в демоне. TUI не запускает менеджер.
	// =====================================================================

	// =====================================================================
	// ФИКС ПРОБЛЕМЫ 2: УБРАН запуск лог-конвейеров в режиме TUI.
	//
	// Раньше здесь было:
	//   go tarpit.ConnectionLogWriter(connLog)
	//   go tarpit.ReportLogWriter(rptLog)
	//
	// В новой архитектуре логи пишет ТОЛЬКО демон. TUI только читает
	// лог-файлы для отображения. Запуск лог-конвейеров в TUI привёл бы
	// к дублированию записей.
	// =====================================================================

	// Инициализация межпроцессного счётчика ботов в режиме СЛЕЙВА.
	// Подключает TUI к файлу `/dev/shm/amberguard.counter`, созданному
	// демоном. Если демон не запущен, `InitShmSlave()` молча выходит,
	// и `GetLiveBotsShmCount()` использует резервное сканирование.
	// ОБНОВЛЕНО В v8.0.1.
	tarpit.InitShmSlave()

	// ИСПРАВЛЕНИЕ: Сначала выводим КРИТИЧЕСКИЙ АЛАРМ, если файл был затёрт!
	// Это остановит выполнение, и админ точно увидит предупреждение.
	if isConfigRecreated {
		showAlertScreen(configPath)
	}

	// ИСПРАВЛЕНИЕ: Только после подтверждения аларма проверяем и ставим geo_tool
	checkAndInstallGeoTool()

	// =========================================================================
	// ФИКС ПРОБЛЕМЫ 2 (КОРЕНЬ Б): Синхронная реанимация состояний.
	// Вызывается ПОСЛЕ загрузки конфига и ДО запуска TUI.
	// Именно здесь принимается решение о проверке/запуске демона.
	// ОБНОВЛЕНО В v8.0.1: Использует инвентарь + маркеры как источник правды.
	// =========================================================================
	ReconcileSystemStates()

	// v8.2.0 (2.4): единая точка инициализации инвентаря периметра.
	// Читатели (TUI, сверки) больше не создают файл как побочный эффект.
	_ = config.InitInventory()

	// v8.3.0 (6.3): вычисление активного языка по приоритетам А.1:
	// флаг --lang → config.json → переменная окружения AGL_LANG → "en".
	activeLang := ""
	switch {
	case *langFlag != "":
		activeLang = *langFlag
	case config.C.Lang != "":
		activeLang = config.C.Lang
	case os.Getenv("AGL_LANG") != "":
		activeLang = os.Getenv("AGL_LANG")
	default:
		activeLang = "en"
	}
	// Жёсткая валидация: любой не-ru/en → "en"
	if activeLang != "ru" && activeLang != "en" {
		activeLang = "en"
	}
	i18n.InitLanguage(activeLang)

	// Запуск интерактивной панели управления (блокирующий вызов)
	// Когда пользователь выходит из TUI, управление возвращается сюда.
	// Демон продолжает работать независимо от TUI.
	// ОБНОВЛЕНО В v8.0.1.
	tui.StartTarpitControlPanel()

	// После выхода из TUI процесс завершается.
	// Демон продолжает работать как отдельный systemd-сервис.
	// Ловушки НЕ останавливаются при выходе из TUI.
	// ОБНОВЛЕНО В v8.0.1.
	log.Println("[INFO] Выполнен выход из TUI-панели.")
}

// ============================================================================
// РЕАНИМАЦИЯ СОСТОЯНИЙ ПРИ СТАРТЕ
// ============================================================================

// ReconcileSystemStates осуществляет глубокое сканирование и реанимацию
// состояний при старте. Вызывается ОДИН раз в `main()` после загрузки конфига.
//
// АРХИТЕКТУРНОЕ РЕШЕНИЕ (ФИКС ПРОБЛЕМЫ 2):
//   - Источник правды = инвентарь `tarpit_inventory.json` + маркеры на диске.
//   - НЕ используется `net.Listen("tcp", ":22")` как источник правды.
//   - НЕ запускается `StartTarpitCoreListener()` из этой функции.
//   - Проверка и запуск демона делегируется `tarpitctl.EnsureDaemonRunning()`.
//
// АЛГОРИТМ:
//  1. Аудит рокировки портов (кастлинг) — без изменений
//  2. Аудит статуса демона через инвентарь
//  3. Если есть активные ловушки, но демон мёртв → запускаем демон (Self-Healing)
//
// ОБНОВЛЕНО В v8.0.1: Полная перепись функции.
// Убраны импорты `net` и `agl/internal/armor`.
func ReconcileSystemStates() {
	// Персистентные маркеры геометрии сервера
	// БЛОК 4.4: Используем константу из config/paths.go (убран лишний пробел в пути!)
	markerCastling := config.MarkerSshMoved

	// M4-гигиена (v8.1.3): вход в панель логируется ВСЕГДА,
	// независимо от наличия активных ловушек.
	sys.LogAction("RECONCILE", "SESSION_START", "INFO",
		"Выполнен вход в TUI-панель.")

	// -------------------------------------------------------------
	// КОНТУР 1: АУДИТ РОКИРОВКИ ПОРТОВ OpenSSH (без изменений)
	// Проверяем, совпадает ли реальный порт из /etc/ssh/sshd_config
	// с целевым скрытым портом из конфига.
	// -------------------------------------------------------------
	activeSshPort := sys.DetectActiveSshPort()
	targetRealPortStr := fmt.Sprintf("%d", config.C.RealSshPort)

	if activeSshPort == targetRealPortStr {
		// Порт совпадает → рокировка активна → маркер должен существовать
		if _, err := os.Stat(markerCastling); os.IsNotExist(err) {
			_ = os.MkdirAll(filepath.Dir(markerCastling), 0755)
			_ = sys.WriteAtomic(markerCastling, []byte(activeSshPort), 0644)
			sys.LogAction("RECONCILE", "CASTLING_MARKER_RESTORE", "SUCCESS",
				fmt.Sprintf("Восстановлен маркер рокировки: порт %s", activeSshPort))
		}
	} else {
		// Порт не совпадает → рокировка неактивна → маркер должен быть удалён
		if _, err := os.Stat(markerCastling); err == nil {
			_ = os.Remove(markerCastling)
			sys.LogAction("RECONCILE", "CASTLING_MARKER_PURGE", "SUCCESS",
				"Маркер рокировки удалён: рокировка неактивна")
		}
	}

	// -------------------------------------------------------------
	// КОНТУР 2: АУДИТ СТАТУСА ДЕМОНА
	//
	// ФИКС ПРОБЛЕМЫ 2 (КОРЕНЬ Б): Полный отказ от нестабильной проверки
	// через `net.Listen("tcp", ":22")`. Инвентарь = источник правды.
	//
	// Раньше было:
	//   ln, errRawSocket := net.Listen("tcp", ":22")
	//   if errRawSocket == nil { ... порт свободен ... }
	//
	// Проблема: горутина могла не успеть занять порт → ложный результат.
	//
	// Теперь: проверяем инвентарь и маркеры. Если есть активные ловушки
	// в инвентаре, но демон мёртв → запускаем демон (Self-Healing).
	// -------------------------------------------------------------

	// Загружаем декларативный инвентарь ловушек
	inv, err := sys.LoadTarpitInventory(sys.TarpitInventoryPath)
	if err != nil {
		// Если файла нет — это нормально для первого старта.
		// Ловушки ещё не настроены.
		sys.LogAction("RECONCILE", "INVENTORY_NOT_FOUND", "INFO",
			"Инвентарь ловушек не найден. Первый старт или ловушки не настроены.")
		return
	}

	// Подсчитываем количество активных ловушек в инвентаре
	activeCount := sys.CountActiveTraps(inv)

	if activeCount > 0 {
		// =============================================================
		// ЕСТЬ АКТИВНЫЕ ЛОВУШКИ → Убеждаемся, что демон запущен.
		// `EnsureDaemonRunning()` проверит статус и запустит демон,
		// если он не жив. Если демон уже жив — ничего не сделает.
		// =============================================================
		if err := tarpitctl.EnsureDaemonRunning(); err != nil {
			sys.LogAction("RECONCILE", "DAEMON_START_FAIL", "ERROR",
				fmt.Sprintf("Сбой обеспечения работы демона: %v", err))
		} else {
			sys.LogAction("RECONCILE", "DAEMON_VERIFIED", "SUCCESS",
				fmt.Sprintf("Демон проверен/запущен. Активных ловушек: %d", activeCount))
		}
	} else {
		// =============================================================
		// НЕТ АКТИВНЫХ ЛОВУШЕК → Демон должен быть остановлен.
		// Демон самостоятельно останавливается через `StopDaemonIfIdle()`
		// в `tarpitctl`, когда последняя ловушка деактивируется.
		// Здесь мы ничего не делаем.
		// =============================================================
		sys.LogAction("RECONCILE", "NO_ACTIVE_TRAPS", "INFO",
			"Активных ловушек в инвентаре нет — демон в режиме ожидания.")
	}
	// -------------------------------------------------------------
	// КОНТУР 3: СВЕРКА БРОНИ (M4 / K-23)
	// Проверяем, совпадают ли квитанции брони с реальными правилами
	// в брандмауэре. Восстанавливаем отсутствующие правила.
	// -------------------------------------------------------------
	ReconcileArmorStates()
}

// ============================================================================
// ВСПОМОГАТЕЛЬНЫЕ ФУНКЦИИ
// ============================================================================

// checkAndInstallGeoTool проверяет наличие `geo_tool` и предлагает установку.
// Вызывается при старте, после проверки аларма конфигурации.
// Если утилита не найдена, предлагает установить пакет `geoip-bin`.
func checkAndInstallGeoTool() {
	// v8.3.0 (5.4): демон-режим не имеет терминала админа.
	// Интерактивный запрос [Y/n] там невозможен: процесс зависнет на stdin.
	// В демонах не предлагаем установку: логируем WARNING и продолжаем
	// без геолокации (страны в логах будут пустыми — приемлемая деградация).
	// В текущей архитектуре демоны выходят из main() раньше этого вызова;
	// предохранитель защищает от будущих рефакторингов порядка вызовов.
	if daemonMode {
		sys.LogAction("GEO_INSTALL", "GEO_SKIP_DAEMON", "WARNING",
			"event=geo.install_skipped reason=daemon_mode. Интерактивная установка гео-инструмента в демон-режиме недоступна; продолжаем без геолокации.")
		return
	}
	geoToolName := "geoiplookup"
	if config.C.GeoTool != "" {
		geoToolName = config.C.GeoTool
	}

	// Проверяем, существует ли утилита в системе
	if _, err := exec.LookPath(geoToolName); err == nil {
		return // Утилита на месте — ничего не делаем
	}

	log.Printf("[%s] [WARNING] Компонент гео-аналитики '%s' не обнаружен.",
		time.Now().Format("2006-01-02 15:04:05"), geoToolName)

	// Очищаем экран для диалога
	fmt.Print("\033[H\033[2J")
	fmt.Println("⚠️  ВНИМАНИЕ: Компонент гео-аналитики (geo_tool) не найден!")
	fmt.Printf("В системе отсутствует утилита '%s', из-за чего страны в логах будут пустыми.\n\n", geoToolName)
	fmt.Print("Хотите автоматически установить необходимый системный пакет (geoip-bin)? [Y/n]: ")

	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(strings.ToLower(input))

	if input == "" || input == "y" || input == "yes" {
		fmt.Println("\n[1/2] Обновление списков пакетов (apt-get update)...")
		// БЛОК 3.1: apt-get update больше не слепой и не бесконечный.
		// Таймаут 300 сек (медленные зеркала), вывод копится в буферах —
		// он и до правки не показывался в консоль.
		// Сбой обновления НЕ фатален: установка попытается по текущим спискам.
		updateRes := sys.RunTimeout(300*time.Second, "apt-get", "update")
		if updateRes.Err != nil {
			// Машинное событие в журнал AGL: списки не обновились.
			sys.LogAction("GEO_INSTALL", "APT_UPDATE_FAIL", "WARNING",
				fmt.Sprintf("event=apt.update_failed err=%v stderr=%s",
					updateRes.Err, strings.TrimSpace(updateRes.Stderr)))
			// Консольное сообщение админу: продолжаем без свежих списков.
			fmt.Println("⚠️ Не удалось обновить списки пакетов. Пробую установку по текущим спискам...")
		}

		fmt.Println("\n[2/2] Установка пакета geoip-bin и баз данных...")
		// БЛОК 3.1: установка с таймаутом 600 сек и ЖИВЫМ стримингом вывода
		// в консоль — админ видит прогресс, как и до правки.
		// Прямой exec.Command уходит из main.go; остаётся только exec.LookPath
		// (он не порождает процесс, лишь ищет утилиту в PATH).
		if err := sys.RunTimeoutStreamed(600*time.Second, "apt-get", "install", "-y", "geoip-bin", "geoip-database"); err != nil {
			// Машинное событие в журнал AGL: установка пакетов провалилась.
			sys.LogAction("GEO_INSTALL", "APT_INSTALL_FAIL", "ERROR",
				fmt.Sprintf("event=apt.install_failed err=%v", err))
			log.Printf("[%s] [ERROR] Сбой установки geoip-bin: %v",
				time.Now().Format("2006-01-02 15:04:05"), err)
			fmt.Printf("\n❌ Ошибка установки: %v. Запуск в ограниченном режиме...\n", err)
			time.Sleep(3 * time.Second)
		} else {
			// Машинное событие: пакеты встали успешно.
			sys.LogAction("GEO_INSTALL", "APT_INSTALL_OK", "SUCCESS",
				"event=apt.install_success packages=geoip-bin,geoip-database")
			log.Printf("[%s] [INFO] Пакет geoip-bin успешно установлен.",
				time.Now().Format("2006-01-02 15:04:05"))
			fmt.Println("\n✅ Установка завершена успешно!")
			time.Sleep(1 * time.Second)
		}
	} else {
		log.Printf("[%s] [WARNING] Администратор отклонил установку GeoIP.",
			time.Now().Format("2006-01-02 15:04:05"))
		fmt.Println("\nУстановка отменена. Модули гео-аналитики отключены.")
		time.Sleep(2 * time.Second)
	}
}

// showAlertScreen выводит предупреждение о нарушении целостности конфигурации.
// Вызывается если файл `config.json` был регенерирован при старте.
// Блокирует выполнение до нажатия ENTER.
func showAlertScreen(configPath string) {
	fmt.Print("\033[H\033[2J")
	fmt.Println("\033[31m" + strings.Repeat("=", 65))
	fmt.Println("⚠️  ВНИМАНИЕ: НАРУШЕНА ЦЕЛОСТНОСТЬ СИСТЕМЫ КОНФИГУРАЦИИ!")
	fmt.Println(strings.Repeat("=", 65))
	fmt.Printf("Файл конфигурации '%s' отсутствовал на сервере\n", configPath)
	fmt.Println("и был автоматически регенерирован с заводскими настройками.")
	fmt.Println()
	fmt.Println("Пожалуйста, проверьте порты Дегтярной ямы и лимиты сетевой брони!")
	fmt.Println("Данный инцидент безопасности зафиксирован в журнале /var/log/amberguard/amberguard.log.")
	fmt.Println(strings.Repeat("=", 65))
	fmt.Print("Нажмите [ ENTER ] для подтверждения и входа в панель управления...\033[0m  ")
	_, _ = bufio.NewReader(os.Stdin).ReadBytes('\n')
}

// ============================================================================
// M4 (K-23): ARMOR RECONCILE LOOP — СВЕРКА БРОНИ ПРИ СТАРТЕ TUI
// ============================================================================
// ReconcileArmorStates сверяет состояние брони (квитанции в `states`)
// с реальным состоянием правил в брандмауэре (через `ufw status`).
//
// АЛГОРИТМ:
//  1. Читаем инвентарь периметра (`perimeter_inventory.json`)
//  2. Для каждого `detected_apps` с портом > 0:
//     - Проверяем наличие квитанции: `/var/lib/amberguard/states/agl_armor_active_port.N`
//     - Если квитанция ЕСТЬ → проверяем наличие правила через `VerifyUfwRulePresence()`
//     - Если правила НЕТ → восстанавливаем через `armor.ExecuteArmorRoute("port_auto_N", "ON")`
//     - Если квитанции НЕТ → проверяем наличие правила через `VerifyUfwRulePresence()`
//     - Если правило ЕСТЬ → только WARNING в лог (restore-only, без удаления правила)
//
// ИБ-ПРИНЦИП: Контур сверки ТОЛЬКО восстанавливает, НЕ удаляет правила.
// Это исключает риск случайного уничтожения правил, созданных вручную админом.
//
// НОВОЕ В v8.1.2 (M4 / K-23): Броня теперь сверяется при старте TUI.
// Раньше броня не сверялась, что приводило к рассинхронизации после ребута.
func ReconcileArmorStates() {
	// Загружаем инвентарь периметра
	apps, err := config.LoadDetectedApps()
	if err != nil {
		sys.LogAction("RECONCILE_ARMOR", "INVENTORY_LOAD_FAIL", "WARNING",
			fmt.Sprintf("Не удалось загрузить инвентарь периметра для сверки брони: %v", err))
		return
	}

	recoveredCount := 0
	warningCount := 0

	for _, app := range apps {
		if app.Port <= 0 {
			continue
		}

		// M4-гигиена (v8.1.3): канал спасения вне автовосстановления брони.
		if app.Port == config.C.WebRescuePort {
			continue
		}

		// Путь к квитанции брони
		// БЛОК 4.4: Используем генератор путей из config/paths.go
		receiptPath := config.ArmorMarkerPath(app.Port)
		receiptExists := false
		if _, errStat := os.Stat(receiptPath); errStat == nil {
			receiptExists = true
		}

		// Сигнатура правила в брандмауэре
		commentCheck := fmt.Sprintf("Amber Guard Lab. DDoS Rate Limit Port %d", app.Port)
		ruleExists := armor.VerifyUfwRulePresence(commentCheck, true)

		// M4-гигиена (v8.1.3): порт с активной ловушкой — вне юрисдикции M4 (C1).
		// Броня на капкане противоестественна: limit троттлит ботов, которых
		// яма обязана держать. Не восстанавливаем даже при живой квитанции.
		// БЛОК 4.4: Используем генератор путей из config/paths.go
		tarpitMarker := config.TarpitMarkerPath(app.Port)
		if _, errT := os.Stat(tarpitMarker); errT == nil {
			// СЦЕНАРИЙ 3 (C1-санитар): трап активен + любое лимит-правило →
			// зачищаем безусловно. Контракт C1 абсолютен: на порту с ловушкой
			// не должно быть лимитов, независимо от наличия квитанции брони.
			// Лимит троттлит ботов, которых яма обязана держать — это противоестественно.
			if ruleExists {
				armor.PurgeLimitRule(app.Port)
				sys.LogAction("RECONCILE_ARMOR", "C1_CLEANUP", "SUCCESS",
					fmt.Sprintf("event=c1.cleanup port=%d Лимит на порту-ловушке удалён: капкан должен оставаться доступным.", app.Port))
			}
			continue
		}

		if receiptExists && !ruleExists {
			// =============================================================
			// СЦЕНАРИЙ 1: Квитанция ЕСТЬ, правила НЕТ → восстанавливаем
			// Это происходит после ребута или после ручного сброса правил.
			// =============================================================
			sys.LogAction("RECONCILE_ARMOR", "RULE_MISSING", "PENDING",
				fmt.Sprintf("Обнаружен рассинхрон: квитанция есть, правила нет. Порт %d (%s). Восстановление...", app.Port, app.Name))

			dynamicID := fmt.Sprintf("port_auto_%d", app.Port)
			if errAct := armor.ExecuteArmorRoute(dynamicID, "ON"); errAct != nil {
				sys.LogAction("RECONCILE_ARMOR", "RESTORE_FAIL", "ERROR",
					fmt.Sprintf("Сбой восстановления брони на порту %d: %v", app.Port, errAct))
			} else {
				recoveredCount++
				sys.LogAction("RECONCILE_ARMOR", "RULE_RESTORED", "SUCCESS",
					fmt.Sprintf("Броня на порту %d (%s) успешно восстановлена.", app.Port, app.Name))
			}
		} else if !receiptExists && ruleExists {
			// =============================================================
			// СЦЕНАРИЙ 2: Правило ЕСТЬ, квитанции НЕТ → только WARNING
			// Это может быть правило, созданное вручную админом.
			// Мы НЕ удаляем его, только логируем предупреждение.
			// =============================================================
			warningCount++
			sys.LogAction("RECONCILE_ARMOR", "RULE_WITHOUT_RECEIPT", "WARNING",
				fmt.Sprintf("Обнаружено правило брони без квитанции: порт %d (%s). Правило сохранено (ручное создание?).", app.Port, app.Name))
		}
	}

	if recoveredCount > 0 || warningCount > 0 {
		sys.LogAction("RECONCILE_ARMOR", "RECONCILE_SUMMARY", "SUCCESS",
			fmt.Sprintf("Сверка брони завершена. Восстановлено правил: %d. Предупреждений: %d.", recoveredCount, warningCount))
	}
}
