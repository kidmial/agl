// internal/armor/port_move_protocol.go
package armor

import (
	"agl/config"
	"agl/i18n"
	"agl/internal/sys"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	defaultPort    = "22"
	defaultPortInt = 22 // Для передачи в функции, принимающие int (SetCustomRateLimit, waitForSshListener)
	markerFile     = config.MarkerSshMoved
	sleepTime      = 300 * time.Millisecond
	restartSleep   = 200 * time.Millisecond
)

// ============================================================================
// R-7 (v8.2.3): ГИГИЕНА РЕЗЕРВНЫХ КОПИЙ sshd_config
// ============================================================================
// sshOriginalPath — неприкосновенный якорь ручного отката.
// Создаётся ОДИН раз при самом первом кастлинге из заводского sshd_config.
// AGL НИКОГДА не перезаписывает и не удаляет его: первая копия — самая честная,
// это состояние «до первого прикосновения AGL к SSH».
const sshOriginalPath = "/etc/ssh/sshd_config.agl_original"

// sshBackupPrefix — префикс штампованных (rolling) бэкапов кастлинга.
// Ротация трогает ТОЛЬКО файлы с этим префиксом:
// якорь agl_original и системный ucf-dist остаются вне юрисдикции.
const sshBackupPrefix = "sshd_config.agl_backup_"

// sshBackupRetention — сколько свежих rolling-бэкапов держать (R-7).
// История обязана быть ограниченной, как logrotate:
// бесконечное накопление — это энтропия, а не история.
const sshBackupRetention = 3

// ensureSshOriginalAnchor сохраняет заводской sshd_config при ПЕРВОМ кастлинге (R-7).
// Вызывается ДО модификации конфига: в этот момент sshd_config ещё
// в состоянии «до AGL» (или восстановлен предыдущим OFF — это то же самое).
//
// СЕМАНТИКА:
//   - якорь уже есть → ничего не делаем (не перезаписываем никогда);
//   - якоря нет → копируем текущий sshd_config в якорь;
//   - сбой копирования → WARNING в журнал, кастлинг продолжается:
//     откат всё равно обеспечен штампованным бэкапом.
func ensureSshOriginalAnchor() {
	// Якорь существует — первая копия священна, не трогаем.
	if _, err := os.Stat(sshOriginalPath); err == nil {
		return
	}
	// Копируем через единый интерфейс с таймаутом.
	res := sys.RunTimeout(5*time.Second, "cp", "/etc/ssh/sshd_config", sshOriginalPath)
	if res.Err != nil {
		sys.LogAction("PORT_CASTLING", "SSH_ORIGINAL_ANCHOR_FAIL", "WARNING",
			i18n.TrEventf("ssh.original_anchor_failed",
				res.Err, strings.TrimSpace(res.Stderr)))
		return
	}
	sys.LogAction("PORT_CASTLING", "SSH_ORIGINAL_ANCHOR_CREATED", "SUCCESS",
		i18n.TrEventf("port_castling.ssh_original_anchor_created", sshOriginalPath))
}

// pruneSshBackups — ограниченная ротация штампованных бэкапов кастлинга (R-7).
// Держит sshBackupRetention самых свежих файлов с префиксом sshBackupPrefix,
// остальные удаляет в момент создания нового бэкапа.
//
// ПОЧЕМУ БЕЗОПАСНО:
//   - трогает ТОЛЬКО файлы с префиксом sshd_config.agl_backup_;
//   - якорь sshd_config.agl_original и системный ucf-dist вне юрисдикции;
//   - штампы 20060102150405 сортируются лексикографически = хронологически,
//     поэтому самые старые оказываются в начале отсортированного среза.
func pruneSshBackups() {
	entries, err := os.ReadDir("/etc/ssh")
	if err != nil {
		// Директория не читается — ротацию пропускаем, это не фатально.
		sys.LogAction("PORT_CASTLING", "SSH_BACKUP_PRUNE_SKIP", "WARNING",
			fmt.Sprintf("event=ssh.backup_prune_skip err=%v", err))
		return
	}
	// Собираем только rolling-бэкапы (якорь и ucf-dist не попадают по префиксу).
	var stamps []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasPrefix(e.Name(), sshBackupPrefix) {
			stamps = append(stamps, e.Name())
		}
	}
	// Хронологическая сортировка: старейшие — в начале.
	sort.Strings(stamps)
	// Всё, что старше окна хранения, удаляем старейшими первыми.
	for len(stamps) > sshBackupRetention {
		oldest := stamps[0]
		stamps = stamps[1:]
		if err := os.Remove("/etc/ssh/" + oldest); err != nil {
			sys.LogAction("PORT_CASTLING", "SSH_BACKUP_PRUNE_FAIL", "WARNING",
				fmt.Sprintf("event=ssh.backup_prune_failed file=%s err=%v", oldest, err))
			continue
		}
		sys.LogAction("PORT_CASTLING", "SSH_BACKUP_PRUNED", "SUCCESS",
			i18n.TrEventf("ssh.backup_pruned", oldest, sshBackupRetention))
	}
}

