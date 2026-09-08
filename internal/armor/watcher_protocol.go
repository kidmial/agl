// internal/armor/watcher_protocol.go
package armor

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"agl/config"
	"agl/i18n"
	"agl/internal/sys"
)

const (
	// Период тикера: 60 секунд
	watcherTickInterval = 60 * time.Second
	// Путь к счётчику простоя администратора
	adminIdleCounterPath = config.FileAdminIdleCounter
	// Сигнатура для испепеления (только SIG_HIDDEN, по контракту v2)
	sigHiddenPrefix = "Amber Guard Lab. Private SSH Invisible Port"
	// ─────────────────────────────────────────────────────────────
	// M3 (v8.1.2): Канал спасения «веб-стук».
	// Путь к файлу сигнала: /dev/shm/agl_proto_web_rescue/agl_proto_web_knock.ip
	// Референс: engine_cron_watcher.sh (Shell-эра) → [АРХИВ].
	// ─────────────────────────────────────────────────────────────
	// Директория для файла веб-стука в ОЗУ
	webRescueDir = config.DirWebRescue
	// Полное имя файла веб-стука
	webKnockFile = webRescueDir + "/agl_proto_web_knock.ip"
)

var (
	sessionSeen   bool         // M3c: первый тик observed — без ложного перехода
	lastActive    bool         // M3c: последнее состояние сессии (для переходов)
	webServer     *http.Server // M3c: управляемый инстанс веб-сервера
	webServerPort int          // M3c: порт, на котором живёт текущий инстанс
	// M2-fix (v8.1.3): терминальное дремлющее состояние после испепеления.
	// Вотчер ЖИВ (шлюз слушает, стуки принимаются), но НЕМ:
	// без счёта, без церемоний, без логов — до возвращения админа или стука.
	safeClosed bool
)

// StartWatcherProtocol запускает 60-сек тикер для отслеживания простоя администратора.
// Вызывается при старте процесса с флагом `--watcher`.
// Блокирующий вызов — не возвращает управление до остановки процесса.
func StartWatcherProtocol() {
	sys.LogAction("WATCHER", "START", "SUCCESS",
		i18n.T.WatcherStarted)

	// M3 (v8.1.2): Инициализация директории веб-стука в ОЗУ
	initWebRescueDir()

	// M3c: первый тик НЕМЕДЛЕННО (сервер и гейт взводятся без ожидания 60 сек)
	watcherTick()

	ticker := time.NewTicker(watcherTickInterval)
	defer ticker.Stop()

	for range ticker.C {
		watcherTick()
	}
}

// watcherTick выполняет один цикл проверки:
// 1. Парсинг `who` для определения активных сессий
// 2. Сброс или инкремент счётчика простоя
// 3. При достижении таймаута — испепеление
func watcherTick() {
	// M3c: хот-релоад конфига — порт, пароль, таймаут и тумблер горячие.
	//
	// v8.2.3 (R-4a): слепое игнорирование ошибки (_ =) заменено на явное логирование.
	//
	// Почему это важно:
	//   - Если JSON-конфиг на диске повреждён (битый синтаксис, сбой прав),
	//     LoadConfig вернёт ошибку, а глобальный снапшот config.C останется
	//     со старыми значениями.
	//   - Молчаливое игнорирование скрывает факт деградации: вотчер
	//     продолжает работать, но админ не знает, что его правки в SETTINGS
	//     не применились в рантайм.
	//   - Явный WARNING с машинным кодом event=config.reload_failed позволяет
	//     увидеть проблему в журнале и исправить JSON.
	//
	// Поведение при ошибке (fail-safe) не меняется: вотчер продолжает работу
	// с предыдущим успешным снапшотом, чтобы не обрушить защиту из-за опечатки.
	if err := config.LoadConfig(config.DirEtc + "/config.json"); err != nil {
		sys.LogAction("WATCHER", "CONFIG_RELOAD_FAIL", "WARNING",
			fmt.Sprintf("event=config.reload_failed err=%v. "+i18n.T.WatcherConfigReloadFailedContinue, err))
	}
	// [здесь остаётся твой блок M3g PasswordJustMigrated, если он внедрён]
	// M3c: контроллер жизненного цикла веб-сервера (fail closed + ретрай)
	syncWebRescueServer()

	// ФАЗА «ОБРАБОТКА СТУКА» (п. 4.6): в оригинале идёт ПЕРЕД проверкой сессий —
	// порядок сохранён.
	handleKnockPhase()

	// ФАЗА «ПРОВЕРКА СЕССИЙ» (п. 4.6): админ активен → цикл прерывается.
	if checkSessionPhase() {
		return
	}

	// ── СЕССИИ НЕТ, ДРЕМОТА ──: полная тишина, без счёта и церемоний
	if safeClosed {
		return
	}

	// ФАЗА «ИНКРЕМЕНТ СЧЁТЧИКА» (п. 4.6).
	newCount := incrementIdlePhase()

	// ФАЗА «ИСИНЕРАЦИЯ» (п. 4.6): при превышении порога.
	incinerationPhase(newCount)
}

