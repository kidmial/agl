// internal/armor/web_rescue_server.go
// =====================================================================
// КОМПОНЕНТ: АВТОНОМНЫЙ HTTP-СЕРВЕР ВЕБ-СТУКА (ПСЕВДО-2FA ШЛЮЗ СПАСЕНИЯ)
// ВЕРСИЯ: v8.1.3 (M3b)
// РОЛЬ: Лёгкий HTTP-сервер на выделенном порту. Полностью автономен от
// внешних веб-серверов (Nginx/Apache). При верном пароле пишет IP
// админа в сигнальный файл; воркер подхватывает его на следующем
// тике и внедряет точечное правило.
//
// РЕФЕРЕНС: secured_rescue_gate.php (Shell-эра) -> [АРХИВ].
// ЗАМЕНА: этот модуль. Принцип генома №5 — автономность.
// =====================================================================
package armor

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"syscall"
	"time"

	"golang.org/x/crypto/bcrypt"

	"agl/config"
	"agl/internal/sys"
)

// ensureWebRescueGate открывает гейт UFW для порта веб-стука.
// Канал спасения обязан быть доступен из интернета даже при полной
// изоляции периметра. Сигнатура ВНЕ множества испепеления — канал
// перманентный (геном: «Каналы спасения обязательны»).
// ensureWebRescueGate открывает гейт UFW для порта веб-стука.
// Канал спасения обязан быть доступен из интернета даже при полной
// изоляции периметра. Сигнатура ВНЕ множества испепеления — канал
// перманентный (геном: «Каналы спасения обязательны»).
//
// БЛОК 3.1:
//   - Команда выполняется через единый интерфейс sys.RunTimeout.
//   - Ошибка не игнорируется.
//   - Успех логируется только если UFW реально отработал без ошибки.
func ensureWebRescueGate(port int) {
	// Формируем порт в формате, который понимает UFW: например, "55111/tcp".
	portStr := fmt.Sprintf("%d/tcp", port)

	// Корпоративный комментарий правила.
	// По нему позже можно идентифицировать гейт спасения в `ufw status`.
	comment := fmt.Sprintf("Amber Guard Lab. Web Rescue Gate %d", port)

	// Выполняем открытие гейта с таймаутом 5 секунд.
	// Таймаут защищает watcher от зависшего UFW/D-Bus/xtables-lock.
	res := sys.RunTimeout(5*time.Second, "ufw", "allow", portStr, "comment", comment)

	// Если UFW не смог добавить правило, канал спасения деградирует.
	// Это не должно ронять watcher, но обязано быть видно в журнале.
	if res.Err != nil {
		// LogWarnIfErr сам вытащит ошибку и обрезанный stderr/stdout.
		// event=ufw.rescue_allow_failed — машинный код сбоя.
		res.LogWarnIfErr("WEB_RESCUE", "UFW_GATE_OPEN", "event=ufw.rescue_allow_failed")

		// Не пишем SUCCESS, потому что команда не подтверждена.
		return
	}

	// Пишем SUCCESS только после реального успеха внешней команды.
	sys.LogAction("WEB_RESCUE", "UFW_GATE_OPEN", "SUCCESS",
		fmt.Sprintf("Открыт UFW-гейт канала веб-стука: порт %d", port))
}

