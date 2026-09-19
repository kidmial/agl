// internal/armor/watcher_protocol.go
package armor

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net/http"
	"os"
	"strconv"
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
		i18n.TrEvent("watcher.start"))

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
			i18n.TrEventf("config.reload_failed", err))
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
			i18n.TrEvent("watcher.safe_reopened_knock"))
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
			i18n.TrEvent("watcher.safe_reopened_session"))
	}
	if !sessionSeen || !lastActive {
		sessionSeen = true
		lastActive = true
		writeAdminIdleCounter(0)
		sys.LogAction("WATCHER", "SESSION_APPEARED", "SUCCESS",
			i18n.TrEvent("watcher.session_appeared"))
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
			i18n.TrEvent("watcher.idle_started"))
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
			i18n.TrEventf("watcher.idle_half_warning", newCount, timeout-newCount))
	}
	if newCount >= timeout {
		sys.LogAction("WATCHER", "IDLE_TIMEOUT_REACHED", "ALERT",
			i18n.TrEventf("watcher.idle_timeout_reached", timeout))
		incinerateHiddenRules() // все SIG_HIDDEN (стук + SSH_CONNECTION); SIG_LOCALE неприкосновенен
		_ = os.Remove(adminIdleCounterPath)
		safeClosed = true
		sys.LogAction("WATCHER", "SAFE_CLOSED", "SUCCESS",
			i18n.TrEvent("watcher.safe_closed"))
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
			i18n.TrEventf("watcher.utmp_read_fail", err))
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
			i18n.TrEventf("watcher.counter_write_fail", err))
		return
	}

	// Атомарное переименование
	if err := os.Rename(tmpPath, adminIdleCounterPath); err != nil {
		sys.LogAction("WATCHER", "COUNTER_RENAME_FAIL", "ERROR",
			i18n.TrEventf("watcher.counter_rename_fail", err))
	}
}

// incinerateHiddenRules испепеляет все внешние правила скрытого порта,
// превращая его в «невидимый ров» (DROP по умолчанию, долгое молчание).
//
// КОНТРАКТ v8.3.3 «ТРАНЗАКЦИЯ РОВ»: намеренный акт одновременно убивает
// всех трёх свидетелей состояния брони:
//  1. правила в ядре (ALLOW SIG_HIDDEN + LIMIT v4/v6);
//  2. квитанции на диске (agl_armor_active_port.N / agl_armor_ratelimit_port.N);
//  3. декларацию в инвентаре периметра (is_armor_enabled=false).
//
// Без пункта 3 RECONCILE_ARMOR в main.go при входе в TUI читает живую
// декларацию и «восстанавливает» лимитер, вскрывая ров: во рву слушатель
// sshd остаётся на порту, поэтому ss-рескан консервирует устаревшую
// декларацию навсегда. Единственная точка правды — сам акт испепеления.
//
// БАРЬЕРЫ:
//   - 127.0.0.1 / ::1 (SIG_LOCALE) не испепеляются никогда;
//   - LIMIT не удаляется в цикле: его снимает хвост транзакции через
//     PurgeLimitRule (без надгробия «Disabled», которое пробило бы ров).
func incinerateHiddenRules() {
	sys.LogAction("WATCHER", "INCINERATION_START", "PENDING",
		i18n.TrEvent("watcher.incineration_start"))

	// Читаем номер порта из маркера (или из конфига как fallback)
	port := readPortFromMarker()
	if port == "" {
		port = fmt.Sprintf("%d", config.C.RealSshPort)
	}
	portInt, _ := strconv.Atoi(port)

	statusRes := sys.RunTimeout(5*time.Second, "ufw", "status", "numbered")
	if statusRes.Err != nil {
		sys.LogAction("WATCHER", "UFW_STATUS_FAIL", "ERROR",
			fmt.Sprintf("event=ufw.status_failed err=%v", statusRes.Err))
		return
	}

	lines := strings.Split(statusRes.Stdout, "\n")
	incineratedCount := 0
	sawLimit := false

	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]

		// ЖЕЛЕЗОБЕТОННЫЙ БАРЬЕР: аварийный люк локальной петли неприкосновенен
		if strings.Contains(line, "127.0.0.1") || strings.Contains(line, "::1") {
			continue
		}

		// ФИЛЬТР ЦЕЛИ: только правила нашего скрытого порта
		if !strings.Contains(line, port+"/tcp") {
			continue
		}

		// LIMIT не сносим здесь: хвост транзакции удалит v4+v6 одним ударом
		// и заодно убьёт квитанции (M4-гигиена).
		if strings.Contains(line, "LIMIT") {
			sawLimit = true
			continue
		}

		// ТОЧЕЧНЫЙ ДЕМОНТАЖ ДВЕРЕЙ: ALLOW/DENY/REJECT с конкретным источником
		var action string
		switch {
		case strings.Contains(line, "ALLOW"):
			action = "allow"
		case strings.Contains(line, "DENY"):
			action = "deny"
		case strings.Contains(line, "REJECT"):
			action = "reject"
		default:
			continue
		}

		source := extractSourceFromLine(line)
		if source == "" || source == "Anywhere" || source == "Anywhere (v6)" {
			// Бессточниковое правило на скрытом порту — аномалия (не дверь
			// админа и не стук). Не трогаем: fail closed против самовскрытия,
			// но аномалия обязана быть видна в журнале.
			sys.LogAction("WATCHER", "MOAT_ANOMALY", "WARNING",
				fmt.Sprintf("event=watcher.moat_anomaly line=%s", strings.TrimSpace(line)))
			continue
		}

		delRes := sys.RunTimeout(5*time.Second, "ufw", "--force", "delete",
			action, "from", source, "to", "any", "port", port, "proto", "tcp")
		if delRes.Err != nil {
			delRes.LogWarnIfErr("WATCHER", "INCINERATION",
				fmt.Sprintf("event=ufw.incinerate_delete_failed action=%s source=%s", action, source))
			continue
		}

		incineratedCount++
		sys.LogAction("WATCHER", "RULE_INCINERATED", "SUCCESS",
			i18n.TrEventf("watcher.rule_incinerated", fmt.Sprintf("%s_%s", action, source)))
	}

	// ─────────────────────────────────────────────────────────────
	// ХВОСТ ТРАНЗАКЦИИ: РОВ. Выполняется ТОЛЬКО если акт состоялся:
	// в простое и при старте вотчер не меняет геометрию и декларации.
	// ─────────────────────────────────────────────────────────────
	if incineratedCount > 0 || sawLimit {
		sealMoat(portInt)
		if sawLimit {
			incineratedCount++ // лимитер учитываем как одно испепелённое правило
		}
	}

	sys.LogAction("WATCHER", "INCINERATION_COMPLETE", "SUCCESS",
		i18n.TrEventf("watcher.incineration_complete", incineratedCount))
}