// handleKnockPhase — фаза «обработка стука» (п. 4.6 аудита).
// БЛОК А: входящий веб-стук — НЕЗАВИСИМО от присутствия админа
// (несколько админов: шлюз всегда активен по геному).
// M2-fix: стук будит дремлющий вотчер.
// ИЗВЛЕЧЕНО ИЗ watcherTick (v8.2.2, п. 4.6): поведение идентично оригиналу.
func handleKnockPhase() {
	if config.C.WebRescueEnabled {
		processWebKnock()
	}
	// M2-fix: стук будит дремлющий вотчер
	if _, err := os.Stat(webKnockFile); err == nil && safeClosed {
		safeClosed = false
		sys.LogAction("WATCHER", "SAFE_REOPENED", "SUCCESS",
			i18n.T.WatcherSafeReopenedByKnock)
	}
}

// checkSessionPhase — фаза «проверка сессий» (п. 4.6 аудита).
// ВОЗВРАЩАЕТ true, если сессия админа активна (watcherTick прерывает цикл).
// Edge-переходы SESSION_APPEARED / SAFE_REOPENED — только при смене состояния.
// ИЗВЛЕЧЕНО ИЗ watcherTick (v8.2.2, п. 4.6): поведение идентично оригиналу.
func checkSessionPhase() bool {
	hasActiveSession := checkActiveSessions()
	if !hasActiveSession {
		return false
	}
	// ── СЕССИЯ АКТИВНА ──
	if safeClosed {
		safeClosed = false
		sys.LogAction("WATCHER", "SAFE_REOPENED", "SUCCESS",
			i18n.T.WatcherSafeReopenedBySession)
	}
	if !sessionSeen || !lastActive {
		sessionSeen = true
		lastActive = true
		writeAdminIdleCounter(0)
		sys.LogAction("WATCHER", "SESSION_APPEARED", "SUCCESS",
			i18n.T.WatcherSessionAppeared)
	}
	return true
}

// incrementIdlePhase — фаза «инкремент счётчика» (п. 4.6 аудита).
// Вызывается ТОЛЬКО в бодрствовании (после проверки safeClosed в диспетчере) —
// переход IDLE_STARTED стоит здесь, чтобы не логировать его в дремоте.
// ИЗВЛЕЧЕНО ИЗ watcherTick (v8.2.2, п. 4.6): поведение идентично оригиналу.
func incrementIdlePhase() int {
	// ── СЕССИИ НЕТ, БОДРСТВОВАНИЕ ──
	if !sessionSeen || lastActive {
		sessionSeen = true
		lastActive = false
		sys.LogAction("WATCHER", "IDLE_STARTED", "PENDING",
			i18n.T.WatcherIdleStarted)
	}
	newCount := readAdminIdleCounter() + 1
	writeAdminIdleCounter(newCount)
	return newCount
}

