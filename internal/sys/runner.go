// internal/sys/runner.go
package sys

import (
	"agl/i18n"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// БЛОК 3.1: ЕДИНЫЙ ИНТЕРФЕЙС ИСПОЛНЕНИЯ ВНЕШНИХ КОМАНД
// ─────────────────────────────────────────────────────────────────────────────
//
// PURPOSE:
//   Заменить слепые вызовы вида `_, _ = sys.RunCmdTimeout(...)`
//   на структурированный результат с отдельными stdout, stderr и ошибкой.
//
// CONTRACT:
//   - Команда всегда выполняется с таймаутом.
//   - Таймаут нельзя случайно потерять, потому что контекст убивает процесс.
//   - Ошибка не игнорируется: вызывающий код обязан проверить `Err`.
//   - Для некритичных сбоев используется машинное событие через `LogAction`.
//
// SIDE-EFFECTS:
//   - Запускает внешний процесс.
//   - При превышении таймаута процесс получает сигнал завершения через
//     `exec.CommandContext`.
// ─────────────────────────────────────────────────────────────────────────────

// CmdResult — стандартизированный результат внешней команды.
//
// Поля:
//   - Stdout: стандартный вывод команды.
//   - Stderr: стандартный поток ошибок команды.
//   - Err:    ошибка запуска, ненулевой код выхода или таймаут.
type CmdResult struct {
	Stdout string
	Stderr string
	Err    error
}

// RunTimeout выполняет внешнюю команду с жёстким таймаутом.
//
// Это основная безопасная замена для старых вызовов RunCmdTimeout.
// Таймаут обязателен, потому что внешние утилиты могут зависнуть:
//   - ufw может ждать блокировку;
//   - systemctl может ждать D-Bus;
//   - ss может деградировать вместе с вводом-выводом;
//   - geoiplookup может отсутствовать или тормозить.
func RunTimeout(timeout time.Duration, name string, args ...string) CmdResult {
	// Защита от нулевого или отрицательного таймаута.
	// Если вызывающий код забыл указать таймаут, даём безопасный дефолт.
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	// Создаём контекст с дедлайном. Когда таймаут истекает,
	// exec.CommandContext завершает дочерний процесс.
	ctx, cancel := context.WithTimeout(context.Background(), timeout)

	// cancel обязан вызываться всегда, иначе возможен потерянный контекст.
	// Это также закрывает ресурсы контекста после завершения функции.
	defer cancel()

	// Команда привязывается к контексту: таймаут управляется через него.
	cmd := exec.CommandContext(ctx, name, args...)

	// Разделяем stdout и stderr.
	// Это лучше, чем CombinedOutput, потому что ошибки часто пишутся в stderr,
	// а полезный вывод — в stdout. Так логи становятся точнее.
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Запускаем и ждём завершения.
	// Если команда завершается с ненулевым кодом, `err` будет ненулевым.
	err := cmd.Run()

	// Если сработал именно дедлайн, помечаем ошибку как таймаут.
	// Это даёт понятный машинный текст в журналах вместо «какой-то сигнал».
	if ctx.Err() == context.DeadlineExceeded {
		if err != nil {
			err = fmt.Errorf(i18n.T.SysRunnerTimeout, name, timeout, err)
		} else {
			err = context.DeadlineExceeded
		}
	}

	// Возвращаем структурированный результат.
	return CmdResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
		Err:    err,
	}
}

// Run — интерфейс, совместимый с формулировкой аудитора.
//
// ВАЖНО: он НЕ является «голым» exec.Command без таймаута.
// Внутри используется безопасный дефолт 10 секунд, чтобы сохранить
// архитектурный принцип M3d и не откатиться к зависшим внешним командам.
func Run(name string, args ...string) CmdResult {
	return RunTimeout(10*time.Second, name, args...)
}