// SetMovePortProtocol выполняет Алгоритм Кастлинга портов OpenSSH v6.0.0.
// Переносит порт управления и бесшовно мигрирует сетевую броню лимитера.
//
// ОБНОВЛЕНО В v8.2.2 (п. 4.6): Декомпозиция монолита на 4 фазы:
//   - prepareCastlingON / prepareCastlingOFF — фаза подготовки
//   - applyCastlingON / applyCastlingOFF — фаза применения (включает верификацию)
//   - rollbackCastlingON — фаза отката при сбое валидации
//
// Публичный контракт НЕ изменён: func SetMovePortProtocol(action string) error
func SetMovePortProtocol(action string) error {
	action = strings.ToUpper(action)
	customPort := fmt.Sprintf("%d", config.C.RealSshPort)

	// ВЕРСИЯ 6.0.0: Строго унифицированный персистентный маркер геометрии сервера
	markerFile := config.MarkerSshMoved

	switch action {
	case "ON":
		// ═══════════════════════════════════════════════════════════════
		// ФАЗА 1: ПОДГОТОВКА К РОКИРОВКЕ ON
		// ═══════════════════════════════════════════════════════════════
		backupPath, limiterWasActive, err := prepareCastlingON(customPort)
		if err != nil {
			return err
		}

		// ═══════════════════════════════════════════════════════════════
		// ФАЗА 2+3: ПРИМЕНЕНИЕ + ВЕРИФИКАЦИЯ (с автоматическим откатом)
		// ═══════════════════════════════════════════════════════════════
		if err := applyCastlingON(customPort, backupPath, limiterWasActive); err != nil {
			// ═══════════════════════════════════════════════════════════════
			// ФАЗА 4: ОТКАТ ПРИ СБОЕ
			// ═══════════════════════════════════════════════════════════════
			rollbackCastlingON(backupPath, customPort)
			return err
		}

		ResetUfwCacheSnapshot()
		return nil

	case "OFF":
		// ═══════════════════════════════════════════════════════════════
		// ФАЗА 1: ПОДГОТОВКА К ОБРАТНОЙ РОКИРОВКЕ OFF
		// ═══════════════════════════════════════════════════════════════
		limiterWasActive := prepareCastlingOFF(customPort)

		// ═══════════════════════════════════════════════════════════════
		// ФАЗА 2+3: ПРИМЕНЕНИЕ + ВЕРИФИКАЦИЯ
		// ═══════════════════════════════════════════════════════════════
		if err := applyCastlingOFF(customPort, limiterWasActive); err != nil {
			return err
		}

		ResetUfwCacheSnapshot()
		return nil

	case "CHECK":
		if _, err := os.Stat(markerFile); err == nil {
			return nil // Рокировка активна (SSH на кастомном порту)
		}
		return fmt.Errorf("%s", i18n.T.PortMoveCastlingInactive)
	}
	return fmt.Errorf("%s", fmt.Sprintf(i18n.T.PortMoveUnknownAction, action))
}

// ═══════════════════════════════════════════════════════════════════════════════
// ФАЗА 1: ПОДГОТОВКА К РОКИРОВКЕ ON
// ═══════════════════════════════════════════════════════════════════════════════

// prepareCastlingON выполняет подготовительные шаги перед рокировкой ON:
//   - Проверка, что порт для рокировки свободен
//   - Создание бэкапа sshd_config
//   - Приостановка лимитера на стандартном порту 22 (если был активен)
//   - Фиксация геометрии кастлинга в маркере
//   - Старт юнита наблюдателя (agld_watcher.service)
//
// КОНТРАКТ (п. 4.6 аудита — «фаза подготовки»):
//   - Возвращает (backupPath, limiterWasActive, nil) при успехе
//   - Возвращает ("", false, err) при критическом сбое (рокировка прервана)
//
// ПОБОЧНЫЕ ЭФФЕКТЫ:
//   - Создаёт бэкап sshd_config с таймстампом
//   - Создаёт маркер геометрии (agl_ssh_moved)
//   - Устанавливает и стартует systemd-юнит agld_watcher.service
//
// ОБНОВЛЕНО В v8.2.2 (п. 4.6): Извлечено из SetMovePortProtocol для декомпозиции монолита.
func prepareCastlingON(customPort string) (backupPath string, limiterWasActive bool, err error) {
	// v8.2.0 (1.8.5): проверка, что порт для рокировки свободен
	conn, errListen := net.Listen("tcp", fmt.Sprintf(":%d", config.C.RealSshPort))
	if errListen != nil {
		return "", false, fmt.Errorf(i18n.T.PortMovePortBusy, config.C.RealSshPort, errListen)
	}
	_ = conn.Close()

	// R-7 (v8.2.3): ДО модификации конфига сохраняем заводской якорь.
	// Создаётся один раз за всю жизнь хоста; далее вызовы — no-op.
	ensureSshOriginalAnchor()
	// v8.2.0 (1.8.1): резервная копия sshd_config перед изменениями.
	// R-7: путь собран из константы префикса — единая точка истины для ротации.
	backupPath = "/etc/ssh/" + sshBackupPrefix + time.Now().Format("20060102150405")
	// БЛОК 3.1: бэкап через единый интерфейс; при сбое — stderr в ошибке.
	backupRes := sys.RunTimeout(5*time.Second, "cp", "/etc/ssh/sshd_config", backupPath)
	if backupRes.Err != nil {
		return "", false, fmt.Errorf(i18n.T.PortMoveBackupCreateFailed,
			backupRes.Err, strings.TrimSpace(backupRes.Stderr))
	}
	sys.LogAction("PORT_CASTLING", "SSHD_BACKUP_CREATED", "SUCCESS",
		i18n.TrEventf("port_castling.sshd_backup_created", backupPath))
	// R-7 (v8.2.3): ограниченная ротация — держим 3 свежих rolling-бэкапа,
	// остальные зачищаем прямо сейчас, в момент создания нового.
	pruneSshBackups()

	// Проверяем активность лимитера на стандартном 22 порту до начала рокировки
	limiterWasActive = false
	if err := SetCustomRateLimit(defaultPortInt, "open_ssh", "Open SSH", "CHECK"); err == nil {
		limiterWasActive = true
		_ = SetCustomRateLimit(defaultPortInt, "open_ssh", "Open SSH", "OFF")
		sys.LogAction("PORT_CASTLING", "LIMITER_SUSPEND", "SUCCESS", i18n.TrEvent("port_castling.limiter_suspend"))
	}

	// Фиксируем геометрию кастлинга в гарантированную директорию states
	_ = os.MkdirAll(filepath.Dir(markerFile), 0755)
	if err := sys.WriteAtomic(markerFile, []byte(customPort), 0644); err != nil {
		return "", false, fmt.Errorf(i18n.T.PortMoveGeometryWriteFailed, err)
	}

	// ─────────────────────────────────────────────────────────────
	// M2 (v8.1.2): Старт юнита наблюдателя простоя администратора.
	// Юнит стартует при создании маркера `agl_ssh_moved`.
	// ─────────────────────────────────────────────────────────────
	if err := ensureWatcherUnit(); err != nil {
		sys.LogAction("PORT_CASTLING", "WATCHER_UNIT_FAIL", "WARNING",
			i18n.TrEventf("port_castling.watcher_unit_fail", err))
	}

	// БЛОК 3.1: старт вотчера больше не слепой.
	daemonReloadRes := sys.RunTimeout(10*time.Second, "systemctl", "daemon-reload")
	daemonReloadRes.LogWarnIfErr("PORT_CASTLING", "WATCHER_START", "event=systemd.daemon_reload_failed")

	enableWatcherRes := sys.RunTimeout(10*time.Second, "systemctl", "enable", "agld_watcher.service")
	enableWatcherRes.LogWarnIfErr("PORT_CASTLING", "WATCHER_START", "event=systemd.watcher_enable_failed")

	startWatcherRes := sys.RunTimeout(10*time.Second, "systemctl", "start", "agld_watcher.service")
	if startWatcherRes.Err != nil {
		// Вотчер не стартовал — канал M2 деградировал, но рокировка важнее:
		// миграция SSH продолжается, админ увидит WARNING в журнале.
		startWatcherRes.LogWarnIfErr("PORT_CASTLING", "WATCHER_START", "event=systemd.watcher_start_failed")
	} else {
		sys.LogAction("PORT_CASTLING", "WATCHER_START", "SUCCESS",
			i18n.TrEvent("port_castling.watcher_start"))
	}

	return backupPath, limiterWasActive, nil
}

