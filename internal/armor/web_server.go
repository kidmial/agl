// internal/armor/web_server.go
// Ядро сервера, TLS, UFW-гейты, 404-заглушка, адаптер логов
package armor

import (
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"agl/config"
	"agl/i18n"
	"agl/internal/analytics"
	"agl/internal/sys"
	"agl/internal/webpanel"

	"golang.org/x/crypto/bcrypt"
)

// v8.6.2: session store для веб-панели статистики.
// Session cookies (без Expires) удаляются при закрытии браузера — безопасно.
type panelSession struct {
	expiry  time.Time
	adminIP string
}

var (
	panelSessionMu     sync.Mutex
	panelSessions      = make(map[string]panelSession)
	panelAuthFailMu    sync.Mutex
	panelAuthFailCount int
	panelAuthLockUntil time.Time
)

// generateSessionID создаёт криптографически стойкий session ID (32 hex-символа).
func generateSessionID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Крайне маловероятный фолбэк
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// panelSessionLifetime — время жизни сессии (8 часов, как в PHP-эталоне).
const panelSessionLifetime = 8 * time.Hour

// tlsAlerted — логируем ALERT один раз при входе в состояние сбоя TLS.
var tlsAlerted bool

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
	portStr := fmt.Sprintf("%d/tcp", port)
	comment := fmt.Sprintf("Amber Guard Lab. Web Rescue Gate %d", port)

	res := sys.RunTimeout(5*time.Second, "ufw", "allow", portStr, "comment", comment)

	if res.Err != nil {
		res.LogWarnIfErr("WEB_RESCUE", "UFW_GATE_OPEN", "event=ufw.rescue_allow_failed")
		return
	}

	sys.LogAction("WEB_RESCUE", "UFW_GATE_OPEN", "SUCCESS",
		i18n.TrEventf("web_rescue.ufw_gate_open", port))
}

// startWebRescueServer поднимает HTTPS-сервер веб-стука и делегирует маршруты панели.
func startWebRescueServer(port int) *http.Server {
	if err := ensureRescueTLS(); err != nil {
		if !tlsAlerted {
			tlsAlerted = true
			sys.LogAction("WEB_RESCUE", "TLS_FAIL_CLOSED", "ALERT",
				i18n.TrEventf("web_rescue.tls_fail_closed", err))
		}
		return nil
	}
	tlsAlerted = false

	// === ИНИЦИАЛИЗАЦИЯ АНАЛИТИКИ ===
	startWebAnalytics()

	mux := http.NewServeMux()

	// Формируем уникальные префиксы из конфига
	knockPrefix := "/" + config.C.WebKnockHashPrefix + "/"
	interfacePrefix := "/" + config.C.WebInterfaceHashPrefix + "/"

	// 1 & 2. Делегируем регистрацию маршрутов стука в web_rescue.go
	registerRescueRoutes(mux, knockPrefix)

	// 3. Редирект с корня префикса интерфейса на /dashboard/overview
	mux.HandleFunc(interfacePrefix, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == interfacePrefix || r.URL.Path == interfacePrefix+"/" {
			http.Redirect(w, r, interfacePrefix+"dashboard/overview", http.StatusFound)
			return
		}
		handle404Stub(w, r)
	})

	// === Делегирование панели ===
	webpanel.RegisterRoutes(mux, analytics.GlobalStore, analytics.GlobalManager, interfacePrefix)

	// Auth endpoints
	mux.HandleFunc(interfacePrefix+"login", func(w http.ResponseWriter, r *http.Request) {
		handlePanelLogin(w, r, interfacePrefix)
	})
	mux.HandleFunc(interfacePrefix+"logout", func(w http.ResponseWriter, r *http.Request) {
		handlePanelLogout(w, r, interfacePrefix)
	})

	// 4. Обработчик 404 для всех неизвестных путей
	mux.HandleFunc("/", handle404Stub)

	// 5. Middleware: session-based auth для путей панели (/dashboard/*, /api/*).
	// Статика (/assets/*), /login, /logout доступны без сессии.
	panelAuthMiddleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, interfacePrefix) {
				next.ServeHTTP(w, r)
				return
			}

			// Статика и auth endpoints без сессии
			if strings.HasPrefix(r.URL.Path, interfacePrefix+"assets/") ||
				r.URL.Path == interfacePrefix+"login" ||
				r.URL.Path == interfacePrefix+"logout" {
				next.ServeHTTP(w, r)
				return
			}

			// Проверка session cookie
			cookie, err := r.Cookie("agl_panel_session")
			if err != nil {
				http.Redirect(w, r, interfacePrefix+"login", http.StatusFound)
				return
			}

			panelSessionMu.Lock()
			sess, ok := panelSessions[cookie.Value]
			if ok && time.Now().After(sess.expiry) {
				delete(panelSessions, cookie.Value)
				ok = false
			}
			panelSessionMu.Unlock()

			if !ok {
				http.Redirect(w, r, interfacePrefix+"login", http.StatusFound)
				return
			}

			next.ServeHTTP(w, r)
		})
	}

	srv := &http.Server{
		Addr:              fmt.Sprintf("0.0.0.0:%d", port),
		Handler:           panelAuthMiddleware(mux),
		TLSConfig:         &tls.Config{MinVersion: tls.VersionTLS12},
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    1 << 16,
		ErrorLog:          log.New(httpErrorLogAdapter{}, "", 0),
	}

	go func() {
		if err := srv.ListenAndServeTLS(rescueCertPath, rescueKeyPath); err != nil && err != http.ErrServerClosed {
			sys.LogAction("WEB_RESCUE", "SERVER_FAIL", "ERROR",
				i18n.TrEventf("web_rescue.server_fail", err))
		}
	}()

	sys.LogAction("WEB_RESCUE", "SERVER_START", "SUCCESS",
		i18n.TrEventf("web_rescue.server_start", port))
	return srv
}