// sealMoat — хвост транзакции РОВ (v8.3.3): убивает свидетелей №2 и №3
// после того, как свидетель №1 (правила ядра) уже мёртв.
//
//   - PurgeLimitRule: снос LIMIT v4/v6 БЕЗ надгробия «Disabled» + обе
//     квитанции (квитанция живёт и умирает вместе с правилом);
//   - инвентарь периметра: is_armor_enabled=false для службы скрытого
//     порта — иначе ss-рескан законсервирует ложь, а RECONCILE_ARMOR
//     восстановит лимитер при входе в TUI;
//   - сброс кэша UFW + аудит MOAT_SEALED.
//
// Идемпотентна: при отсутствующем лимитере ufw delete подтверждённо
// пропускает, квитанции и декларация гасятся в no-op.
func sealMoat(portInt int) {
	// Свидетель №2: квитанции и остаток лимитера (C1-санитар без надгробия).
	PurgeLimitRule(portInt)

	// Свидетель №3: декларация в /etc/amberguard/perimeter_inventory.json.
	// Матчим по порту: в инвентаре может быть несколько записей SSHD
	// (скрытый порт и прочие), порт — единственный надёжный ключ.
	apps, err := config.LoadDetectedApps()
	if err != nil {
		sys.LogAction("WATCHER", "MOAT_INVENTORY_SKIP", "WARNING",
			fmt.Sprintf("event=watcher.moat_inventory_skip err=%v", err))
	} else {
		for _, app := range apps {
			if app.Port == portInt {
				if errUpd := config.UpdateDetectedAppArmorStatus(app.Name, app.Port, false); errUpd != nil {
					sys.LogAction("WATCHER", "MOAT_INVENTORY_FAIL", "WARNING",
						fmt.Sprintf("event=watcher.moat_inventory_fail port=%d err=%v", portInt, errUpd))
					continue
				}
				sys.LogAction("WATCHER", "MOAT_INVENTORY_FLIPPED", "SUCCESS",
					fmt.Sprintf("event=watcher.moat_inventory_flipped service=%s port=%d", app.Name, portInt))
			}
		}
	}

	ResetUfwCacheSnapshot()
	sys.LogAction("WATCHER", "MOAT_SEALED", "SUCCESS",
		fmt.Sprintf("event=watcher.moat_sealed port=%d", portInt))
}

// readPortFromMarker читает номер порта из персистентного маркера agl_ssh_moved.
// Возвращает пустую строку, если маркер отсутствует — вызывающий
// обязан сделать fallback на config.C.RealSshPort.
func readPortFromMarker() string {
	data, err := os.ReadFile(config.MarkerSshMoved)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// extractSourceFromLine извлекает источник (IP или Anywhere) из строки ufw status.
// Формат: "[14] 2244/tcp ALLOW IN 203.0.113.10" или "[15] 2244/tcp LIMIT IN Anywhere"
func extractSourceFromLine(line string) string {
	parts := strings.Fields(line)
	for i, part := range parts {
		if (part == "ALLOW" || part == "LIMIT" || part == "DENY" || part == "REJECT") &&
			i+2 < len(parts) && parts[i+1] == "IN" {
			return parts[i+2] // Возвращаем IP или "Anywhere"
		}
	}
	return ""
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
		i18n.TrEventf("watcher.web_rescue_dir_init", webRescueDir))
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
			i18n.TrEventf("watcher.web_knock_read_fail", err))
		return
	}
	adminIP := strings.TrimSpace(string(data))

	// Шаг 3: v8.2.0 (1.5): строгая валидация IPv4 через net.ParseIP
	if !isValidKnockIPv4(adminIP) {
		sys.LogAction("WATCHER", "WEB_KNOCK_INVALID_IP", "ALERT",
			i18n.TrEventf("watcher.web_knock_invalid_ip", adminIP))
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
			i18n.TrEventf("web_knock.ufw_persist_failed", adminIP, targetPort))

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
		i18n.TrEventf("watcher.web_knock_processed", adminIP, targetPort))
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
				i18n.TrEvent("web_rescue.server_disabled"))
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
			i18n.TrEventf("web_rescue.server_restart", wantPort))
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