// ═══════════════════════════════════════════════════════════════════════════════
// ФАЗА 2+3: ПРИМЕНЕНИЕ + ВЕРИФИКАЦИЯ (РОКИРОВКА ON)
// ═══════════════════════════════════════════════════════════════════════════════

// applyCastlingON выполняет применение рокировки ON:
//   - Модификация конфигурации sshd_config на кастомный порт
//   - Валидация конфигурации (sshd -t) с автоматическим откатом при сбое
//   - Создание правил анти-локаута (UFW whitelist)
//   - Зачистка старых правил порта 22
//   - Маскирование ssh.socket
//   - Рестарт ssh.service на новом порту
//   - Восстановление лимитера на новом порту
//   - Асинхронный перескан периметра
//
// КОНТРАКТ (п. 4.6 аудита — «фаза применения + верификация»):
//   - Возвращает nil при успехе
//   - Возвращает err при критическом сбое (вызывающий должен вызвать rollbackCastlingON)
//
// ПОБОЧНЫЕ ЭФФЕКТЫ:
//   - Модифицирует /etc/ssh/sshd_config
//   - Создаёт/удаляет правила UFW
//   - Маскирует ssh.socket
//   - Перезапускает ssh.service
//   - Запускает горутину перескана периметра
//
// ОБНОВЛЕНО В v8.2.2 (п. 4.6): Извлечено из SetMovePortProtocol для декомпозиции монолита.
func applyCastlingON(customPort, backupPath string, limiterWasActive bool) error {
	// Модифицируем конфигурацию демона OpenSSH
	if err := modifySshConfig(customPort); err != nil {
		return err
	}

	// v8.2.0 (1.8.2): валидация конфигурации перед рестартом sshd
	// БЛОК 3.1: валидация через единый интерфейс, stderr в ошибке.
	sshTestRes := sys.RunTimeout(10*time.Second, "sshd", "-t")
	if sshTestRes.Err != nil {
		// Валидация провалилась — откатываем бэкап и прерываем рокировку.
		rollbackRes := sys.RunTimeout(5*time.Second, "cp", backupPath, "/etc/ssh/sshd_config")
		if rollbackRes.Err != nil {
			// ОТКАТ НЕ ПОДТВЕРЖДЁН: sshd_config мог остаться битым.
			// Это ALERT: без ручного восстановления SSH не поднимется.
			sys.LogAction("PORT_CASTLING", "SSHD_ROLLBACK_FAIL", "ALERT",
				i18n.TrEventf("sshd.rollback_failed",
					rollbackRes.Err, strings.TrimSpace(rollbackRes.Stderr), backupPath))
		}
		_ = os.Remove(markerFile)
		return fmt.Errorf(i18n.T.PortMoveValidationFailedRollback,
			sshTestRes.Err, strings.TrimSpace(sshTestRes.Stderr))
	}
	sys.LogAction("PORT_CASTLING", "SSHD_CONFIG_WRITE", "SUCCESS", i18n.TrEventf("port_castling.sshd_config_write", customPort))

	// ─────────────────────────────────────────────────────────────
	// M1 (v8.1.2): Анти-локаут кастлинга — прерогатива proto_move_port.
	// Два точечных правила по сигнатурному контракту v2:
	//  1. SIG_LOCALE  → 127.0.0.1 → НЕ испепеляется никогда
	//  2. SIG_ADMIN   → IP админа (SSH_CONNECTION) → цель испепеления
	//
	// При сбое детекции IP админа — только SIG_LOCALE + ALERT.
	// ─────────────────────────────────────────────────────────────
	// БЛОК 3.1 (lint S1039): строка без форматных глаголов — Sprintf не нужен.
	sigLocale := "Amber Guard Lab. Master Admin Anti-Lockout"
	sigHidden := fmt.Sprintf("Amber Guard Lab. Private SSH Invisible Port %s", customPort)

	// Правило 1: локальная петля (не испепеляется никогда).
	// БЛОК 3.1: сбой не игнорируем — локальный доступ тоже канал спасения.
	localeRes := sys.RunTimeout(10*time.Second, "ufw", "allow", "from", "127.0.0.1",
		"to", "any", "port", customPort, "proto", "tcp", "comment", sigLocale)
	if localeRes.Err != nil {
		localeRes.LogWarnIfErr("PORT_CASTLING", "ANTI_LOCKOUT_LOCALE", "event=ufw.locale_allow_failed")
	} else {
		sys.LogAction("PORT_CASTLING", "ANTI_LOCKOUT_LOCALE_CREATED", "SUCCESS",
			i18n.TrEventf("port_castling.anti_lockout_locale_created", customPort))
	}

	// Правило 2: точечный whitelist IP админа
	adminIP := ""
	sshConn := os.Getenv("SSH_CONNECTION")
	if sshConn != "" {
		parts := strings.Split(sshConn, " ")
		if len(parts) > 0 {
			adminIP = parts[0]
		}
	}
	if adminIP != "" {
		// Правило 2: точечный whitelist IP админа.
		// БЛОК 3.1: сбой вставки = потенциальный локаут после рестарта sshd.
		// Рокировку не рвём (локальная петля жива), но WARNING обязателен.
		adminRes := sys.RunTimeout(10*time.Second, "ufw", "allow", "from", adminIP,
			"to", "any", "port", customPort, "proto", "tcp", "comment", sigHidden)
		if adminRes.Err != nil {
			adminRes.LogWarnIfErr("PORT_CASTLING", "ANTI_LOCKOUT_ADMIN", "event=ufw.admin_allow_failed")
		} else {
			sys.LogAction("PORT_CASTLING", "ANTI_LOCKOUT_ADMIN", "SUCCESS",
				i18n.TrEventf("port_castling.anti_lockout_admin", adminIP, customPort))
		}
	} else {
		// Сбой детекции: НЕ открывать подсеть. Только локальная петля + ALERT.
		sys.LogAction("PORT_CASTLING", "ANTI_LOCKOUT_ALERT", "ALERT",
			i18n.TrEvent("port_castling.anti_lockout_alert"))
	}

	// Жестко зачищаем старый периметр порта 22 от старых правил UFW.
	// БЛОК 3.1: зачистка best-effort (правил может не быть),
	// но каждый сбой логируется со своим машинным кодом.
	delLimit22 := sys.RunTimeout(10*time.Second, "ufw", "--force", "delete", "limit", "22/tcp")
	delLimit22.LogWarnIfErr("PORT_CASTLING", "PERIMETER_CLEAN", "event=ufw.limit22_delete_failed")

	delAllow22 := sys.RunTimeout(10*time.Second, "ufw", "--force", "delete", "allow", "22/tcp")
	delAllow22.LogWarnIfErr("PORT_CASTLING", "PERIMETER_CLEAN", "event=ufw.allow22_delete_failed")

	delReject22 := sys.RunTimeout(10*time.Second, "ufw", "--force", "delete", "reject", "22/tcp")
	delReject22.LogWarnIfErr("PORT_CASTLING", "PERIMETER_CLEAN", "event=ufw.reject22_delete_failed")

	reloadAfterClean := sys.RunTimeout(10*time.Second, "ufw", "reload")
	reloadAfterClean.LogWarnIfErr("PORT_CASTLING", "PERIMETER_CLEAN", "event=ufw.reload_failed")

	time.Sleep(sleepTime) // Стабилизационная пауза по спецификации

	// Полное маскирование сокет-активатора Ubuntu во избежание перехвата порта ядра.
	// БЛОК 3.1: каждый шаг проверяем; SUCCESS только после подтверждённой маски.
	stopSocketRes := sys.RunTimeout(10*time.Second, "systemctl", "stop", "ssh.socket")
	stopSocketRes.LogWarnIfErr("PORT_CASTLING", "SOCKET_MASK", "event=systemd.socket_stop_failed")

	disableSocketRes := sys.RunTimeout(10*time.Second, "systemctl", "disable", "ssh.socket")
	disableSocketRes.LogWarnIfErr("PORT_CASTLING", "SOCKET_MASK", "event=systemd.socket_disable_failed")

	maskSocketRes := sys.RunTimeout(10*time.Second, "systemctl", "mask", "ssh.socket")
	if maskSocketRes.Err != nil {
		// Сокет не замаскирован: риск, что systemd перехватит порт после рестарта.
		maskSocketRes.LogWarnIfErr("PORT_CASTLING", "SOCKET_MASK", "event=systemd.socket_mask_failed")
	} else {
		sys.LogAction("PORT_CASTLING", "SOCKET_MASK", "SUCCESS", i18n.TrEvent("port_castling.socket_mask"))
	}

	reloadBeforeRestart := sys.RunTimeout(10*time.Second, "systemctl", "daemon-reload")
	reloadBeforeRestart.LogWarnIfErr("PORT_CASTLING", "SOCKET_MASK", "event=systemd.daemon_reload_failed")

	// Критичная команда: без рестарта sshd рокировка не произойдёт.
	// Ошибка наружу + stderr для диагностики.
	restartSshRes := sys.RunTimeout(20*time.Second, "systemctl", "restart", "ssh.service")
	if restartSshRes.Err != nil {
		return fmt.Errorf(i18n.T.PortMoveSshRestartFailed,
			restartSshRes.Err, strings.TrimSpace(restartSshRes.Stderr))
	}
	sys.LogAction("PORT_CASTLING", "SSH_SERVICE_RESTART", "SUCCESS", i18n.TrEventf("port_castling.ssh_service_restart", customPort))

	// ─────────────────────────────────────────────────────────────
	// R-8 (симметрично фазе OFF): контрольная верификация кастомного порта.
	// systemctl может вернуть 0, но sshd не забиндится (порт занят, права и т.д.).
	// Если баннер не получен — рокировка не удалась, возвращаем ошибку наружу,
	// чтобы SetMovePortProtocol принудительно вызвал rollbackCastlingON.
	// ─────────────────────────────────────────────────────────────
	if err := waitForSshListener(config.C.RealSshPort, 5, 400*time.Millisecond); err != nil {
		sys.LogAction("PORT_CASTLING", "SSH_SERVICE_RESTART", "ALERT",
			i18n.TrEventf("port_castling.ssh_listener_verify_failed", config.C.RealSshPort, err))
		return fmt.Errorf(i18n.T.PortMoveSshListenerVerifyFailed, config.C.RealSshPort, err)
	}

	if limiterWasActive {
		time.Sleep(restartSleep)
		if err := SetCustomRateLimit(config.C.RealSshPort, "open_ssh", "Open SSH", "ON"); err != nil {
			sys.LogAction("PORT_CASTLING", "LIMITER_RESTORE", "WARNING",
				i18n.TrEventf("port_castling.limiter_restore_failed", config.C.RealSshPort, err))
		} else {
			sys.LogAction("PORT_CASTLING", "LIMITER_RESTORE", "SUCCESS",
				i18n.TrEventf("port_castling.limiter_restore", config.C.RealSshPort))
		}
	}

	// ВАРИАНТ А (v8.4.6): СИНХРОННЫЙ перескан периметра после рокировки.
	// Геометрия портов изменилась — инвентарь обязан самоактуализироваться,
	// иначе предохранитель ямы (C8) смотрит в устаревшие данные
	// («порт 22 занят легитимным процессом SSHD»).
	//
	// УБРАНО `go func()`: если SetMovePortProtocol вызван из CLI, процесс завершится
	// (os.Exit) до того, как горутина успеет обновить инвентарь в базе данных.
	// Делаем синхронно, но в анонимной обёртке с defer recover() — чтобы паника
	// внутри RunPerimeterInventoryScan не уронила весь CLI-процесс.
	func() {
		defer func() {
			if r := recover(); r != nil {
				sys.LogAction("PORT_CASTLING", "PERIMETRE_RESCAN", "ALERT",
					i18n.TrEventf("port_castling.perimetre_rescan_panic", r))
			}
		}()
		if err := config.RunPerimeterInventoryScan(); err != nil {
			sys.LogAction("PORT_CASTLING", "PERIMETRE_RESCAN", "WARNING",
				i18n.TrEventf("port_castling.perimetre_rescan_failed", err))
		} else {
			sys.LogAction("PORT_CASTLING", "PERIMETRE_RESCAN", "SUCCESS",
				i18n.TrEvent("port_castling.perimetre_rescan"))
		}
	}()

	return nil
}

