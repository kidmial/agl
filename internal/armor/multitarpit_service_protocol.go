// internal/armor/multitarpit_service_protocol.go
package armor

import (
	"fmt"
	"strings"
	"time"

	"agl/i18n"
	"agl/internal/sys"
)

// SetMultiTarpitProtocol управляет UFW-правилами для мульти-ловушек.
// Аналог SetTarpitProtocol, но для кастомных портов.
func SetMultiTarpitProtocol(port int, action string) error {
	portStr := fmt.Sprintf("%d/tcp", port)
	commentMsg := "Amber Guard Lab. Active Tarpit Station"

	switch action {
	case "ON":
		// Открываем порт в UFW.
		//
		// v8.2.3 (R-2a): прямой exec.Command заменяем на единый интерфейс
		// исполнения внешних команд sys.RunTimeout.
		//
		// Что это даёт:
		//   - единый стиль работы с внешними командами по всему проекту;
		//   - принудительный таймаут, чтобы зависший UFW не блокировал протокол;
		//   - захват stdout и stderr для последующей диагностики;
		//   - возможность вернуть машинно-читаемый контекст ошибки.
		//
		// Таймаут 10 секунд выбран с запасом:
		// операция добавления одного правила обычно быстрая, но UFW может ждать
		// xtables-lock или D-Bus, если система занята другим правилом.
		res := sys.RunTimeout(10*time.Second, "ufw", "allow", portStr, "comment", commentMsg)

		// Если команда завершилась ошибкой, протокол не должен делать вид,
		// что порт открыт. Возвращаем ошибку наверх.
		//
		// Важно:
		//   - %w сохраняет исходную ошибку для последующего анализа;
		//   - добавляем обрезанный stderr, потому что именно там обычно лежит
		//     реальная причина сбоя: конфликт правил, права, синтаксис UFW.
		if res.Err != nil {
			return fmt.Errorf(i18n.T.ArmorMultiTarpitOpenErr, port, res.Err, strings.TrimSpace(res.Stderr))
		}
		ResetUfwCacheSnapshot()
		sys.LogAction("MULTI_TARPIT_SERVICE", "UFW_GATE_OPEN", "SUCCESS",
			fmt.Sprintf(i18n.T.ArmorMultiTarpitGateOpen, port))
		return nil

	case "OFF":
		// Закрываем порт в UFW.
		//
		// v8.2.3 (R-2a): прямой exec.Command заменяем на единый интерфейс
		// исполнения внешних команд sys.RunTimeout.
		//
		// Что это даёт:
		//   - таймаут на удаление правила;
		//   - логирование ошибки вместо тихого игнорирования;
		//   - доступ к stderr для диагностики;
		//   - единый контракт исполнения команд во всём проекте.
		//
		// Таймаут 5 секунд достаточен для удаления одного правила,
		// но защищает от зависшего UFW/xtables-lock.
		delRes := sys.RunTimeout(5*time.Second, "ufw", "--force", "delete", "allow", portStr)

		// v8.2.3 (верификация аудитора, замечание 1): SUCCESS только при
		// подтверждённом удалении. Раньше при реальном сбое удаления журнал
		// получал ПРОТИВОРЕЧИВУЮ пару: WARNING от LogWarnIfErr и безусловный
		// SUCCESS. Теперь ветви взаимоисключающие:
		//   - Err != nil → только WARNING со stderr (гейт считаем закрытым
		//     best-effort: отсутствие правила = цель достигнута);
		//   - Err == nil → только SUCCESS, удаление подтверждено ufw.
		// Функция возвращает nil в обоих случаях: отсутствие правила не ошибка.
		if delRes.Err != nil {
			delRes.LogWarnIfErr("MULTI_TARPIT_SERVICE", "UFW_GATE_CLOSED",
				"event=ufw.multi_tarpit_gate_close_failed")
		} else {
			sys.LogAction("MULTI_TARPIT_SERVICE", "UFW_GATE_CLOSED", "SUCCESS",
				fmt.Sprintf(i18n.T.ArmorMultiTarpitGateClosed, port))
		}
		// Сброс кэша UFW-снапшота остаётся безусловным: он дешёвый и безопасный
		// в обоих исходах (кэш — лишь слепок для сверок, следующий опрос перечитает).
		ResetUfwCacheSnapshot()
		return nil
	}
	return fmt.Errorf(i18n.T.ArmorMultiTarpitUnknownAction, action)
}