// incinerationPhase — фаза «инсинерация» (п. 4.6 аудита).
// Половинное предупреждение, порог, испепеление SIG_HIDDEN,
// удаление счётчика и переход в дремоту (safeClosed=true).
// ИЗВЛЕЧЕНО ИЗ watcherTick (v8.2.2, п. 4.6): поведение идентично оригиналу.
func incinerationPhase(newCount int) {
	timeout := config.C.AdminIdleTimeout
	if timeout <= 0 {
		timeout = 10
	}
	if newCount == timeout/2 {
		sys.LogAction("WATCHER", "IDLE_HALF_WARNING", "WARNING",
			fmt.Sprintf(i18n.T.WatcherIdleHalfWarning, newCount, timeout-newCount))
	}
	if newCount >= timeout {
		sys.LogAction("WATCHER", "IDLE_TIMEOUT_REACHED", "ALERT",
			fmt.Sprintf(i18n.T.WatcherIdleTimeoutReached, timeout))
		incinerateHiddenRules() // все SIG_HIDDEN (стук + SSH_CONNECTION); SIG_LOCALE неприкосновенен
		_ = os.Remove(adminIdleCounterPath)
		safeClosed = true
		sys.LogAction("WATCHER", "SAFE_CLOSED", "SUCCESS",
			i18n.T.WatcherSafeClosed)
	}
}

// utmpRecordSize — размер одной записи /var/run/utmp (glibc, x86_64/ARM64).
const utmpRecordSize = 384

// utmpUserProcess — тип записи: активная пользовательская сессия.
const utmpUserProcess = 7

// checkActiveSessions парсит /var/run/utmp НАПРЯМУЮ, без exec `who`.
// Сессия активна, если есть запись USER_PROCESS на pts/* с внешним хостом.
// FAIL-SAFE: сбой чтения utmp трактуется как «сессия есть» — отказ датчика
// не должен запускать испепеление живого админа (геном №1).
func checkActiveSessions() bool {
	data, err := os.ReadFile("/var/run/utmp")
	if err != nil {
		sys.LogAction("WATCHER", "UTMP_READ_FAIL", "ERROR",
			fmt.Sprintf(i18n.T.WatcherUtmpReadFail, err))
		return true
	}
	for off := 0; off+utmpRecordSize <= len(data); off += utmpRecordSize {
		rec := data[off : off+utmpRecordSize]
		// Смещения glibc utmp: ut_type [0:4], ut_line [8:40], ut_host [76:332]
		if int32(binary.LittleEndian.Uint32(rec[0:4])) != utmpUserProcess {
			continue
		}
		line := utmpCString(rec[8:40])
		host := utmpCString(rec[76:332])
		if !strings.Contains(line, "pts/") {
			continue
		}
		if strings.Contains(line, "tmux") {
			continue
		}
		if host == "" || host == "127.0.0.1" || host == "localhost" || host == "::1" {
			continue
		}
		return true
	}
	return false
}

// utmpCString извлекает строку из фиксированного поля utmp до первого нуль-байта.
func utmpCString(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return strings.TrimSpace(string(b))
}

// readAdminIdleCounter читает текущее значение счётчика простоя.
// Возвращает 0, если файл не существует или повреждён.
func readAdminIdleCounter() int {
	data, err := os.ReadFile(adminIdleCounterPath)
	if err != nil {
		return 0
	}

	var count int
	_, _ = fmt.Sscanf(string(data), "%d", &count)
	return count
}

// writeAdminIdleCounter записывает значение счётчика простоя.
// Использует атомарную запись (tmp + rename) для надёжности.
func writeAdminIdleCounter(value int) {
	// Гарантируем существование директории
	_ = os.MkdirAll(config.DirLib, 0755)

	tmpPath := adminIdleCounterPath + ".tmp"
	content := fmt.Sprintf("%d", value)

	// Записываем во временный файл
	if err := os.WriteFile(tmpPath, []byte(content), 0644); err != nil {
		sys.LogAction("WATCHER", "COUNTER_WRITE_FAIL", "ERROR",
			fmt.Sprintf(i18n.T.WatcherCounterWriteFail, err))
		return
	}

	// Атомарное переименование
	if err := os.Rename(tmpPath, adminIdleCounterPath); err != nil {
		sys.LogAction("WATCHER", "COUNTER_RENAME_FAIL", "ERROR",
			fmt.Sprintf(i18n.T.WatcherCounterRenameFail, err))
	}
}