// ═══════════════════════════════════════════════════════════════════════════════
// ФАЗА 4: ОТКАТ ПРИ СБОЕ (РОКИРОВКА ON)
// ═══════════════════════════════════════════════════════════════════════════════

// rollbackCastlingON выполняет откат рокировки ON при критическом сбое:
//   - Удаление маркера геометрии
//   - Остановка юнита наблюдателя
//   - Логирование ALERT для ручного вмешательства
//
// КОНТРАКТ (п. 4.6 аудита — «фаза отката»):
//   - Вызывается ТОЛЬКО при сбое в applyCastlingON (после успешной prepareCastlingON)
//   - НЕ возвращает ошибку — best-effort cleanup
//
// ПОБОЧНЫЕ ЭФФЕКТЫ:
//   - Удаляет маркер agl_ssh_moved
//   - Останавливает и отключает agld_watcher.service
//
// ОБНОВЛЕНО В v8.2.2 (п. 4.6): Извлечено из SetMovePortProtocol для декомпозиции монолита.
func rollbackCastlingON(backupPath, customPort string) {
	sys.LogAction("PORT_CASTLING", "ROLLBACK_START", "WARNING",
		i18n.TrEventf("port_castling.rollback_start", backupPath))

	// Удаляем маркер геометрии
	_ = os.Remove(markerFile)

	// R-8: откат обязан реально делать то, что обещает журнал:
	// восстановить sshd_config из бэкапа и вернуть sshd на порт 22.
	restoreRes := sys.RunTimeout(5*time.Second, "cp", backupPath, "/etc/ssh/sshd_config")
	if restoreRes.Err != nil {
		sys.LogAction("PORT_CASTLING", "SSHD_ROLLBACK_FAIL", "ALERT",
			i18n.TrEventf("sshd.rollback_failed",
				restoreRes.Err, strings.TrimSpace(restoreRes.Stderr), backupPath))
	} else {
		// Возвращаем периметр порта 22: правила UFW были зачищены в apply-фазе
		restorePort22Perimeter()
		if testRes := sys.RunTimeout(10*time.Second, "sshd", "-t"); testRes.Err != nil {
			sys.LogAction("PORT_CASTLING", "SSHD_ROLLBACK_FAIL", "ALERT",
				i18n.TrEventf("sshd.rollback_failed",
					testRes.Err, strings.TrimSpace(testRes.Stderr), backupPath))
		} else {
			restartRes := sys.RunTimeout(20*time.Second, "systemctl", "restart", "ssh.service")
			restartRes.LogWarnIfErr("PORT_CASTLING", "ROLLBACK_SSH_RESTART", "event=systemd.rollback_restart_failed")
		}
	}

	// Останавливаем юнит наблюдателя
	disableWatcherRes := sys.RunTimeout(10*time.Second, "systemctl", "disable", "agld_watcher.service")
	disableWatcherRes.LogWarnIfErr("PORT_CASTLING", "ROLLBACK_WATCHER", "event=systemd.watcher_disable_failed")

	stopWatcherRes := sys.RunTimeout(10*time.Second, "systemctl", "stop", "agld_watcher.service")
	stopWatcherRes.LogWarnIfErr("PORT_CASTLING", "ROLLBACK_WATCHER", "event=systemd.watcher_stop_failed")

	reloadRes := sys.RunTimeout(10*time.Second, "systemctl", "daemon-reload")
	reloadRes.LogWarnIfErr("PORT_CASTLING", "ROLLBACK_WATCHER", "event=systemd.daemon_reload_failed")

	sys.LogAction("PORT_CASTLING", "ROLLBACK_COMPLETE", "WARNING",
		i18n.TrEvent("port_castling.rollback_complete"))
}