// LogWarnIfErr логирует некритичную ошибку внешней команды.
//
// Используется там, где сбой команды не должен рвать сценарий,
// но обязан быть видимым в журнале через машинный код события.
//
// Параметры:
//   - component: подсистема, например "ARMOR" или "PORT_CASTLING".
//   - op:        короткое имя операции, например "H1_TOMB_CLEANUP".
//   - event:     машинный код события, например "event=ufw.status_failed".
func (r CmdResult) LogWarnIfErr(component, op, event string) {
	// Если ошибки нет — событие не пишем, чтобы не спамить журнал.
	if r.Err == nil {
		return
	}

	// Сначала пытаемся показать stderr: там обычно причина сбоя.
	details := strings.TrimSpace(r.Stderr)

	// Если stderr пуст, но команда что-то сказала в stdout,
	// используем stdout как диагностический материал.
	if details == "" {
		details = strings.TrimSpace(r.Stdout)
	}

	// Переводим строки в одну, чтобы событие оставалось однострочным.
	// Это важно для машинной обработки журнала.
	details = strings.ReplaceAll(details, "\n", " | ")

	// Обрезаем слишком длинные выводы, чтобы не раздувать лог.
	if len(details) > 300 {
		details = details[:300] + "..."
	}

	// Нормализуем машинный код события.
	// Вызывающий может передать либо полный код вида "event=ufw.status_failed",
	// либо короткий код вида "ufw.status_failed".
	// Это защищает журнал от двойного префикса "event=event=...".
	eventCode := strings.TrimSpace(event)
	if !strings.HasPrefix(eventCode, "event=") {
		eventCode = "event=" + eventCode
	}

	// Пишем WARNING с машинным кодом события.
	// Формат: event=... err=... details=...
	LogAction(component, op, "WARNING", fmt.Sprintf("%s err=%v details=%s", eventCode, r.Err, details))
}

// RunTimeoutStreamed выполняет внешнюю команду с жёстким таймаутом,
// НО потоково передаёт stdout/stderr прямо в консоль (os.Stdout/os.Stderr).
//
// PURPOSE:
//
//	Долгие интерактивные операции (установка пакетов), где администратор
//	обязан видеть живой прогресс. В отличие от RunTimeout, вывод НЕ
//	накапливается в буферах — он идёт на терминал в реальном времени.
//
// CONTRACT:
//   - Таймаут обязателен: по дедлайну дочерний процесс убивается.
//   - Возвращается ошибка команды (включая обёрнутый таймаут).
//
// SIDE-EFFECTS:
//   - Запускает внешний процесс.
//   - Пишет его вывод напрямую в консоль процесса AGL.
func RunTimeoutStreamed(timeout time.Duration, name string, args ...string) error {
	// Защита от нулевого или отрицательного таймаута.
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	// Контекст с дедлайном: при истечении таймаута процесс завершается.
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	// cancel вызывается всегда — без потерянных контекстов.
	defer cancel()

	// Команда привязана к контексту.
	cmd := exec.CommandContext(ctx, name, args...)

	// Живой стриминг вывода в консоль: админ видит прогресс установки.
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Запускаем и ждём завершения.
	err := cmd.Run()

	// Если сработал именно дедлайн — помечаем ошибку как таймаут,
	// чтобы в журналах была понятная причина, а не «какой-то сигнал».
	if ctx.Err() == context.DeadlineExceeded {
		if err != nil {
			err = fmt.Errorf(i18n.T.SysRunnerTimeout, name, timeout, err)
		} else {
			err = context.DeadlineExceeded
		}
	}
	return err
}

// WriteAtomic выполняет атомарную запись данных в файл.
//
// PURPOSE:
//
//	Гарантировать, что файл-маркер или файл состояния либо записан полностью и корректно,
//	либо не записан вовсе. Защищает от частичной записи при сбое питания или панике.
//
// CONTRACT:
//   - Пишет во временный файл (.tmp) в той же директории.
//   - Делает f.Sync() для сброса буферов ОС на диск.
//   - Переименовывает (os.Rename) временный файл в целевой (атомарная операция в POSIX).
//   - При любой ошибке временный файл удаляется.
func WriteAtomic(path string, data []byte, perm os.FileMode) error {
	tmpPath := path + ".tmp"

	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}

	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return err
	}

	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return err
	}

	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}

	return os.Rename(tmpPath, path)
}