// incinerateHiddenRules испепеляет правила с сигнатурой `SIG_HIDDEN`.
// Множество испепеления = {"Amber Guard Lab. Private SSH Invisible Port"}.
// Правила `SIG_LOCALE` (127.0.0.1) НЕ трогаются — они остаются навсегда.
func incinerateHiddenRules() {
	sys.LogAction("WATCHER", "INCINERATION_START", "PENDING",
		i18n.T.WatcherIncinerationStart)

	// Парсим вывод `ufw status numbered`
	// БЛОК 3.1: получаем нумерованный список правил через единый интерфейс.
	// Stdout и Stderr разделены, ошибка структурирована.
	statusRes := sys.RunTimeout(5*time.Second, "ufw", "status", "numbered")

	// Если UFW недоступен, испепеление выполнять нельзя:
	// мы не видим правила, значит не можем безопасно удалять их по номерам.
	if statusRes.Err != nil {
		// Для диагностической строки сначала пытаемся взять stderr.
		details := strings.TrimSpace(statusRes.Stderr)

		// Если stderr пуст, но команда что-то вывела в stdout,
		// используем stdout как дополнительный источник контекста.
		if details == "" {
			details = strings.TrimSpace(statusRes.Stdout)
		}

		// Сохраняем уровень ERROR, как было, но добавляем машинный код события.
		sys.LogAction("WATCHER", "UFW_STATUS_FAIL", "ERROR",
			fmt.Sprintf("event=ufw.status_failed err=%v details=%s", statusRes.Err, details))

		// Прерываем испепеление: без списка правил удаление недопустимо.
		return
	}

	// Разбираем stdout построчно. Номера правил нужны для удаления.
	lines := strings.Split(statusRes.Stdout, "\n")
	incineratedCount := 0

	// Итерируем в обратном порядке, чтобы номера правил не съезжали при удалении
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]

		// Проверяем, что строка содержит сигнатуру `SIG_HIDDEN`
		if !strings.Contains(line, sigHiddenPrefix) {
			continue
		}

		// Извлекаем номер правила из формата "[ 12]"
		numStr := ""
		if idx := strings.Index(line, "["); idx != -1 {
			end := strings.Index(line[idx:], "]")
			if end != -1 {
				numStr = strings.TrimSpace(line[idx+1 : idx+end])
			}
		}

		if numStr != "" {
			// БЛОК 3.1: удаление правила по номеру больше не слепое.
			// Если удаление не подтверждено, правило НЕ считается испепелённым.
			delRes := sys.RunTimeout(5*time.Second, "ufw", "--force", "delete", numStr)

			// Если UFW не смог удалить правило, пишем WARNING и переходим
			// к следующей строке. Инкремент счётчика здесь недопустим.
			if delRes.Err != nil {
				// event=ufw.incinerate_delete_failed — машинный код сбоя.
				delRes.LogWarnIfErr("WATCHER", "INCINERATION", "event=ufw.incinerate_delete_failed")
				continue
			}

			// Счётчик увеличивается только после подтверждённого удаления.
			incineratedCount++

			// SUCCESS пишем только после реального успеха команды.
			sys.LogAction("WATCHER", "RULE_INCINERATED", "SUCCESS",
				fmt.Sprintf(i18n.T.WatcherRuleIncinerated, numStr))
		}
	}

	sys.LogAction("WATCHER", "INCINERATION_COMPLETE", "SUCCESS",
		fmt.Sprintf(i18n.T.WatcherIncinerationComplete, incineratedCount))
}

// ─────────────────────────────────────────────────────────────────────────────
// M3 (v8.1.2): КАНАЛ СПАСЕНИЯ «ВЕБ-СТУК»
// ─────────────────────────────────────────────────────────────────────

// initWebRescueDir инициализирует директорию файла веб-стука в ОЗУ.
// v8.2.0 (1.3): 0700 root-only. Веб-сервер живёт в том же процессе (root),
// внешний владелец не нужен. /dev/shm — tmpfs: очищается при ребуте,
// поэтому легаси-права не переживают ближайшую перезагрузку.
func initWebRescueDir() {
	_ = os.MkdirAll(webRescueDir, 0700)
	sys.LogAction("WATCHER", "WEB_RESCUE_DIR_INIT", "SUCCESS",
		fmt.Sprintf(i18n.T.WatcherWebRescueDirInit, webRescueDir))
}