// ═══════════════════════════════════════════════════════════════════════════════
// ФАЗА 1: ПОДГОТОВКА К ОБРАТНОЙ РОКИРОВКЕ OFF
// ═══════════════════════════════════════════════════════════════════════════════

// prepareCastlingOFF выполняет подготовительные шаги перед обратной рокировкой OFF:
//   - Приостановка лимитера на кастомном порту (если был активен)
//   - Удаление персистентного маркера геометрии
//   - Остановка юнита наблюдателя (agld_watcher.service)
//
// КОНТРАКТ (п. 4.6 аудита — «фаза подготовки»):
//   - Возвращает limiterWasActive (bool)
//   - Не возвращает ошибку — best-effort подготовка
//
// ПОБОЧНЫЕ ЭФФЕКТЫ:
//   - Удаляет маркер agl_ssh_moved
//   - Останавливает и отключает agld_watcher.service
//
// ОБНОВЛЕНО В v8.2.2 (п. 4.6): Извлечено из SetMovePortProtocol для декомпозиции монолита.
func prepareCastlingOFF(customPort string) (limiterWasActive bool) {
	// Проверяем активность лимитера на кастомном порту перед откатом
	limiterWasActive = false
	if err := SetCustomRateLimit(config.C.RealSshPort, "open_ssh", "Open SSH", "CHECK"); err == nil {
		limiterWasActive = true
		_ = SetCustomRateLimit(config.C.RealSshPort, "open_ssh", "Open SSH", "OFF")
		sys.LogAction("PORT_CASTLING", "LIMITER_SUSPEND", "SUCCESS", i18n.TrEventf("port_castling.limiter_suspend_custom", config.C.RealSshPort))
	}

	// Удаляем персистентный маркер геометрии v6.0.0
	_ = os.Remove(markerFile)

	// ─────────────────────────────────────────────────────────────
	// M2 (v8.1.2): Стоп юнита наблюдателя простоя администратора.
	// Юнит останавливается при удалении маркера `agl_ssh_moved`.
	// ─────────────────────────────────────────────────────────────
	// БЛОК 3.1: стоп вотчера больше не слепой.
	disableWatcherRes := sys.RunTimeout(10*time.Second, "systemctl", "disable", "agld_watcher.service")
	disableWatcherRes.LogWarnIfErr("PORT_CASTLING", "WATCHER_STOP", "event=systemd.watcher_disable_failed")

	stopWatcherRes := sys.RunTimeout(10*time.Second, "systemctl", "stop", "agld_watcher.service")
	if stopWatcherRes.Err != nil {
		stopWatcherRes.LogWarnIfErr("PORT_CASTLING", "WATCHER_STOP", "event=systemd.watcher_stop_failed")
	} else {
		sys.LogAction("PORT_CASTLING", "WATCHER_STOP", "SUCCESS",
			i18n.TrEvent("port_castling.watcher_stop"))
	}

	reloadAfterWatcher := sys.RunTimeout(10*time.Second, "systemctl", "daemon-reload")
	reloadAfterWatcher.LogWarnIfErr("PORT_CASTLING", "WATCHER_STOP", "event=systemd.daemon_reload_failed")

	return limiterWasActive
}