// removeWebRescueGate снимает гейт UFW старого порта веб-стука.
// M3c: вызывается при смене порта в SETTINGS и при выключении канала.
//
// БЛОК 3.1:
//   - Команда выполняется через единый интерфейс sys.RunTimeout.
//   - Ошибка удаления правила логируется, а не игнорируется.
//   - SUCCESS пишется только при успешном удалении.
func removeWebRescueGate(port int) {
	portSpec := fmt.Sprintf("%d/tcp", port)
	res := sys.RunTimeout(5*time.Second, "ufw", "--force", "delete", "allow", portSpec)

	if res.Err != nil {
		res.LogWarnIfErr("WEB_RESCUE", "UFW_GATE_CLOSED", "event=ufw.rescue_delete_failed")
		return
	}

	sys.LogAction("WEB_RESCUE", "UFW_GATE_CLOSED", "SUCCESS",
		i18n.TrEventf("web_rescue.ufw_gate_closed", port))
}

// renderPanelLoginForm отдаёт HTML-форму авторизации веб-панели.
// Аналог rescue channel: монохромный стиль, анти-брутфорс, PRG.
func renderPanelLoginForm(w http.ResponseWriter, showError bool) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	errorHTML := ""
	redirectHTML := ""
	if showError {
		errorHTML = `<div class="error">` + i18n.T.WebRescuePageAccessDenied + `</div>`
		redirectHTML = `<meta http-equiv="refresh" content="300; url=https://google.com">`
	}

	html := fmt.Sprintf(`<!DOCTYPE html>
<html lang="ru">
<head>
<meta charset="utf-8">
<title>%s</title>
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
<div class="auth_required">%s</div>
<form action="" method="post">
<input class="key_entry" type="password" name="panel_pass" value="" autofocus placeholder="••••••••••••" autocomplete="off" />
<input class="button" type="submit" value="%s">
</form>
%s
</div>
</body>
</html>`, i18n.T.WebRescuePageFormTitle, redirectHTML, i18n.T.WebRescuePageAuthRequired, i18n.T.WebRescuePageEnterButton, errorHTML)

	_, _ = w.Write([]byte(html))
}

