// internal/armor/web_server.go
// Ядро сервера, TLS, UFW-гейты, 404-заглушка, адаптер логов
package armor

import (
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"agl/config"
	"agl/i18n"
	"agl/internal/analytics"
	"agl/internal/sys"
	"agl/internal/webpanel"
)

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

	// 3. Редирект с корня префикса интерфейса на /panel
	mux.HandleFunc(interfacePrefix, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == interfacePrefix || r.URL.Path == interfacePrefix+"/" {
			http.Redirect(w, r, interfacePrefix+"panel", http.StatusFound)
			return
		}
		handle404Stub(w, r)
	})

	// === Делегирование панели ===
	webpanel.RegisterRoutes(mux, analytics.GlobalStore, analytics.GlobalManager, interfacePrefix)

	// 4. Обработчик 404 для всех неизвестных путей
	mux.HandleFunc("/", handle404Stub)

	srv := &http.Server{
		Addr:              fmt.Sprintf("0.0.0.0:%d", port),
		Handler:           mux,
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