// ═══════════════════════════════════════════════════════════════════════════════
// ФАЗА 2+3: ПРИМЕНЕНИЕ + ВЕРИФИКАЦИЯ (ОБРАТНАЯ РОКИРОВКА OFF)
// ═══════════════════════════════════════════════════════════════════════════════

// applyCastlingOFF выполняет применение обратной рокировки OFF:
//   - Возврат конфигурации sshd_config на стандартный порт 22
//   - Валидация конфигурации (sshd -t)
//   - Удаление правил анти-локаута (SIG_HIDDEN)
//   - Возврат правил порта 22
//   - Размаскирование ssh.socket
//   - Рестарт ssh.service на порту 22
//   - Восстановление лимитера на порту 22
//   - Асинхронный перескан периметра
//
// КОНТРАКТ (п. 4.6 аудита — «фаза применения + верификация»):
//   - Возвращает nil при успехе
//   - Возвращает err при критическом сбое
//
// ПОБОЧНЫЕ ЭФФЕКТЫ:
//   - Модифицирует /etc/ssh/sshd_config
//   - Удаляет правила UFW (SIG_HIDDEN)
//   - Размаскирует ssh.socket
//   - Перезапускает ssh.service
//   - Запускает горутину перескана периметра
//
// ОБНОВЛЕНО В v8.2.2 (п. 4.6): Извлечено из SetMovePortProtocol для декомпозиции монолита.
func applyCastlingOFF(customPort string, limiterWasActive bool) error {
	// Возвращаем стандартный Port 22 в sshd_config
	if err := modifySshConfig(defaultPort); err != nil {
		// Тут ошибку логичнее прокинуть, так как без конфига SSH не заведется
		return err
	}

	// v8.2.0 (1.8.2): валидация конфигурации перед рестартом sshd
	// БЛОК 3.1: валидация отката через единый интерфейс, stderr в ошибке.
	sshTestOffRes := sys.RunTimeout(10*time.Second, "sshd", "-t")
	if sshTestOffRes.Err != nil {
		return fmt.Errorf(i18n.T.PortMoveValidationFailedOff,
			sshTestOffRes.Err, strings.TrimSpace(sshTestOffRes.Stderr))
	}
	sys.LogAction("PORT_CASTLING", "SSHD_CONFIG_RESTORE", "SUCCESS", i18n.TrEvent("port_castling.sshd_config_restore"))

	// ─────────────────────────────────────────────────────────────
	// M1 (v8.1.2): OFF — полная очистка правил анти-локаута.
	// При возврате на порт 22, все правила для customPort (и SIG_HIDDEN,
	// и SIG_LOCALE) должны быть безжалостно удалены как мусор.
	// ─────────────────────────────────────────────────────────────
	sigHidden := fmt.Sprintf("Amber Guard Lab. Private SSH Invisible Port %s", customPort)
	sigLocale := "Amber Guard Lab. Master Admin Anti-Lockout"

	// Парсим вывод ufw и удаляем ЛЮБЫЕ правила AGL для кастомного порта.
	statusOffRes := sys.RunTimeout(5*time.Second, "ufw", "status", "numbered")
	if statusOffRes.Err != nil {
		statusOffRes.LogWarnIfErr("PORT_CASTLING", "ANTI_LOCKOUT_REMOVE", "event=ufw.status_failed")
	} else {
		lines := strings.Split(statusOffRes.Stdout, "\n")
		// Итерируем в обратном порядке, чтобы номера правил не съезжали при удалении
		for i := len(lines) - 1; i >= 0; i-- {
			line := lines[i]
			// Удаляем правило, если оно относится к кастомному порту И содержит нашу сигнатуру (внешнюю или локальную)
			if strings.Contains(line, customPort) && (strings.Contains(line, sigHidden) || strings.Contains(line, sigLocale)) {
				// Извлекаем номер правила через общую функцию (DRY)
				numStr := extractRuleNumber(line)
				if numStr != "" {
					// БЛОК 3.1: удаление больше не слепое.
					delRes := sys.RunTimeout(10*time.Second, "ufw", "--force", "delete", numStr)
					if delRes.Err != nil {
						delRes.LogWarnIfErr("PORT_CASTLING", "ANTI_LOCKOUT_REMOVE", "event=ufw.castling_rule_delete_failed")
					} else {
						sys.LogAction("PORT_CASTLING", "ANTI_LOCKOUT_REMOVE", "SUCCESS",
							i18n.TrEventf("port_castling.anti_lockout_remove", customPort, numStr))
					}
				}
			}
		}
	}

	// ═══════════════════════════════════════════════════════════════════════════════
	// STATEFUL-МИГРАЦИЯ ЛИМИТОВ (v8.2.4): взводим броню ДО переключения юнитов.
	// UFW-лимитер не зависит от sshd, поэтому порт 22 защищён с момента открытия.
	// ═══════════════════════════════════════════════════════════════════════════════
	if limiterWasActive {
		if err := SetCustomRateLimit(defaultPortInt, "open_ssh", "Open SSH", "ON"); err != nil {
			sys.LogAction("PORT_CASTLING", "LIMITER_RESTORE", "WARNING",
				i18n.TrEventf("port_castling.limiter_restore_failed", 22, err))
		} else {
			sys.LogAction("PORT_CASTLING", "LIMITER_RESTORE", "SUCCESS",
				i18n.TrEvent("port_castling.limiter_restore_22"))
		}
	}

	// Возвращаем строгое комментирование UFW по корпоративному стандарту v6.0.0
	// (DRY: общий helper с откатом рокировки ON, см. restorePort22Perimeter)
	restorePort22Perimeter()

	time.Sleep(sleepTime)

	// ═══════════════════════════════════════════════════════════════════════════════
	// РЕАНИМАЦИЯ СЛУЖБ UBUNTU 24.04 (v8.2.4): исключаем Race Condition
	// между ssh.service и ssh.socket за порт 22.
	// ═══════════════════════════════════════════════════════════════════════════════
	unmaskRes := sys.RunTimeout(10*time.Second, "systemctl", "unmask", "ssh.socket")
	unmaskRes.LogWarnIfErr("PORT_CASTLING", "SOCKET_UNMASK", "event=systemd.socket_unmask_failed")

	enableSocketRes := sys.RunTimeout(10*time.Second, "systemctl", "enable", "ssh.socket")
	if enableSocketRes.Err != nil {
		enableSocketRes.LogWarnIfErr("PORT_CASTLING", "SOCKET_UNMASK", "event=systemd.socket_enable_failed")
	} else {
		sys.LogAction("PORT_CASTLING", "SOCKET_UNMASK", "SUCCESS", i18n.TrEvent("port_castling.socket_unmask"))
	}

	reloadOffSysd := sys.RunTimeout(10*time.Second, "systemctl", "daemon-reload")
	reloadOffSysd.LogWarnIfErr("PORT_CASTLING", "SOCKET_UNMASK", "event=systemd.daemon_reload_failed")

	// Освобождаем порт 22: останавливаем классическую службу ДО передачи порта сокету.
	stopSshServiceRes := sys.RunTimeout(20*time.Second, "systemctl", "stop", "ssh.service")
	if stopSshServiceRes.Err != nil {
		stopSshServiceRes.LogWarnIfErr("PORT_CASTLING", "SSH_SERVICE_STOP", "event=systemd.ssh_stop_failed")
	} else {
		sys.LogAction("PORT_CASTLING", "SSH_SERVICE_STOP", "SUCCESS",
			i18n.TrEvent("port_castling.ssh_service_stop"))
	}

	// Передаём управление портом сокет-активатору: он делает bind() на чистый порт 22.
	// Критичная команда: сбой сокета = порт 22 без слушателя = SSH недоступен.
	// Поэтому: ALERT в файл ОБЯЗАТЕЛЬНО, затем ошибка наружу (точка невозврата пройдена,
	// но оператор обязан увидеть сбой и поднять ssh.service вручную).
	startSocketOffRes := sys.RunTimeout(10*time.Second, "systemctl", "start", "ssh.socket")
	if startSocketOffRes.Err != nil {
		stderr := strings.TrimSpace(startSocketOffRes.Stderr)
		// Классическая схема: в системе нет юнита сокета (not found) —
		// падаем в прямой старт ssh.service, иначе хост останется без SSH.
		if strings.Contains(stderr, "not found") || strings.Contains(startSocketOffRes.Err.Error(), "not found") {
			sys.LogAction("PORT_CASTLING", "SSH_SERVICE_FALLBACK", "WARNING",
				i18n.TrEvent("port_castling.ssh_service_fallback"))
			fallbackRes := sys.RunTimeout(20*time.Second, "systemctl", "start", "ssh.service")
			if fallbackRes.Err != nil {
				sys.LogAction("PORT_CASTLING", "SSH_SOCKET_RESTORE", "ALERT",
					i18n.TrEventf("port_castling.ssh_socket_restore_failed",
						fallbackRes.Err, strings.TrimSpace(fallbackRes.Stderr)))
				return fmt.Errorf(i18n.T.PortMoveSshSocketRestoreFailed,
					fallbackRes.Err, strings.TrimSpace(fallbackRes.Stderr))
			}
		} else {
			sys.LogAction("PORT_CASTLING", "SSH_SOCKET_RESTORE", "ALERT",
				i18n.TrEventf("port_castling.ssh_socket_restore_failed", startSocketOffRes.Err, stderr))
			return fmt.Errorf(i18n.T.PortMoveSshSocketRestoreFailed, startSocketOffRes.Err, stderr)
		}
	} else {
		sys.LogAction("PORT_CASTLING", "SSH_SOCKET_RESTORE", "SUCCESS",
			i18n.TrEvent("port_castling.ssh_socket_restore"))
	}

	time.Sleep(restartSleep) // Фиксация стабилизации системы

	// ─────────────────────────────────────────────────────────────
	// R-8: журнал не умеет молчать о результате обратной рокировки.
	// Финальная запись SUCCESS ставится ТОЛЬКО после того, как контрольное
	// соединение убедилось: на порту 22 действительно есть слушатель.
	// ─────────────────────────────────────────────────────────────
	if err := waitForSshListener(defaultPortInt, 5, 400*time.Millisecond); err != nil {
		sys.LogAction("PORT_CASTLING", "SSH_SERVICE_RESTORE", "ALERT",
			i18n.TrEventf("port_castling.ssh_listener_verify_failed", 22, err))
		return fmt.Errorf(i18n.T.PortMoveSshListenerVerifyFailed, 22, err)
	}
	sys.LogAction("PORT_CASTLING", "SSH_SERVICE_RESTORE", "SUCCESS",
		i18n.TrEvent("port_castling.ssh_service_restore"))

	// ВАРИАНТ А (v8.4.6): СИНХРОННЫЙ перескан после обратной рокировки —
	// инвентарь обязан увидеть sshd, вернувшийся на 22.
	// (Аналогично фазе ON: убираем `go func()` для корректной работы из CLI).
	func() {
		defer func() {
			if r := recover(); r != nil {
				sys.LogAction("PORT_CASTLING", "PERIMETRE_RESCAN", "ALERT",
					i18n.TrEventf("port_castling.perimetre_rescan_panic", r))
			}
		}()
		if err := config.RunPerimeterInventoryScan(); err != nil {
			sys.LogAction("PORT_CASTLING", "PERIMETRE_RESCAN", "WARNING",
				i18n.TrEventf("port_castling.perimetre_rescan_failed", err))
		} else {
			sys.LogAction("PORT_CASTLING", "PERIMETRE_RESCAN", "SUCCESS",
				i18n.TrEvent("port_castling.perimetre_rescan_off"))
		}
	}()

	return nil
}