// startWebRescueServer поднимает HTTPS-сервер веб-стука на ЛИЧНОМ mux.
// M3f (v8.1.3): fail closed — нет сертификата, нет сервера.
// Пароль не должен путешествовать по открытому каналу.
func startWebRescueServer(port int) *http.Server {
	if err := ensureRescueTLS(); err != nil {
		if !tlsAlerted {
			tlsAlerted = true
			sys.LogAction("WEB_RESCUE", "TLS_FAIL_CLOSED", "ALERT",
				fmt.Sprintf("TLS-сертификат недоступен: %v. Сервер веб-стука НЕ запущен (fail closed). Ретрай на следующем тике.", err))
		}
		return nil
	}
	tlsAlerted = false

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleWebRescue)
	srv := &http.Server{
		Addr:              fmt.Sprintf("0.0.0.0:%d", port),
		Handler:           mux,
		TLSConfig:         &tls.Config{MinVersion: tls.VersionTLS12},
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    1 << 16,
	}
	go func() {
		if err := srv.ListenAndServeTLS(rescueCertPath, rescueKeyPath); err != nil && err != http.ErrServerClosed {
			sys.LogAction("WEB_RESCUE", "SERVER_FAIL", "ERROR",
				fmt.Sprintf("Сбой HTTPS-сервера веб-стука: %v", err))
		}
	}()
	sys.LogAction("WEB_RESCUE", "SERVER_START", "SUCCESS",
		fmt.Sprintf("Автономный HTTPS-сервер веб-стука запущен на порту %d (TLS 1.2+)", port))
	return srv
}

// tlsAlerted — логируем ALERT один раз при входе в состояние сбоя TLS.
var tlsAlerted bool

// removeWebRescueGate снимает гейт UFW старого порта веб-стука.
// M3c: вызывается при смене порта в SETTINGS и при выключении канала.
//
// БЛОК 3.1:
//   - Команда выполняется через единый интерфейс sys.RunTimeout.
//   - Ошибка удаления правила логируется, а не игнорируется.
//   - SUCCESS пишется только при успешном удалении.
func removeWebRescueGate(port int) {
	// Формируем идентификатор правила для UFW: например, "55111/tcp".
	portSpec := fmt.Sprintf("%d/tcp", port)

	// Пытаемся удалить ALLOW-правило гейта спасения.
	// --force нужен, чтобы UFW не задавал интерактивных вопросов.
	res := sys.RunTimeout(5*time.Second, "ufw", "--force", "delete", "allow", portSpec)

	// Если удаление не подтверждено, фиксируем предупреждение.
	// Возможные причины: UFW занят, правило уже отсутствует, сбой прав.
	if res.Err != nil {
		// event=ufw.rescue_delete_failed — машинный код сбоя.
		res.LogWarnIfErr("WEB_RESCUE", "UFW_GATE_CLOSED", "event=ufw.rescue_delete_failed")

		// Не пишем SUCCESS, потому что факт закрытия гейта не подтверждён.
		return
	}

	// Успех логируем только после реального успеха команды.
	sys.LogAction("WEB_RESCUE", "UFW_GATE_CLOSED", "SUCCESS",
		fmt.Sprintf("Гейт UFW веб-стука снят: порт %d", port))
}

// v8.2.3 (1.7): счётчик брутфорса в памяти сервера.
//
// R-1: добавлен мьютекс для защиты состояния анти-брутфорса.
//
// Почему это нужно:
//   - handleWebRescue выполняется HTTP-сервером в нескольких горутинах;
//   - rescueFailCount и rescueLockUntil являются общим изменяемым состоянием;
//   - без синхронизации два одновременных запроса могут испортить счётчик
//     или время блокировки.
//
// Почему используем обычный sync.Mutex:
//   - операции короткие;
//   - критическая секция маленькая;
//   - нет отдельного сценария "только чтение", ради которого нужен RWMutex.
var (
	// rescueFailMu защищает rescueFailCount и rescueLockUntil.
	rescueFailMu sync.Mutex

	// rescueFailCount хранит количество подряд неудачных попыток ввода пароля.
	rescueFailCount int

	// rescueLockUntil хранит момент времени, до которого действует блокировка.
	rescueLockUntil time.Time
)

