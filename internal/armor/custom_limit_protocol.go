// internal/armor/custom_limit_protocol.go
package armor

import (
	"agl/config"
	"agl/i18n"
	"agl/internal/sys" // ИНТЕГРАЦИЯ АУДИТОРА v6.4.0
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

var (
	ufwCacheMu       sync.RWMutex
	lastUfwSnapshot  string
	lastUfwCacheTime time.Time

	// v8.2.2 (3.1): edge-triggered флаг деградации статуса UFW на горячем пути.
	// Один WARNING при входе в сбой, один SUCCESS при восстановлении.
	// Без флага TUI-кэш (2 сек) заспамил бы журнал одинаковыми событиями.
	ufwStatusDegraded bool
)

// SetCustomRateLimit управляет правилами лимитации пакетов Netfilter/UFW по стандартам Amber Guard Lab v6.0.0.
// Маркеры состояний унифицированы для исключения конфуза рассинхронизации UI.
func SetCustomRateLimit(targetPort int, sysTag, targetName, action string) error {
	action = strings.ToUpper(action)
	portProtocol := fmt.Sprintf("%d/tcp", targetPort)

	// Формула летучего маркера состояний в ОЗУ tmpfs.
	// БЛОК 4.4: генератор пути из config/paths.go; ВАЖНО сохранить
	// присваивание — runtimeMarker используется в ON/OFF/CHECK ниже.
	runtimeMarker := config.ArmorRateLimitMarkerPath(targetPort)

	switch action {
	case "ON":
		// C1-предохранитель: путь к маркеру ловушки из config/paths.go.
		// ВАЖНО сохранить присваивание — переменная читается в os.Stat ниже.
		tarpitMarker := config.TarpitMarkerPath(targetPort)
		if _, errT := os.Stat(tarpitMarker); errT == nil {
			return fmt.Errorf(i18n.T.ArmorLimitTarpitReject, targetPort)
		}
		// Очищаем цепочки для исключения наслоения правил.
		// БЛОК 3.1: прямой exec.Command заменён на sys.RunTimeout.
		// Зачистка — best-effort: правила могут отсутствовать, поэтому
		// запоминаем ПОСЛЕДНЮЮ попытку каждого типа и логируем только её.
		var lastLimitClean, lastAllowClean sys.CmdResult
		for i := 0; i < 3; i++ {
			// Удаляем старый лимитер порта (3 попытки по контракту стабилизации).
			lastLimitClean = sys.RunTimeout(5*time.Second, "ufw", "--force", "delete", "limit", portProtocol)
			// Удаляем старый allow порта (3 попытки).
			lastAllowClean = sys.RunTimeout(5*time.Second, "ufw", "--force", "delete", "allow", portProtocol)
		}
		// LogWarnIfErr сам промолчит при успехе и напишет WARNING при сбое.
		// event=ufw.limit_pre_clean_failed / event=ufw.allow_pre_clean_failed.
		lastLimitClean.LogWarnIfErr("ARMOR", "C1_PRE_CLEAN", "event=ufw.limit_pre_clean_failed")
		lastAllowClean.LogWarnIfErr("ARMOR", "C1_PRE_CLEAN", "event=ufw.allow_pre_clean_failed")

		// H1 (v8.1.3): зачистка надгробий ARMOR_OFF. Удаляем ALLOW-правила
		// с сигнатурой "DDoS protection Disabled on Port N" — иначе порт
		// остаётся открытым для всех (дыра на скрытом порту!).
		// БЛОК 3.1: статус через единый интерфейс; сбой статуса больше не молчит.
		statusRes := sys.RunTimeout(5*time.Second, "ufw", "status", "numbered")
		if statusRes.Err != nil {
			// Без списка правил надгробия не найти. Это деградация H1-контракта,
			// поэтому пишем WARNING с машинным кодом.
			statusRes.LogWarnIfErr("ARMOR", "H1_TOMB_CLEANUP", "event=ufw.status_failed")
		} else {
			// Сигнатура надгробия для данного порта.
			tombSig := fmt.Sprintf("DDoS protection Disabled on Port %d", targetPort)
			// Разбираем stdout построчно.
			lines := strings.Split(statusRes.Stdout, "\n")
			// Идём с конца: номера правил съезжают при удалении.
			for i := len(lines) - 1; i >= 0; i-- {
				if strings.Contains(lines[i], tombSig) {
					if numStr := extractRuleNumber(lines[i]); numStr != "" {
						// БЛОК 3.1: удаление надгробия больше не слепое.
						delRes := sys.RunTimeout(5*time.Second, "ufw", "--force", "delete", numStr)
						// Надгробие обязано быть уничтожено: сбой оставляет
						// дыру «открыт для всех» на порту. WARNING обязателен.
						delRes.LogWarnIfErr("ARMOR", "H1_TOMB_CLEANUP", "event=ufw.tomb_delete_failed")
					}
				}
			}
		}

		// Накатываем лимит пакетов со строгим корпоративным комментарием
		// commentMsg := fmt.Sprintf("Amber Guard Lab. DDoS Rate Limit Port %d", targetPort)

		// ФИКС v6.2.0: Синхронизируем ИБ-спецификацию лояльных комментариев с реальными переменными бэкенда
		commentMsg := fmt.Sprintf("Amber Guard Lab. DDoS Rate Limit Port %d (%s)", targetPort, targetName)

		// БЛОК 3.1: критичная команда через единый интерфейс с таймаутом 10 сек.
		// Без подтверждённого лимита броня порта НЕ считается взведённой.
		limitRes := sys.RunTimeout(10*time.Second, "ufw", "limit", portProtocol, "comment", commentMsg)
		if limitRes.Err != nil {
			// Возвращаем ошибку наружу, обогащённую stderr внешней команды:
			// роутер TUI покажет админу реальную причину, а не голый err.
			return fmt.Errorf(i18n.T.ArmorLimitActivateErr, targetPort, limitRes.Err, strings.TrimSpace(limitRes.Stderr))
		}

		// Создаем летучий маркер в ОЗУ для индикации в TUI
		_ = sys.WriteAtomic(runtimeMarker, []byte("ON"), 0644)
		// M4-гигиена (v8.1.3): квитанция живёт и умирает ВМЕСТЕ с правилом.
		// ON всегда возвращает квитанцию — порт снова под управлением M4.
		_ = sys.WriteAtomic(config.ArmorMarkerPath(targetPort), []byte("ON"), 0644)
		ResetUfwCacheSnapshot()
		return nil

	case "OFF":
		// ЦИКЛИЧЕСКИЙ ДЕМОНТАЖ: Вызываем удаление 4 раза подряд.
		// БЛОК 3.1: прямой exec.Command заменён на sys.RunTimeout.
		// Демонтаж — best-effort: логируем только последнюю попытку каждого типа.
		var lastLimitDel, lastAllowDel sys.CmdResult
		for i := 0; i < 4; i++ {
			// Сносим лимитер порта (4 попытки по контракту демонтажа).
			lastLimitDel = sys.RunTimeout(5*time.Second, "ufw", "--force", "delete", "limit", portProtocol)
			// Сносим allow порта (4 попытки).
			lastAllowDel = sys.RunTimeout(5*time.Second, "ufw", "--force", "delete", "allow", portProtocol)
		}
		// При сбое последней попытки пишем WARNING с машинным кодом.
		lastLimitDel.LogWarnIfErr("ARMOR", "C1_DISMANTLE", "event=ufw.limit_dismiss_failed")
		lastAllowDel.LogWarnIfErr("ARMOR", "C1_DISMANTLE", "event=ufw.allow_dismiss_failed")

		// =================================================================
		// ФИКС v6.2.0: ЛОЯЛЬНЫЙ КОММЕНТАРИЙ ПРИ ДЕАКТИВАЦИИ РУБЕЖА ЗАЩИТЫ
		// =================================================================
		// Вместо безликого текста выводим имя службы, возвращенной в штатный режим
		commentMsg := fmt.Sprintf("Amber Guard Lab. DDoS protection Disabled on Port %d (%s)", targetPort, targetName)

		// БЛОК 3.1: критичная команда через единый интерфейс с таймаутом 10 сек.
		// Без подтверждённого allow порт остаётся без штатного режима.
		allowRes := sys.RunTimeout(10*time.Second, "ufw", "allow", portProtocol, "comment", commentMsg)
		if allowRes.Err != nil {
			// Ошибка наружу + stderr внешней команды для диагностики TUI.
			return fmt.Errorf(i18n.T.ArmorLimitDeactivateErr, targetPort, allowRes.Err, strings.TrimSpace(allowRes.Stderr))
		}

		// Уничтожаем летучий маркер рантайма
		_ = os.Remove(runtimeMarker)
		// M4-гигиена (v8.1.3): правило снято → квитанция снята.
		// «Маркер без порта» — мусор, порождающий фантомные восстановления M4.
		_ = os.Remove(config.ArmorMarkerPath(targetPort))
		ResetUfwCacheSnapshot()
		return nil

	case "CHECK":
		// Оперативная память ОЗУ является главным источником правды для TUI
		if _, err := os.Stat(runtimeMarker); err == nil {
			return nil // Рубеж активен
		}
		return fmt.Errorf("%s", i18n.T.ArmorLimitCheckDisabled)

	default:
		return fmt.Errorf(i18n.T.ArmorLimitUnknownAction, action)
	}
}

// IsUfwRulePresent выполняет кэшированный опрос Netfilter для защиты процессора от перегрузок
func IsUfwRulePresent(port, comment string) bool {
	ufwCacheMu.Lock()

	// Порог кэширования — 2 секунды. Снимает петлю 100% CPU Overload в TUI
	// полностью избавляя процессор от вызовов тяжелого Python-скрипта UFW
	if time.Since(lastUfwCacheTime) < 2*time.Second && lastUfwSnapshot != "" {
		ufwCacheMu.Unlock()
		return parseUfwOutput(lastUfwSnapshot, port)
	}

	// Время кэша истекло или это первый запуск — делаем ОДИН системный вызов.
	// БЛОК 3.1: через единый интерфейс sys.RunTimeout.
	statusRes := sys.RunTimeout(5*time.Second, "ufw", "status")

	// ГОРЯЧИЙ ПУТЬ: этот вызов живёт в цикле TUI-рендера (кэш 2 сек).
	// Логировать каждый сбой — значит залить журнал. Применяем
	// edge-triggered принцип: один WARNING на вход в деградацию.
	if statusRes.Err != nil {
		if !ufwStatusDegraded {
			// Первый сбой после успеха — фиксируем переход.
			ufwStatusDegraded = true
			statusRes.LogWarnIfErr("ARMOR", "UFW_STATUS_CACHE", "event=ufw.status_failed")
		}
		// Статус недоступен: правило считаем неподтверждённым (fail-safe).
		ufwCacheMu.Unlock()
		return false
	}

	// Статус снова доступен — закрываем ребро деградации одним SUCCESS.
	if ufwStatusDegraded {
		ufwStatusDegraded = false
		sys.LogAction("ARMOR", "UFW_STATUS_CACHE", "SUCCESS",
			i18n.T.ArmorLimitUfwRestored)
	}

	// Обновляем глобальный кэш в оперативной памяти.
	// Используем структурированный Stdout вместо сырого []byte.
	lastUfwSnapshot = statusRes.Stdout
	lastUfwCacheTime = time.Now()
	ufwCacheMu.Unlock()
	return parseUfwOutput(lastUfwSnapshot, port)
}

func parseUfwOutput(snapshot, port string) bool {
	lines := strings.Split(snapshot, "\n")
	for _, line := range lines {
		// Очищаем пробелы для точного синтаксического анализа
		normalizedLine := strings.ReplaceAll(line, " ", "")
		targetPortPattern := port + "/tcp"

		// Рубеж считается активным (ON) только если в строке UFW присутствует
		// номер порта И ключевое слово лимитера LIMIT (а не просто ALLOW)
		if strings.Contains(normalizedLine, targetPortPattern) && strings.Contains(strings.ToUpper(normalizedLine), "LIMIT") {
			return true
		}
	}
	return false
}

// ResetUfwCacheSnapshot принудительно обнуляет кэш UFW в памяти для мгновенного обновления сетки TUI
// Вызывается при массовых транзакциях во избежание фризов и конфузов графической сетки.
func ResetUfwCacheSnapshot() {
	ufwCacheMu.Lock()
	lastUfwSnapshot = ""
	lastUfwCacheTime = time.Unix(0, 0)
	ufwCacheMu.Unlock()
}

// PurgeLimitRule (C1-санитар, v8.1.3): жёсткое удаление лимит-правила порта
// БЕЗ надгробия «Disabled». Используется M4 для зачистки устаревших лимитов
// на портах с активными ловушками: капкан обязан оставаться доступным,
// а надгробие на капкане — шум (гейт ямы и так держит allow N/tcp).
func PurgeLimitRule(targetPort int) {
	portProtocol := fmt.Sprintf("%d/tcp", targetPort)
	// БЛОК 3.1: удаление больше не слепое.
	// Три попытки по контракту санитарии; запоминаем последний результат.
	var lastPurge sys.CmdResult
	for i := 0; i < 3; i++ {
		lastPurge = sys.RunTimeout(5*time.Second, "ufw", "--force", "delete", "limit", portProtocol)
	}

	// Если удаление НЕ подтверждено — правило может быть живо.
	// Тогда маркеры сносить НЕЛЬЗЯ: квитанция живёт и умирает ВМЕСТЕ
	// с правилом (M4-гигиена). Иначе C1-нарушение на порту с ловушкой
	// станет невидимым для следующих reconcile.
	if lastPurge.Err != nil {
		// WARNING с машинным кодом: санитария C1 не подтверждена.
		lastPurge.LogWarnIfErr("ARMOR", "LIMIT_PURGE", "event=ufw.purge_delete_failed")
		// Кэш сбрасываем, чтобы TUI увидел актуальную картину при следующем опросе.
		ResetUfwCacheSnapshot()
		// Маркеры остаются: C1-санитар повторится на следующем reconcile.
		return
	}

	// Удаление подтверждено — лимит мёртв.
	// Сносим оба маркера: квитанция и рантайм не должны жить без правила.
	_ = os.Remove(config.ArmorRateLimitMarkerPath(targetPort))
	_ = os.Remove(config.ArmorMarkerPath(targetPort))
	ResetUfwCacheSnapshot()
	sys.LogAction("ARMOR", "LIMIT_PURGED", "SUCCESS",
		fmt.Sprintf(i18n.T.ArmorLimitPurgeSuccess, targetPort))
}