// ═══════════════════════════════════════════════════════════════════════════════
// ВСПОМОГАТЕЛЬНЫЕ ФУНКЦИИ (без изменений)
// ═══════════════════════════════════════════════════════════════════════════════

// modifySshConfig изменяет конфигурацию sshd_config на заданный порт.
// v8.2.0 (1.8.3): если строка `Port` не найдена — добавляем в конец.
// v8.2.0 (1.8.4): права файла = 0600 (только root).
func modifySshConfig(port string) error {
	input, err := os.ReadFile("/etc/ssh/sshd_config")
	if err != nil {
		return fmt.Errorf(i18n.T.PortMoveSshdConfigReadFailed, err)
	}
	lines := strings.Split(string(input), "\n")

	found := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Ловим все варианты написания: "Port ", "#Port ", "# Port "
		if strings.HasPrefix(trimmed, "Port ") || strings.HasPrefix(trimmed, "#Port ") || strings.HasPrefix(trimmed, "# Port ") {
			if !found {
				// Первая найденная директива становится активной
				lines[i] = "Port " + port
				found = true
			} else {
				// Все последующие принудительно комментируем (защита от дубликатов в .d)
				if strings.HasPrefix(trimmed, "Port ") {
					lines[i] = "# " + trimmed
				}
			}
		}
	}

	if !found {
		lines = append(lines, "Port "+port)
	}
	if err := os.WriteFile("/etc/ssh/sshd_config", []byte(strings.Join(lines, "\n")), 0600); err != nil {
		return fmt.Errorf(i18n.T.PortMoveSshdConfigWriteFailed, err)
	}
	return nil
}