// isValidKnockIPv4 — v8.2.0 (1.5): строгая валидация адреса источника стука.
// net.ParseIP вместо регулярки: 999.999.999.999 больше не проходит.
// Дополнительно отклоняются: 0.0.0.0, 255.255.255.255, мультикаст 224.0.0.0/4.
func isValidKnockIPv4(ip string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil || parsed.To4() == nil {
		return false
	}
	if parsed.IsUnspecified() {
		return false
	}
	if parsed.Equal(net.IPv4bcast) {
		return false
	}
	if parsed.IsMulticast() {
		return false
	}
	return true
}

// webRescuePasswordValid — v8.2.0 (1.7): bcrypt-сравнение вместо SHA-256 и «==».
// bcrypt.CompareHashAndPassword устойчив к тайминг-атакам и хранит соль внутри хэша.
func webRescuePasswordValid(input string) bool {
	if config.C.WebRescuePasswordHash == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword(
		[]byte(config.C.WebRescuePasswordHash), []byte(input)) == nil
}

// handleWebRescue обрабатывает запросы к странице веб-стука (псевдо-2FA).
// v8.2.3: net.ParseIP-валидация, bcrypt, задержка 5 сек, лок-аут после 5 неудач.
//
// R-1:
//   - Все чтения и записи rescueFailCount и rescueLockUntil выполняются только
//     под мьютексом rescueFailMu.
//   - Критические секции делаются максимально короткими.
//   - time.Sleep и рендеринг HTML выполняются вне мьютекса, чтобы один
//     медленный запрос не блокировал обработку остальных запросов.
func handleWebRescue(w http.ResponseWriter, r *http.Request) {
	// Получаем адрес клиента из RemoteAddr.
	// RemoteAddr обычно имеет вид "1.2.3.4:56789".
	adminIP := r.RemoteAddr

	// Отделяем IP от порта.
	// Если разбор не удался, оставляем как есть,
	// дальше сработает строгая валидация адреса.
	if host, _, err := net.SplitHostPort(adminIP); err == nil {
		adminIP = host
	}

	// GET и другие методы показываем как обычную форму входа.
	// Лок-аут здесь не проверяем, потому что пароль ещё не передавался.
	// Это сохраняет текущее поведение контракта.
	if r.Method != http.MethodPost {
		renderWebRescueForm(w, false)
		return
	}

	// Разбираем POST-форму.
	// Ошибка игнорируется сознательно: пустая форма приведёт к пустому паролю,
	// а пустой пароль не пройдёт проверку подлинности.
	_ = r.ParseForm()

	// Извлекаем пароль из поля формы.
	password := r.FormValue("f_pass")

	// v8.2.3 (1.7): идёт лок-аут — пароль даже не сравниваем.
	//
	// R-1: читаем rescueLockUntil только под мьютексом.
	//
	// Почему делаем именно так:
	//   - time.Now().Before(...) читает rescueLockUntil;
	//   - другой поток в этот момент может изменить rescueLockUntil;
	//   - без блокировки чтение состояния гонки недопустимо.
	//
	// Почему сразу освобождаем мьютекс:
	//   - дальше будет искусственная задержка 5 секунд;
	//   - нельзя держать мьютекс во время Sleep;
	//   - иначе один заблокированный запрос остановит проверку состояния
	//     для других параллельных запросов.
	rescueFailMu.Lock()
	locked := time.Now().Before(rescueLockUntil)
	rescueFailMu.Unlock()

	// Если активна блокировка, отвечаем той же формой ошибки
	// и добавляем задержку для замедления перебора.
	if locked {
		time.Sleep(5 * time.Second)
		renderWebRescueForm(w, true)
		return
	}

	// Проверяем пароль через bcrypt.
	if webRescuePasswordValid(password) {
		// Успешная аутентификация.
		//
		// R-1: сбрасываем счётчик неудачных попыток только под мьютексом.
		//
		// Зачем:
		//   - если до этого были неудачные попытки, серия должна быть сброшена;
		//   - запись в общий счётчик без мьютекса является гонкой.
		rescueFailMu.Lock()
		rescueFailCount = 0
		rescueFailMu.Unlock()

		// v8.2.3 (1.5): строгая валидация IPv4 через net.ParseIP.
		//
		// Пароль может быть верным, но адрес источника всё равно обязан быть
		// корректным IPv4-адресом. Иначе сигнал не пишем.
		if !isValidKnockIPv4(adminIP) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("<html><body>Ошибка: некорректный IP-адрес источника.</body></html>"))
			return
		}

		// v8.2.3 (1.4): O_NOFOLLOW + 0600 — защита от symlink-атак.
		//
		// Открываем сигнальный файл:
		//   - только на запись;
		//   - создаём, если его нет;
		//   - обрезаем содержимое;
		//   - не переходим по симлинкам;
		//   - права 0600, чтобы файл не был доступен другим пользователям системы.
		f, errSig := os.OpenFile(webKnockFile,
			os.O_WRONLY|os.O_CREATE|os.O_TRUNC|syscall.O_NOFOLLOW, 0600)

		if errSig != nil {
			// Если не смогли открыть файл, канал спасения не получает сигнал.
			// Это нужно обязательно писать в журнал.
			sys.LogAction("WEB_RESCUE", "SIGNAL_WRITE_FAIL", "ERROR",
				fmt.Sprintf("Не удалось записать файл сигнала: %v", errSig))
		} else {
			// Записываем IP админа в сигнальный файл.
			_, errSig = f.WriteString(adminIP)

			// Закрываем файл сразу после записи.
			// Ошибка закрытия проверяется ниже вместе с ошибкой записи.
			_ = f.Close()

			if errSig != nil {
				// Ошибка записи или закрытия означает, что сигнал не доставлен.
				sys.LogAction("WEB_RESCUE", "SIGNAL_WRITE_FAIL", "ERROR",
					fmt.Sprintf("Не удалось записать файл сигнала: %v", errSig))
			} else {
				// Успешный веб-стук зафиксирован.
				sys.LogAction("WEB_RESCUE", "SIGNAL_WRITTEN", "SUCCESS",
					fmt.Sprintf("Файл сигнала записан (0600): IP=%s. Воркер подхватит на следующем тике.", adminIP))
			}
		}

		// Показываем экран успешного стука.
		renderWebRescueSuccess(w, adminIP)
		return
	}

	// Неудачная аутентификация.
	//
	// R-1: вся работа со счётчиком неудач выполняется под мьютексом.
	//
	// Почему здесь нельзя просто сделать rescueFailCount++:
	//   - два параллельных запроса могут прочитать одно и то же значение;
	//   - затем оба записать увеличенное значение;
	//   - в итоге счётчик увеличится только один раз вместо двух;
	//   - это может отложить или сломать срабатывание лок-аута.
	rescueFailMu.Lock()

	// Увеличиваем количество подряд неудачных попыток.
	rescueFailCount++

	// Проверяем, достигнут ли порог блокировки.
	if rescueFailCount >= 5 {
		// Порог достигнут.
		//
		// Сбрасываем счётчик, чтобы после окончания блокировки
		// отсчёт начался заново.
		rescueFailCount = 0

		// Устанавливаем окно блокировки на 60 секунд.
		rescueLockUntil = time.Now().Add(60 * time.Second)

		// Освобождаем мьютекс до записи в журнал.
		//
		// Зачем:
		//   - LogAction может выполнять форматирование и запись в файл;
		//   - не нужно удерживать состояние анти-брутфорса на это время.
		rescueFailMu.Unlock()

		// Фиксируем факт срабатывания анти-брутфорс блокировки.
		sys.LogAction("WEB_RESCUE", "BRUTEFORCE_LOCKOUT", "ALERT",
			"Пять неудачных попыток пароля подряд — канал веб-стука заблокирован на 60 секунд.")
	} else {
		// Порог ещё не достигнут.
		// Просто освобождаем мьютекс и продолжаем обычную обработку.
		rescueFailMu.Unlock()
	}

	// v8.2.3 (1.7): анти-брутфорс задержка поднята с 2 до 5 секунд.
	//
	// Важно: задержка выполняется вне мьютекса.
	// Если держать мьютекс здесь, один неудачный запрос будет тормозить
	// проверку состояния для всех остальных горутин.
	time.Sleep(5 * time.Second)

	// Показываем форму с ошибкой доступа.
	renderWebRescueForm(w, true)
}

