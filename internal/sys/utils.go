// internal/sys/utils.go
package sys

import (
	"agl/i18n"
	"context"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// v8.2.0 (2.8): КЭШ ДЕТЕКЦИИ SSH-ПОРТА
// ─────────────────────────────────────────────────────────────────────────────
// DetectActiveSshPort вызывается при каждом рендере главного меню TUI (1 Гц).
// Без кэша это означало бы дёрганье `ss -lntp` каждую секунду.
// Кэш обновляется раз в 10 сек — достаточно для диагностики расхождений,
// но не нагружает систему.
var (
	sshPortCacheMu  sync.Mutex
	sshPortCacheAt  time.Time
	sshPortCacheVal string
	sshPortCacheTTL = 10 * time.Second
	// v8.2.0 (2.8-ext): дедупликация логов PORT_MISMATCH (edge-triggered).
	// Пишем WARNING только при НОВОМ расхождении и SUCCESS при сходимости.
	lastMismatchConfig string
	lastMismatchReal   string

	// v8.2.2 (3.1): edge-triggered флаг деградации опроса `ss`.
	// Гонок нет: detectActiveSshPortUncached вызывается под sshPortCacheMu.
	ssDegraded bool
)

// PrettyServiceName — v8.2.0 (2.5-ext): локальная замена deprecated strings.Title.
// Приводит строку к нижнему регистру и поднимает первый символ, а также каждый
// символ после не-буквенно-цифрового разделителя («xray-linux-amd6» → «Xray-Linux-Amd6»).
// Побайтово совпадает со старой трансформацией для ASCII-имён служб,
// поэтому UFW-комментарии не расходятся со старыми правилами. Без x/text.
func PrettyServiceName(s string) string {
	b := []byte(strings.ToLower(s))
	up := true
	for i := range b {
		c := b[i]
		switch {
		case c >= 'a' && c <= 'z':
			if up {
				b[i] -= 32 // ASCII: нижняя → верхняя
			}
			up = false
		case c >= '0' && c <= '9':
			up = false
		default:
			up = true // разделитель: следующую букву поднимаем
		}
	}
	return string(b)
}

// DetectActiveSshPort определяет, какой порт SSH сейчас реально слушает система.
// v8.2.0 (2.8): кэш 10 сек + сверка с реальным listening-портом через `ss`.
// Приоритет: конфиг (намерение админа); `ss` — диагностика расхождений.
func DetectActiveSshPort() string {
	sshPortCacheMu.Lock()
	defer sshPortCacheMu.Unlock()
	if time.Since(sshPortCacheAt) < sshPortCacheTTL {
		return sshPortCacheVal
	}
	val := detectActiveSshPortUncached()
	sshPortCacheVal = val
	sshPortCacheAt = time.Now()
	return val
}

// detectActiveSshPortUncached — внутренняя реализация детекции без кэша.
// Читает конфиг + сверяет с реальным listening-портом через `ss -lntp`.
// При расхождении — ОДИН edge-triggered WARNING, без спама.
// ВАЖНО (граница владения): функция ТОЛЬКО диагностирует и возвращает
// порт из конфига (намерение админа). AGL НИКОГДА не правит sshd_config
// автономно — запись в системный конфиг происходит только внутри
// SetMovePortProtocol (явная операция рокировки под рукой админа).
func detectActiveSshPortUncached() string {
	// Шаг 1: читаем конфиг (как было до v8.2.0)
	configPort := "22" // дефолт безопасности
	input, err := os.ReadFile("/etc/ssh/sshd_config")
	if err == nil {
		lines := strings.Split(string(input), "\n")
		for _, line := range lines {
			cleanLine := strings.TrimSpace(line)
			if strings.HasPrefix(cleanLine, "Port ") {
				portValue := strings.TrimSpace(strings.TrimPrefix(cleanLine, "Port "))
				if portValue != "" {
					configPort = portValue
					break
				}
			}
		}
	}

	// Шаг 2: v8.2.0 (2.8) — сверка с реальным listening-портом через `ss`.
	// Ищем строку вида ":<порт>.*sshd" в выводе `ss -lntp`.
	// Если конфиг и реальность расходятся — логируем, но возвращаем конфиг.
	// БЛОК 3.1: сверка через единый интерфейс RunTimeout (тот же пакет).
	// Сбой `ss` больше не молчит полностью: один edge-triggered WARNING
	// на переход в деградацию, один SUCCESS на восстановление.
	ssRes := RunTimeout(5*time.Second, "ss", "-lntp", "-H")
	if ssRes.Err != nil {
		if !ssDegraded {
			ssDegraded = true
			// event=ss.probe_failed — машинный код сбоя диагностики.
			ssRes.LogWarnIfErr("SSH_DETECT", "SS_PROBE", "event=ss.probe_failed")
		}
		// Диагностика недоступна: возвращаем намерение админа (конфиг).
		return configPort
	}
	// `ss` снова отвечает — закрываем ребро деградации одним SUCCESS.
	if ssDegraded {
		ssDegraded = false
		LogAction("SSH_DETECT", "SS_PROBE", "SUCCESS",
			i18n.TrEvent("ss.probe_restored"))
	}
	// Регулярка ищет ":<порт>" в адресе прослушивания + "sshd" в имени процесса
	re := regexp.MustCompile(`:(\d+)\s+.*users:\(\("sshd"`)
	matches := re.FindAllStringSubmatch(ssRes.Stdout, -1)
	if len(matches) > 0 {
		// Берём первый найденный порт (обычно sshd слушает один порт)
		realPort := matches[0][1]
		if realPort != configPort {
			// v8.2.0 (2.8-ext): логируем только НОВОЕ расхождение
			if realPort != lastMismatchReal || configPort != lastMismatchConfig {
				LogAction("SSH_DETECT", "PORT_MISMATCH", "WARNING",
					i18n.TrEventf("ssh_detect.port_mismatch", configPort, realPort))
				lastMismatchConfig = configPort
				lastMismatchReal = realPort
			}
		} else if lastMismatchReal != "" {
			// Расхождение исчезло — один SUCCESS и сброс состояния
			LogAction("SSH_DETECT", "PORT_MISMATCH_RESOLVED", "SUCCESS",
				i18n.TrEventf("ssh_detect.port_mismatch_resolved", configPort))
			lastMismatchConfig = ""
			lastMismatchReal = ""
		}
	}
	return configPort
}

// RunCmdTimeout — внешний вызов с жёстким таймаутом (M3d).
// Защищает вечные циклы (вотчер, демон) и TUI от зависших внешних
// процессов: ufw на xtables-локе, systemctl на D-Bus, who/ss при деградации I/O.
// По таймауту дочерний процесс получает SIGKILL, вызывающий получает ошибку.
func RunCmdTimeout(timeout time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}
