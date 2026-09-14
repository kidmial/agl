// internal/armor/sysctl_tune_protocol.go
package armor

import (
	"agl/config"
	"agl/i18n"
	"agl/internal/sys" // ИНТЕГРАЦИЯ АУДИТОРА v6.4.0
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// runSysctlCmd — безопасная нативная обёртка для записи параметров в ядро Linux.
//
// БЛОК 3.1: внешний вызов переведён на единый интерфейс sys.RunTimeout:
//   - жёсткий таймаут 5 секунд защищает протокол от зависшего sysctl
//     (деградация I/O на /proc/sys, блокировки);
//   - ошибка записи больше не игнорируется: каждый несостоявшийся параметр
//     оставляет WARNING с машинным кодом event=sysctl.write_failed.
//
// Сбой одного параметра НЕ прерывает протокол: броня накатывается частично,
// а администратор видит деградацию в журнале и может дозапустить ON.
func runSysctlCmd(param, value string) {
	// Выполняем запись в ядро через единый интерфейс с таймаутом.
	// stdout/stderr разделяются, ошибка структурирована.
	res := sys.RunTimeout(5*time.Second, "sysctl", "-w", fmt.Sprintf("%s=%s", param, value))

	// LogWarnIfErr сам проверяет Err: при успехе — ничего не пишет,
	// при сбое — WARNING с именем параметра и обрезанным stderr.
	// Имя параметра включаем в код события для точечной диагностики.
	res.LogWarnIfErr("SYSCTL_TUNE", "SYSCTL_WRITE",
		fmt.Sprintf("event=sysctl.write_failed param=%s", param))
}

// ─────────────────────────────────────────────────────────────
// v8.2.0 (2.6): БАЗЛАЙН ПАРАМЕТРОВ ЯДРА.
// OFF восстанавливает ДОБРОННОЕ состояние ядра вместо хардкода,
// который отключал rp_filter (anti-spoofing) и расходился с эталонами.
// ─────────────────────────────────────────────────────────────

// sysctlBaselinePath — слепок значений ядра до наката брони.
var sysctlBaselinePath = filepath.Join(config.DirLib, "sysctl_baseline.json")

// sysctlParams — упорядоченный список параметров, управляемых протоколом.
var sysctlParams = []string{
	"net.ipv4.tcp_syncookies",
	"net.ipv4.tcp_fin_timeout",
	"net.ipv4.tcp_syn_retries",
	"net.ipv4.tcp_synack_retries",
	"net.core.somaxconn",
	"net.ipv4.tcp_max_syn_backlog",
	"net.ipv4.conf.all.rp_filter",
	"net.ipv4.conf.default.rp_filter",
}

// readSysctlParam читает текущее значение параметра из ядра Linux.
//
// БЛОК 3.1: чтение переведено на единый интерфейс sys.RunTimeout.
// Ошибка обогащается stderr внешней команды — вызывающий код
// (saveSysctlBaseline) логирует её в BASELINE_READ_FAIL с полной диагностикой.
func readSysctlParam(param string) (int, error) {
	// Читаем значение параметра с таймаутом 5 секунд.
	res := sys.RunTimeout(5*time.Second, "sysctl", "-n", param)

	// При сбое возвращаем ошибку с контекстом: параметр, причина, stderr.
	// Это даёт читаемую диагностику в журнале базлайна.
	if res.Err != nil {
		return 0, fmt.Errorf("sysctl -n %s: %w; stderr=%s",
			param, res.Err, strings.TrimSpace(res.Stderr))
	}

	// Разбираем целое из stdout (обрезаем пробелы и перевод строки).
	return strconv.Atoi(strings.TrimSpace(res.Stdout))
}

// saveSysctlBaseline снимает слепок текущих значений ядра.
// Вызывается при ON. НЕ перезаписывает существующий слепок:
// иначе повторный ON сохранил бы боевые значения как «базлайн»,
// и OFF вернул бы броню вместо пре-броневого состояния.
func saveSysctlBaseline() {
	if _, err := os.Stat(sysctlBaselinePath); err == nil {
		return // базлайн уже снят до первой брони
	}
	base := make(map[string]int, len(sysctlParams))
	for _, p := range sysctlParams {
		v, err := readSysctlParam(p)
		if err != nil {
			sys.LogAction("SYSCTL_TUNE", "BASELINE_READ_FAIL", "WARNING",
				i18n.TrEventf("sysctl_tune.baseline_read_fail", p, err))
			continue
		}
		base[p] = v
	}
	data, err := json.MarshalIndent(base, "", "  ")
	if err != nil {
		return
	}
	if err := sys.WriteAtomic(sysctlBaselinePath, data, 0600); err != nil {
		sys.LogAction("SYSCTL_TUNE", "BASELINE_WRITE_FAIL", "WARNING",
			i18n.TrEventf("sysctl_tune.baseline_write_fail", err))
		return
	}
	sys.LogAction("SYSCTL_TUNE", "BASELINE_SAVED", "SUCCESS",
		i18n.TrEventf("sysctl_tune.baseline_saved", len(base)))
}

// restoreSysctlBaseline возвращает ядро в пре-броневое состояние:
// значения из базлайна; при отсутствии файла или ключа —
// заводские эталоны из config.C.SysctlCore.Default*.
func restoreSysctlBaseline() {
	// v8.2.2 (волна гонки 2.3): копия значения вместо указателя в снепшот —
	// чтения согласованы, случайная мутация через s.X невозможна.
	s := config.C.SysctlCore
	defaults := map[string]int{
		"net.ipv4.tcp_syncookies":         s.DefaultNetIpv4TcpSyncookies,
		"net.ipv4.tcp_fin_timeout":        s.DefaultNetIpv4TcpFinTimeout,
		"net.ipv4.tcp_syn_retries":        s.DefaultNetIpv4TcpSynRetries,
		"net.ipv4.tcp_synack_retries":     s.DefaultNetIpv4TcpSynackRetries,
		"net.core.somaxconn":              s.DefaultNetCoreSomaxconn,
		"net.ipv4.tcp_max_syn_backlog":    s.DefaultNetIpv4TcpMaxSynBacklog,
		"net.ipv4.conf.all.rp_filter":     s.DefaultNetIpv4ConfAllRpFilter,
		"net.ipv4.conf.default.rp_filter": s.DefaultNetIpv4ConfDefaultRpFilter,
	}
	base := make(map[string]int)
	fromBaseline := false
	if data, err := os.ReadFile(sysctlBaselinePath); err == nil {
		if json.Unmarshal(data, &base) == nil {
			fromBaseline = true
		}
	}
	for _, p := range sysctlParams {
		v, ok := base[p]
		if !fromBaseline || !ok {
			v = defaults[p]
		}
		runSysctlCmd(p, strconv.Itoa(v))
	}
	_ = os.Remove(sysctlBaselinePath)
	src := i18n.TrEvent("sysctl_tune.restore_src_baseline")
	if !fromBaseline {
		src = i18n.TrEvent("sysctl_tune.restore_src_factory")
	}
	sys.LogAction("SYSCTL_TUNE", "RESTORE_BASELINE_CORE", "SUCCESS",
		i18n.TrEventf("sysctl_tune.restore_baseline_core", src))
}

// SetSysctlProtocol осуществляет низкоуровневую оптимизацию и защиту сетевого стека ядра.
func SetSysctlProtocol(action string) error {
	// Летучий маркер состояния в ОЗУ tmpfs по стандартам Amber Guard Lab
	markerFile := filepath.Join(config.DirRun, "agl_sysctl.state")

	// Быстрая ссылка на контур боевых настроек ядра из config.json
	// v8.2.2 (волна гонки 2.3): копия значения вместо указателя в снепшот.
	s := config.C.SysctlCore
	switch strings.ToUpper(action) {
	case "ON":
		// v8.2.0 (2.6): слепок пре-броневого состояния ядра — до наката.
		saveSysctlBaseline()
		// Накатываем параметры активной защиты, динамически считывая их из синглтона конфигурации
		runSysctlCmd("net.ipv4.tcp_syncookies", strconv.Itoa(s.ArmorNetIpv4TcpSyncookies))
		runSysctlCmd("net.ipv4.tcp_fin_timeout", strconv.Itoa(s.ArmorNetIpv4TcpFinTimeout))
		runSysctlCmd("net.ipv4.tcp_syn_retries", strconv.Itoa(s.ArmorNetIpv4TcpSynRetries))
		runSysctlCmd("net.ipv4.tcp_synack_retries", strconv.Itoa(s.ArmorNetIpv4TcpSynackRetries))
		runSysctlCmd("net.core.somaxconn", strconv.Itoa(s.ArmorNetCoreSomaxconn))
		runSysctlCmd("net.ipv4.tcp_max_syn_backlog", strconv.Itoa(s.ArmorNetIpv4TcpMaxSynBacklog))
		runSysctlCmd("net.ipv4.conf.all.rp_filter", strconv.Itoa(s.ArmorNetIpv4ConfAllRpFilter))
		runSysctlCmd("net.ipv4.conf.default.rp_filter", strconv.Itoa(s.ArmorNetIpv4ConfDefaultRpFilter))

		// Фиксируем маркер успешной активации брони в ОЗУ
		// АУДИТ: Фиксируем точный числовой слепок параметров, улетевших в ядро Linux
		sys.LogAction("SYSCTL_TUNE", "APPLY_CORE_PROTECTION", "SUCCESS",
			i18n.TrEventf("sysctl_tune.apply_core_protection",
				s.ArmorNetIpv4TcpSyncookies, s.ArmorNetIpv4TcpFinTimeout, s.ArmorNetIpv4TcpMaxSynBacklog, s.ArmorNetCoreSomaxconn))

		_ = sys.WriteAtomic(markerFile, []byte("ON"), 0644)
		return nil

	case "OFF":
		// Возвращаем стандартные дефолтные параметры «чистой» ОС Linux при деактивации Брони
		// v8.2.0 (2.6): восстановление пре-броневого состояния (базлайн)
		restoreSysctlBaseline()
		_ = os.Remove(markerFile)
		return nil

	case "CHECK":
		// Проверяем наличие рантайм-маркера в ОЗУ. Если файла нет — оптимизация выключена
		if _, err := os.Stat(markerFile); err != nil {
			return fmt.Errorf("%s", i18n.T.SysctlOptimizationDisabled)
		}

		// Для исключения ложных срабатываний опрашиваем реальное текущее
		// значение таймаута в ядре Linux.
		// БЛОК 3.1: опрос через единый интерфейс sys.RunTimeout,
		// без прямого exec.Command и без ручного буфера.
		res := sys.RunTimeout(5*time.Second, "sysctl", "-n", "net.ipv4.tcp_fin_timeout")

		// Если чтение не подтверждено, состояние ядра проверить невозможно.
		// Логируем WARNING с машинным кодом и честно отдаём ошибку статуса.
		if res.Err != nil {
			res.LogWarnIfErr("SYSCTL_TUNE", "SYSCTL_CHECK_READ",
				"event=sysctl.read_failed param=net.ipv4.tcp_fin_timeout")
			return fmt.Errorf("%s", i18n.T.SysctlKernelIntervalsMismatch)
		}

		// Разбираем реальное значение из stdout.
		currentTimeoutStr := strings.TrimSpace(res.Stdout)
		currentTimeout, err := strconv.Atoi(currentTimeoutStr)

		// Если реальное значение в ядре совпадает с тем, что администратор
		// выставил в конфиге — статус валиден.
		if err == nil && currentTimeout == s.ArmorNetIpv4TcpFinTimeout {
			return nil // Успешно, защита ядра полностью активна и соответствует конфигу
		}

		// АУДИТ ИБ-ИНЦИДЕНТА: Фиксируем рассинхронизацию, если ядро изменили в обход Laboratory
		sys.LogAction("SYSCTL_TUNE", "CORE_MISCONFIGURATION", "ALERT",
			i18n.TrEventf("sysctl_tune.core_misconfiguration", currentTimeout, s.ArmorNetIpv4TcpFinTimeout))
		return fmt.Errorf("%s", i18n.T.SysctlKernelIntervalsMismatch)
	}

	return fmt.Errorf("%s", fmt.Sprintf(i18n.T.SysctlUnknownAction, action))
}