// processWebKnock обрабатывает входящий сигнал веб-стука.
// Если файл сигнала существует, читает IP, валидирует его как IPv4,
// внедряет точечное правило в брандмауэр и самоуничтожает файл.
//
// АЛГОРИТМ (референс: engine_cron_watcher.sh, БЛОК А → [АРХИВ]):
//  1. Проверяем существование файла сигнала
//  2. Читаем IP-адрес из файла
//  3. Валидируем формат IPv4 через regexp
//  4. Внедряем правило `iptables -I ufw-user-input 1` (атомарно, без сброса таблиц)
//  5. Фоновая синхронизация через `ufw insert 1` (для персистентности)
//  6. Самоуничтожение файла сигнала
func processWebKnock() {
	// Шаг 1: Проверяем существование файла сигнала
	if _, err := os.Stat(webKnockFile); os.IsNotExist(err) {
		return // Файла нет — веб-стук не поступал
	}

	// Шаг 2: Читаем IP-адрес из файла
	data, err := os.ReadFile(webKnockFile)
	if err != nil {
		sys.LogAction("WATCHER", "WEB_KNOCK_READ_FAIL", "ERROR",
			fmt.Sprintf(i18n.T.WatcherWebKnockReadFail, err))
		return
	}
	adminIP := strings.TrimSpace(string(data))

	// Шаг 3: v8.2.0 (1.5): строгая валидация IPv4 через net.ParseIP
	if !isValidKnockIPv4(adminIP) {
		sys.LogAction("WATCHER", "WEB_KNOCK_INVALID_IP", "ALERT",
			fmt.Sprintf(i18n.T.WatcherWebKnockInvalidIP, adminIP))
		// Самоуничтожение невалидного файла
		_ = os.Remove(webKnockFile)
		return
	}

	// Порт берём из глобального конфига (скрытый порт SSH)
	targetPort := fmt.Sprintf("%d", config.C.RealSshPort)

	// Шаг 4: M3c «пересадка на первое место» (геном Shell-эры).
	// Удаляем существующий дубль (сигнатура + IP), чтобы ufw insert 1
	// реально поставил правило НА ПЕРВОЕ место, а не сказал "Skipping".
	sigHidden := fmt.Sprintf("Amber Guard Lab. Private SSH Invisible Port %s", targetPort)
	// БЛОК 3.1: статус UFW для дедупликации получается через единый интерфейс.
	// Если статус недоступен — дедупликацию пропускаем, но фиксируем сбой.
	statusRes := sys.RunTimeout(5*time.Second, "ufw", "status", "numbered")

	if statusRes.Err != nil {
		// Сбой чтения статуса не останавливает весь веб-стук,
		// но может привести к дублю правила. Это нужно видеть в логе.
		// event=ufw.status_failed — машинный код сбоя.
		statusRes.LogWarnIfErr("WATCHER", "WEB_KNOCK_DEDUP", "event=ufw.status_failed")
	} else {
		// Разбираем stdout построчно.
		lines := strings.Split(statusRes.Stdout, "\n")

		// Идём с конца, потому что при удалении номера правил могут съезжать.
		for i := len(lines) - 1; i >= 0; i-- {
			// Ищем старый дубль: та же сигнатура и тот же IP админа.
			if strings.Contains(lines[i], sigHidden) && strings.Contains(lines[i], adminIP) {
				// Извлекаем номер правила из строки вида "[ 12]".
				if numStr := extractRuleNumber(lines[i]); numStr != "" {
					// БЛОК 3.1: удаление дубля больше не слепое.
					delRes := sys.RunTimeout(5*time.Second, "ufw", "--force", "delete", numStr)

					// Если дубль не удалился, это не всегда фатально,
					// но может помешать вставке правила строго на позицию 1.
					// event=ufw.knock_dedup_delete_failed — машинный код сбоя.
					delRes.LogWarnIfErr("WATCHER", "WEB_KNOCK_DEDUP", "event=ufw.knock_dedup_delete_failed")
				}
			}
		}
	}

	// Шаг 5: мгновенный iptables на позицию 1 (наносекунды, без сброса таблиц).
	// БЛОК 3.1: результат команды больше не игнорируется.
	iptablesRes := sys.RunTimeout(5*time.Second, "iptables",
		"-I", "ufw-user-input", "1",
		"-s", adminIP,
		"-p", "tcp",
		"--dport", targetPort,
		"-j", "ACCEPT",
		"-m", "comment",
		"--comment", sigHidden,
	)

	// Если мгновенное правило не внедрилось, это снижает скорость спасения,
	// но ниже есть синхронный `ufw insert`, который также применяет правило.
	// Поэтому здесь достаточно WARNING.
	// event=iptables.knock_insert_failed — машинный код сбоя.
	iptablesRes.LogWarnIfErr("WATCHER", "WEB_KNOCK_IPTABLES", "event=iptables.knock_insert_failed")

	// Шаг 6: СИНХРОННЫЙ ufw insert 1 (персистентность + гарантия позиции 1).
	// БЛОК 3.1: результат команды больше не игнорируется.
	ufwRes := sys.RunTimeout(10*time.Second, "ufw",
		"insert", "1", "allow",
		"from", adminIP,
		"to", "any",
		"port", targetPort,
		"proto", "tcp",
		"comment", sigHidden,
	)

	// Если `ufw insert` не подтверждён, персистентность и позиция правила
	// не гарантированы. В этом случае файл сигнала уничтожать нельзя:
	// следующий тик вотчера повторит попытку.
	if ufwRes.Err != nil {
		// event=ufw.knock_insert_failed — машинный код сбоя.
		ufwRes.LogWarnIfErr("WATCHER", "WEB_KNOCK_UFW", "event=ufw.knock_insert_failed")

		// Повышаем до ALERT, потому что канал спасения может не пережить
		// перезагрузку/перечитывание UFW без персистентного правила.
		sys.LogAction("WATCHER", "WEB_KNOCK_PERSIST_FAIL", "ALERT",
			fmt.Sprintf("event=web_knock.ufw_persist_failed ip=%s port=%s. "+i18n.T.WatcherWebKnockPersistFailContinue, adminIP, targetPort))

		// НЕ удаляем webKnockFile: даём вотчеру шанс повторить стук.
		return
	}

	// Шаг 7: Самоуничтожение файла сигнала.
	// Выполняется только если персистентное правило подтверждено.
	_ = os.Remove(webKnockFile)

	// SUCCESS пишем после подтверждённого персистентного внедрения.
	// Если мгновенный iptables завершился ошибкой, но ufw insert успешен,
	// правило всё равно внедрено через UFW/Netfilter.
	sys.LogAction("WATCHER", "WEB_KNOCK_PROCESSED", "SUCCESS",
		fmt.Sprintf(i18n.T.WatcherWebKnockProcessed, adminIP, targetPort))
}

