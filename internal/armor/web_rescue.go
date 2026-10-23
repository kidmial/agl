// internal/armor/web_rescue.go
// Логика стука, формы, анти-брутфорс, валидация IP
package armor

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"syscall"
	"time"

	"golang.org/x/crypto/bcrypt"

	"agl/config"
	"agl/i18n"
	"agl/internal/sys"
)

// v8.2.3 (1.7): счётчик брутфорса в памяти сервера.
// R-1: добавлен мьютекс для защиты состояния анти-брутфорса.
var (
	rescueFailMu    sync.Mutex
	rescueFailCount int
	rescueLockUntil time.Time
)

// registerRescueRoutes монтирует маршруты веб-стука в общий ServeMux сервера.
func registerRescueRoutes(mux *http.ServeMux, knockPrefix string) {
	// Редирект с корня префикса на /rescue
	mux.HandleFunc(knockPrefix, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == knockPrefix || r.URL.Path == knockPrefix+"/" {
			http.Redirect(w, r, knockPrefix+"rescue", http.StatusFound)
			return
		}
		handle404Stub(w, r)
	})

	// Канал спасения (веб-стук)
	mux.HandleFunc(knockPrefix+"rescue", handleWebRescue)
}

// isValidKnockIPv4 — строгая валидация адреса источника стука.
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

// webRescuePasswordValid — bcrypt-сравнение вместо SHA-256 и «==».
func webRescuePasswordValid(input string) bool {
	if config.C.WebRescuePasswordHash == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword(
		[]byte(config.C.WebRescuePasswordHash), []byte(input)) == nil
}

// handleWebRescue обрабатывает запросы к странице веб-стука (псевдо-2FA).
func handleWebRescue(w http.ResponseWriter, r *http.Request) {
	// v8.5.0 (K2): маршрут /rescue без опции B отдаёт 404
	if !config.C.SshIpWebKnock {
		handle404Stub(w, r)
		return
	}

	adminIP := r.RemoteAddr
	if host, _, err := net.SplitHostPort(adminIP); err == nil {
		adminIP = host
	}

	if r.Method != http.MethodPost {
		renderWebRescueForm(w, false)
		return
	}

	_ = r.ParseForm()
	password := r.FormValue("delta_pass")

	// Проверка лок-аута
	rescueFailMu.Lock()
	locked := time.Now().Before(rescueLockUntil)
	rescueFailMu.Unlock()

	if locked {
		time.Sleep(5 * time.Second)
		renderWebRescueForm(w, true)
		return
	}

	// Проверка пароля
	if webRescuePasswordValid(password) {
		rescueFailMu.Lock()
		rescueFailCount = 0
		rescueFailMu.Unlock()

		if !isValidKnockIPv4(adminIP) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("<html><body>" + i18n.T.WebRescuePageInvalidIP + "</body></html>"))
			return
		}

		// Запись сигнала (webKnockFile определена в watcher_protocol.go)
		f, errSig := os.OpenFile(webKnockFile,
			os.O_WRONLY|os.O_CREATE|os.O_TRUNC|syscall.O_NOFOLLOW, 0600)

		if errSig != nil {
			sys.LogAction("WEB_RESCUE", "SIGNAL_WRITE_FAIL", "ERROR",
				i18n.TrEventf("web_rescue.signal_write_fail", errSig))
		} else {
			_, errSig = f.WriteString(adminIP)
			_ = f.Close()

			if errSig != nil {
				sys.LogAction("WEB_RESCUE", "SIGNAL_WRITE_FAIL", "ERROR",
					i18n.TrEventf("web_rescue.signal_write_fail", errSig))
			} else {
				sys.LogAction("WEB_RESCUE", "SIGNAL_WRITTEN", "SUCCESS",
					i18n.TrEventf("web_rescue.signal_written", adminIP))
			}
		}

		renderWebRescueSuccess(w, adminIP)
		return
	}

	// Неудачная аутентификация (анти-брутфорс)
	rescueFailMu.Lock()
	rescueFailCount++

	if rescueFailCount >= 5 {
		rescueFailCount = 0
		rescueLockUntil = time.Now().Add(60 * time.Second)
		rescueFailMu.Unlock()

		sys.LogAction("WEB_RESCUE", "BRUTEFORCE_LOCKOUT", "ALERT",
			i18n.TrEvent("web_rescue.bruteforce_lockout"))
	} else {
		rescueFailMu.Unlock()
	}

	time.Sleep(5 * time.Second)
	renderWebRescueForm(w, true)
}

// renderWebRescueForm отдаёт HTML-форму авторизации.
func renderWebRescueForm(w http.ResponseWriter, showError bool) {
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
 <input class="key_entry" type="password" name="delta_pass" value="" autofocus placeholder="••••••••••••" autocomplete="off" />
 <input class="button" type="submit" value="%s">
 </form>
%s
 </div>
 </body>
 </html>`, i18n.T.WebRescuePageFormTitle, redirectHTML, i18n.T.WebRescuePageAuthRequired, i18n.T.WebRescuePageEnterButton, errorHTML)

	_, _ = w.Write([]byte(html))
}

// renderWebRescueSuccess отдаёт экран подтверждения успешного веб-стука.
func renderWebRescueSuccess(w http.ResponseWriter, adminIP string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	html := fmt.Sprintf(`<!DOCTYPE html>
<html lang="ru">
<head>
<meta charset="utf-8">
<title>%s</title>
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<meta http-equiv="refresh" content="300; url=https://google.com">
<style>
body { background: #0f141c; color: #00ff66; font-family: monospace; display: flex; justify-content: center; align-items: center; height: 100vh; margin: 0; }
.success { text-align: center; border: 1px solid #00ff66; padding: 30px; box-shadow: 0 0 20px rgba(0,255,102,0.2); font-size: 16px; line-height: 1.6; }
</style>
</head>
<body>
<div class="success">%s<br>%s %s<br>%s<hr>%s</div>
</body>
</html>`, i18n.T.WebRescuePageSuccessTitle, i18n.T.WebRescuePageAccessGranted, i18n.T.WebRescuePageIPAddress, adminIP, i18n.T.WebRescuePageRecorded, i18n.T.WebRescuePageWaiting)

	_, _ = w.Write([]byte(html))
}