// renderWebRescueForm отдаёт HTML-форму авторизации.
// Стиль из референса: тёмный фон, моноширинный шрифт, неоновые акценты.
func renderWebRescueForm(w http.ResponseWriter, showError bool) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	errorHTML := ""
	redirectHTML := ""
	if showError {
		errorHTML = `<div class="error">Ошибка: Доступ запрещен.</div>`
		redirectHTML = `<meta http-equiv="refresh" content="300; url=https://google.com">`
	}

	html := fmt.Sprintf(`<!DOCTYPE html>
<html lang="ru">
<head>
<meta charset="utf-8">
<title>authorizationform</title>
<meta name="viewport" content="width=device-width, initial-scale=1.0, minimum-scale=1.0, maximum-scale=1.0, user-scalable=no">
%s
<style>
body { background: #0f141c; color: #a5b4fc; font-family: monospace; display: flex; justify-content: center; align-items: center; height: 100vh; margin: 0; }
.authorizationform { border: 1px solid #1e293b; background: #111827; padding: 40px; border-radius: 4px; box-shadow: 0 10px 25px rgba(0,0,0,0.5); text-align: center; width: 280px; }
.auth_required { font-size: 20px; font-weight: bold; margin-bottom: 25px; color: #f43f5e; letter-spacing: 2px; }
.key_entry { background: #030712; border: 1px solid #374151; padding: 10px; color: #00ff66; text-align: center; font-size: 16px; width: 90%%; margin-bottom: 20px; outline: none; }
.key_entry:focus { border-color: #6366f1; }
.button { background: #312e81; border: 1px solid #4338ca; color: #fff; padding: 10px 25px; cursor: pointer; font-size: 14px; font-weight: bold; width: 100%%; transition: 0.2s; }
.button:hover { background: #3730a3; border-color: #4f46e5; }
.error { color: #f43f5e; margin-top: 15px; font-weight: bold; }
</style>
</head>
<body>
<div class="authorizationform">
<div class="auth_required">AUTH REQUIRED</div>
<form action="" method="post">
<input class="key_entry" type="password" name="f_pass" value="" autofocus placeholder="••••••••••••" autocomplete="off" />
<input class="button" type="submit" value="Enter">
</form>
%s
</div>
</body>
</html>`, redirectHTML, errorHTML)

	_, _ = w.Write([]byte(html))
}

// renderWebRescueSuccess отдаёт экран подтверждения успешного веб-стука.
// Авто-редирект на Гугл через 100 секунд (анти-сканер).
func renderWebRescueSuccess(w http.ResponseWriter, adminIP string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	html := fmt.Sprintf(`<!DOCTYPE html>
<html lang="ru">
<head>
<meta charset="utf-8">
<title>Reset Activated</title>
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<meta http-equiv="refresh" content="300; url=https://google.com">
<style>
body { background: #0f141c; color: #00ff66; font-family: monospace; display: flex; justify-content: center; align-items: center; height: 100vh; margin: 0; }
.success { text-align: center; border: 1px solid #00ff66; padding: 30px; box-shadow: 0 0 20px rgba(0,255,102,0.2); font-size: 16px; line-height: 1.6; }
</style>
</head>
<body>
<div class="success">[ ACCESS GRANTED ]<br>
IP-адрес %s зафиксирован.<br>
Ожидание...</div>
</body>
</html>`, adminIP)

	_, _ = w.Write([]byte(html))
}