// syncWebRescueServer — M3c контроллер жизненного цикла веб-сервера.
// SETTINGS горячие: смена порта → рестарт + смена гейта; тумблер 0 → стоп + снятие гейта.
func syncWebRescueServer() {
	wantPort := config.C.WebRescuePort
	if wantPort <= 0 {
		wantPort = 55111
	}
	if !config.C.WebRescueEnabled {
		if webServer != nil {
			_ = webServer.Close()
			removeWebRescueGate(webServerPort)
			webServer = nil
			webServerPort = 0
			sys.LogAction("WEB_RESCUE", "SERVER_DISABLED", "SUCCESS",
				i18n.T.WatcherWebRescueServerDisabled)
		}
		return
	}
	if webServer == nil {
		ensureWebRescueGate(wantPort)
		// M3f: fail closed — при сбое TLS сервер вернёт nil, ретрай на следующем тике
		if srv := startWebRescueServer(wantPort); srv != nil {
			webServer = srv
			webServerPort = wantPort
		}
		return
	}
	if webServerPort != wantPort {
		_ = webServer.Close()
		removeWebRescueGate(webServerPort)
		ensureWebRescueGate(wantPort)
		webServer = startWebRescueServer(wantPort)
		webServerPort = wantPort
		sys.LogAction("WEB_RESCUE", "SERVER_RESTART", "SUCCESS",
			fmt.Sprintf(i18n.T.WatcherWebRescueServerRestart, wantPort))
	}
}

// extractRuleNumber извлекает номер правила из строки `ufw status numbered` ("[ 12]").
func extractRuleNumber(line string) string {
	idx := strings.Index(line, "[")
	if idx == -1 {
		return ""
	}
	end := strings.Index(line[idx:], "]")
	if end == -1 {
		return ""
	}
	return strings.TrimSpace(line[idx+1 : idx+end])
}