// handlePanelLogin обрабатывает форму авторизации веб-панели.
// GET → рендер формы; POST → проверка пароля, генерация session, Set-Cookie, PRG.
func handlePanelLogin(w http.ResponseWriter, r *http.Request, interfacePrefix string) {
	if r.Method != http.MethodPost {
		renderPanelLoginForm(w, false)
		return
	}

	// Проверка lockout
	panelAuthFailMu.Lock()
	locked := time.Now().Before(panelAuthLockUntil)
	panelAuthFailMu.Unlock()

	if locked {
		time.Sleep(5 * time.Second)
		renderPanelLoginForm(w, true)
		return
	}

	_ = r.ParseForm()
	password := r.FormValue("panel_pass")

	adminIP := r.RemoteAddr
	if host, _, err := net.SplitHostPort(adminIP); err == nil {
		adminIP = host
	}

	// Проверка пароля через bcrypt
	if config.C.WebPanelPasswordHash != "" &&
		bcrypt.CompareHashAndPassword([]byte(config.C.WebPanelPasswordHash), []byte(password)) == nil {

		// Успех — сбрасываем счётчик неудач
		panelAuthFailMu.Lock()
		panelAuthFailCount = 0
		panelAuthFailMu.Unlock()

		// Генерация session ID (защита от session fixation)
		sessionID := generateSessionID()

		panelSessionMu.Lock()
		panelSessions[sessionID] = panelSession{
			expiry:  time.Now().Add(panelSessionLifetime),
			adminIP: adminIP,
		}
		panelSessionMu.Unlock()

		// Session cookie (без Expires — удаляется при закрытии браузера)
		http.SetCookie(w, &http.Cookie{
			Name:     "agl_panel_session",
			Value:    sessionID,
			Path:     interfacePrefix,
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteStrictMode,
		})

		sys.LogAction("WEB_PANEL", "LOGIN_SUCCESS", "INFO",
			fmt.Sprintf("Successful login from %s", adminIP))

		// PRG: редирект на GET, чтобы F5 не вызывал повторной отправки формы
		http.Redirect(w, r, interfacePrefix+"dashboard/overview", http.StatusFound)
		return
	}

	// Неудачная аутентификация (анти-брутфорс)
	panelAuthFailMu.Lock()
	panelAuthFailCount++

	if panelAuthFailCount >= 5 {
		panelAuthFailCount = 0
		panelAuthLockUntil = time.Now().Add(60 * time.Second)
		panelAuthFailMu.Unlock()

		sys.LogAction("WEB_PANEL", "BRUTEFORCE_LOCKOUT", "ALERT",
			"Five failed attempts — panel auth locked for 60 seconds")
	} else {
		panelAuthFailMu.Unlock()
		sys.LogAction("WEB_PANEL", "LOGIN_FAILED", "WARNING",
			fmt.Sprintf("Failed login attempt from %s", adminIP))
	}

	time.Sleep(5 * time.Second)
	renderPanelLoginForm(w, true)
}

// handlePanelLogout очищает сессию и удаляет cookie.
func handlePanelLogout(w http.ResponseWriter, r *http.Request, interfacePrefix string) {
	cookie, err := r.Cookie("agl_panel_session")
	if err == nil {
		panelSessionMu.Lock()
		delete(panelSessions, cookie.Value)
		panelSessionMu.Unlock()
	}

	// Удаление cookie (MaxAge=-1)
	http.SetCookie(w, &http.Cookie{
		Name:     "agl_panel_session",
		Value:    "",
		Path:     interfacePrefix,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})

	sys.LogAction("WEB_PANEL", "LOGOUT", "INFO", "Admin logged out")

	// Редирект на форму входа
	http.Redirect(w, r, interfacePrefix+"login", http.StatusFound)
}

// handle404Stub — обработка всех неизвестных путей (защита от сканирования).
func handle404Stub(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	fmt.Fprintf(w, `
        <!DOCTYPE html>
        <html lang="ru">
        <head>
            <meta charset="UTF-8">
            <meta name="viewport" content="width=device-width, initial-scale=1.0">
            <title>404</title>
            <style>
                body { margin: 0; padding: 0; background-color: #0f141c; color: #ffffff; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Ubuntu, sans-serif; display: flex; justify-content: center; align-items: center; height: 100vh; text-align: center; }
                .error-container { background-color: #1c2030; padding: 40px 60px; border-radius: 8px; box-shadow: 0 4px 20px rgba(0,0,0,0.5); border: 1px solid #2a2e39; max-width: 500px; }
                .error-code { font-size: 72px; font-weight: bold; color: #f23645; margin: 0; line-height: 1; }
            </style>
        </head>
        <body>
            <div class="error-container">
                <div class="error-code">404</div>
            </div>
        </body>
        </html>
    `)
}

// httpErrorLogAdapter — адаптер ErrorLog стандартного HTTP-сервера.
type httpErrorLogAdapter struct{}

// Write разбирает одну строку служебных сообщений HTTP-сервера.
func (httpErrorLogAdapter) Write(p []byte) (int, error) {
	line := strings.TrimSpace(string(p))
	if line == "" {
		return len(p), nil
	}
	if strings.Contains(line, "TLS handshake error from") {
		client := line
		if idx := strings.Index(client, "TLS handshake error from"); idx >= 0 {
			client = client[idx+len("TLS handshake error from"):]
		}
		client = strings.TrimSpace(client)
		if idx := strings.Index(client, ": remote error"); idx >= 0 {
			client = client[:idx]
		}
		sys.LogAction("WEB_RESCUE", "TLS_HANDSHAKE_REJECTED", "WARNING",
			i18n.TrEventf("web_rescue.tls_handshake_rejected", client))
		return len(p), nil
	}
	sys.LogAction("WEB_RESCUE", "SERVER_ERRORLOG", "WARNING",
		i18n.TrEventf("web_rescue.server_errorlog", line))
	return len(p), nil
}