// waitForSshListener — контрольная проверка наличия слушателя на порту (БЛОК 3.1: SUCCESS не вслепую).
// Делаем несколько попыток с паузой: systemd-сокет-активатор может занять порт
// не мгновенно, а спустя доли секунды после start.
// Успех TCP-подключения = порт действительно удерживается слушателем.
// Контрольное соединение закрывается сразу, без обмена аутентификации.
func waitForSshListener(port, attempts int, pause time.Duration) error {
	var lastErr error
	for i := 0; i < attempts; i++ {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 2*time.Second)
		if err == nil {
			// Connect принял backlog systemd — это ещё не доказательство жизни sshd.
			// Доказательство — приветственный баннер: живой sshd шлёт его сразу.
			_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
			buf := make([]byte, len("SSH-2.0-"))
			_, readErr := io.ReadFull(conn, buf)
			_ = conn.Close()
			if readErr == nil && (string(buf) == "SSH-2.0-" || string(buf) == "SSH-1.99-") {
				return nil // сервер OpenSSH действительно отвечает
			}
			lastErr = fmt.Errorf("баннер OpenSSH не получен: read=%q err=%v", string(buf), readErr)
		} else {
			lastErr = err
		}
		time.Sleep(pause)
	}
	return lastErr
}

// restorePort22Perimeter — возврат правил UFW порта 22 по корпоративному стандарту v6.0.0.
// Вызывается из двух мест: обратная рокировка OFF и откат рокировки ON (R-8).
// БЛОК 3.1: возврат периметра порта 22 больше не слепой.
func restorePort22Perimeter() {
	if errTarpit := SetTarpitProtocol("CHECK"); errTarpit == nil {
		allow22Res := sys.RunTimeout(10*time.Second, "ufw", "allow", "22/tcp", "comment", "Amber Guard Lab. Active Tarpit Station")
		allow22Res.LogWarnIfErr("PORT_CASTLING", "PERIMETER_RESTORE", "event=ufw.tarpit_allow_failed")
	} else {
		allow22Res := sys.RunTimeout(10*time.Second, "ufw", "allow", "22/tcp", "comment", "Amber Guard Lab. SSH Standard")
		allow22Res.LogWarnIfErr("PORT_CASTLING", "PERIMETER_RESTORE", "event=ufw.standard_allow_failed")
	}
	reloadRes := sys.RunTimeout(10*time.Second, "ufw", "reload")
	reloadRes.LogWarnIfErr("PORT_CASTLING", "PERIMETER_RESTORE", "event=ufw.reload_failed")
}

// ensureWatcherUnit генерирует и устанавливает юнит `agld_watcher.service`,
// если он ещё не существует. Вызывается при кастлинге ON.
// Паттерн: аналогично `agld_tarpit.service` в `tarpitctl`.
func ensureWatcherUnit() error {
	const servicePath = "/etc/systemd/system/agld_watcher.service"

	// Если юнит уже существует — ничего не делаем
	if _, err := os.Stat(servicePath); err == nil {
		return nil
	}

	// Определяем путь к бинарнику
	binaryPath := config.FileBinary
	serviceContent := fmt.Sprintf(`[Unit]
Description=Amber Guard Lab. - Admin Idle Watcher (Anti-Lockout Incineration)
After=network-online.target
Wants=network-online.target

[Service]
Type=exec
User=root
ExecStart=%s --watcher
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
`, binaryPath)

	// Записываем юнит на диск
	if err := os.WriteFile(servicePath, []byte(serviceContent), 0644); err != nil {
		return fmt.Errorf(i18n.T.PortMoveWatcherUnitWriteFailed, err)
	}
	return nil
}
