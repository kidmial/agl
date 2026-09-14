// internal/armor/armorrouter.go
package armor

import (
	"agl/config"
	"agl/i18n"
	"agl/internal/sys" // ИНТЕГРАЦИЯ АУДИТОРА v6.4.0
	"encoding/json"    // ДОБАВИТЬ для маршалинга инвентаря
	"fmt"
	"os"            // ДОБАВИТЬ для работы с файлами-маркерами
	"path/filepath" // ДОБАВИТЬ для путей к папке states
	"regexp"
	"strings"
	"time" // v8.2.3 (R-2c): для таймаутов sys.RunTimeout
)

// isValidServiceName — v8.2.0 (2.5): гигиеническая валидация имени службы
// перед передачей в systemctl. Имя должно содержать только буквы, цифры,
// точки, подчёркивания и дефисы (допустимые символы в именах systemd unit).
// Это страховка от мусора и опечаток в инвентаре, а не защита от инъекций.
var serviceRegex = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

func isValidServiceName(name string) bool {
	return serviceRegex.MatchString(name)
}

// ExecuteArmorRoute служит центральным интерфейсным диспетчером для подсистемы защиты Amber Guard Lab v6.0.0.
// Принимает UUID рубежа (или системный тег) и требуемое действие (ON / OFF / CHECK).
func ExecuteArmorRoute(targetUUID, action string) error {
	action = strings.ToUpper(action)
	normalizedUUID := strings.ToLower(targetUUID)

	// ИБ-ФИЛЬТР АУДИТА: Логируем только активные изменения стейтов оператором (ON/OFF), игнорируя CHECK
	if action == "ON" || action == "OFF" {
		sys.LogAction("ARMOR_ROUTER", "CHANGE_STATE", "PENDING",
			fmt.Sprintf(i18n.T.ArmorRouterAdminStateChange, action, targetUUID))
	}

	// 1. РОУТИНГ ПО ТИПУ: статические рубежи ядра (sysctl, tarpit, move_port).
	// Если UUID распознан — возвращаем результат. Иначе — идём в динамический инвентарь.
	if handled, err := routeStaticArmor(normalizedUUID, action); handled {
		return err
	}

	// 2. РОУТИНГ ПО ИНВЕНТАРЮ: ищем динамические рубежи (Rate Limit) по UUID в JSON-инвентаре.
	// Если найден — применяем лимит и возвращаем результат. Иначе — идём в авто-подхват.
	if handled, err := routeInventoryArmor(normalizedUUID, action); handled {
		return err
	}

	// 3. ЭШЕЛОН АВТО-ПОДХВАТА: разрешение цели (порт/служба) и вычисление пути к квитанции.
	// Нормализует вход, ищет цель в PerimeterInventory, обновляет статус и возвращает
	// параметры для исполнительного движка действий (Транша 4).
	extractedPort, targetServiceName, receiptPath := resolveAutoTarget(normalizedUUID, action)

	// 4. ИСПОЛНИТЕЛЬНЫЙ ДВИЖОК ДЕЙСТВИЙ: исполнение ON / OFF / CHECK для авто-подхвата.
	// Если действие не распознано (не ON/OFF/CHECK), executeAutoAction вернёт (false, nil),
	// и мы упадём в фолбэк неизвестного рубежа.
	if handled, err := executeAutoAction(action, extractedPort, targetServiceName, receiptPath); handled {
		return err
	}

	// Фолбэк: неизвестный рубеж защиты
	sys.LogAction("ARMOR_ROUTER", "CHANGE_STATE", "ERROR", fmt.Sprintf(i18n.T.ArmorRouterUnknownPerimeter, targetUUID))
	return fmt.Errorf(i18n.T.ArmorRouterArchFail, targetUUID)

}

// routeStaticArmor обрабатывает статические системные рубежи ядра:
//   - "sysctl"    → оптимизация сетевого стека (SetSysctlProtocol)
//   - "tarpit"    → базовая дегтярная ловушка (SetTarpitProtocol)
//   - "move_port" → рокировка SSH (SetMovePortProtocol)
//
// КОНТРАКТ (п. 4.6 аудита — «роутинг по типу»):
//   - (true, nil)   → рубеж распознан и обработан успешно.
//   - (true, err)   → рубеж распознан, но протокол вернул ошибку.
//   - (false, nil)  → UUID не совпал ни с одним статическим рубежом;
//     вызывающий должен продолжить поиск (инвентарь/авто-подхват).
//
// ПОБОЧНЫЕ ЭФФЕКТЫ:
//   - Логирует SUCCESS/ERROR через событийную модель (только для ON/OFF).
//   - НЕ модифицирует глобальное состояние — делегирует работу протоколам.
//
// ОБНОВЛЕНО В v8.2.2 (п. 4.6): Извлечено из ExecuteArmorRoute для декомпозиции монолита.
func routeStaticArmor(normalizedUUID, action string) (handled bool, err error) {
	switch normalizedUUID {
	case "sysctl":
		if err := SetSysctlProtocol(action); err != nil {
			if action == "ON" || action == "OFF" {
				sys.LogAction("ARMOR_ROUTER", "SYSCTL_TUNING", "ERROR", err.Error())
			}
			return true, fmt.Errorf(i18n.T.ArmorRouterSysctlFail, err)
		}
		if action == "ON" || action == "OFF" {
			sys.LogAction("ARMOR_ROUTER", "SYSCTL_TUNING", "SUCCESS",
				fmt.Sprintf(i18n.T.ArmorRouterSysctlSuccess, action))
		}
		return true, nil

	case "tarpit":
		if err := SetTarpitProtocol(action); err != nil {
			if action == "ON" || action == "OFF" {
				sys.LogAction("ARMOR_ROUTER", "TARPIT_STATION", "ERROR", err.Error())
			}
			return true, fmt.Errorf(i18n.T.ArmorRouterTarpitFail, err)
		}
		if action == "ON" || action == "OFF" {
			sys.LogAction("ARMOR_ROUTER", "TARPIT_STATION", "SUCCESS",
				fmt.Sprintf(i18n.T.ArmorRouterTarpitSuccess, action))
		}
		return true, nil

	case "move_port":
		if err := SetMovePortProtocol(action); err != nil {
			if action == "ON" || action == "OFF" {
				sys.LogAction("ARMOR_ROUTER", "PORT_CASTLING", "ERROR", err.Error())
			}
			return true, fmt.Errorf(i18n.T.ArmorRouterCastlingFail, err)
		}
		if action == "ON" || action == "OFF" {
			sys.LogAction("ARMOR_ROUTER", "PORT_CASTLING", "SUCCESS",
				fmt.Sprintf(i18n.T.ArmorRouterCastlingSuccess, action))
		}
		return true, nil
	}
	// UUID не совпал ни с одним статическим рубежом — передаём управление дальше.
	return false, nil
}

// routeInventoryArmor обрабатывает динамические рубежи Rate Limit, объявленные в JSON-инвентаре.
// Ищет совпадение normalizedUUID в config.LoadInventory() и применяет SetCustomRateLimit.
//
// КОНТРАКТ (п. 4.6 аудита — «обработка порта / инвентарь»):
//   - (true, nil)   → рубеж найден в инвентаре и обработан успешно.
//   - (true, err)   → рубеж найден, но применение лимита вернуло ошибку.
//   - (false, nil)  → рубеж не найден в инвентаре (или инвентарь недоступен);
//     вызывающий должен продолжить поиск (эшелон авто-подхвата).
//
// ПОБОЧНЫЕ ЭФФЕКТЫ:
//   - Логирует SUCCESS/ERROR через событийную модель (только для ON/OFF).
//   - Читает JSON-инвентарь через config.LoadInventory().
//
// ОБНОВЛЕНО В v8.2.2 (п. 4.6): Извлечено из ExecuteArmorRoute для декомпозиции монолита.
func routeInventoryArmor(normalizedUUID, action string) (handled bool, err error) {
	ports, errLoad := config.LoadInventory()
	if errLoad != nil {
		// Инвентарь недоступен — пропускаем этот эшелон, пусть решает авто-подхват.
		return false, nil
	}

	for _, target := range ports {
		if strings.ToLower(target.UUID) == normalizedUUID {
			// Применяем лимит по плоскому контракту
			if errLim := SetCustomRateLimit(target.Port, target.SysTag, target.Service, action); errLim != nil {
				if action == "ON" || action == "OFF" {
					sys.LogAction("ARMOR_ROUTER", "CUSTOM_RATE_LIMIT", "ERROR",
						fmt.Sprintf(i18n.T.ArmorRouterRateLimitFail, target.Service, target.Port, errLim))
				}
				return true, fmt.Errorf(i18n.T.ArmorRouterRateLimitCriticalFail, target.Service, target.Port, errLim)
			}
			if action == "ON" || action == "OFF" {
				sys.LogAction("ARMOR_ROUTER", "CUSTOM_RATE_LIMIT", "SUCCESS",
					fmt.Sprintf(i18n.T.ArmorRouterRateLimitSuccess, target.Service, target.Port, action))
			}
			return true, nil
		}
	}
	// UUID не найден в инвентаре — передаём управление дальше.
	return false, nil
}

// resolveAutoTarget осуществляет парсинг входного идентификатора (порт или служба),
// поиск цели в эталонном инвентаре периметра и вычисление пути к файлу-квитанции.
//
// КОНТРАКТ (п. 4.6 аудита — «определение цели и маршрутизация квитанции»):
//   - Принимает нормализованный (нижний регистр) UUID и действие (для обновления статуса).
//   - Возвращает извлечённый порт (0, если служба беспортная), имя службы и путь к маркеру.
//   - Если цель не найдена в инвентаре, переменные могут остаться нулевыми/пустыми;
//     исполнительный движок (Транша 4) сам принимает решение о применимости действий.
//
// ПОБОЧНЫЕ ЭФФЕКТЫ:
//   - Читает эталонный инвентарь периметра (config.FilePerimeterInv).
//   - Выполняет транзакционное обновление статуса брони в инвентаре (если цель найдена).
//   - Логирует сбой обновления инвентаря через событийную модель.
//
// ОБНОВЛЕНО В v8.2.2 (п. 4.6): Извлечено из ExecuteArmorRoute для декомпозиции монолита.
func resolveAutoTarget(normalizedUUID, action string) (extractedPort int, targetServiceName, receiptPath string) {
	// Нормализуем разделители под наш стандарт (и точка, и подчеркивание для обратной совместимости TUI)
	cleanUUID := strings.ReplaceAll(normalizedUUID, "port_auto.", "port_auto_")

	isAptService := false
	if strings.HasPrefix(cleanUUID, "port_auto_") {
		_, errParse := fmt.Sscanf(cleanUUID, "port_auto_%d", &extractedPort)
		if errParse != nil || extractedPort <= 0 {
			extractedPort = 0
		}
	} else {
		// Если префикса порта нет, значит TUI передал сырое имя беспортной службы (например, fail2ban)
		isAptService = true
		targetServiceName = normalizedUUID
	}

	// Загружаем эталонный статический реестр инвентаря периметра
	inventoryPath := config.FilePerimeterInv
	var inv config.PerimeterInventory

	jsonData, errInv := os.ReadFile(inventoryPath)
	if errInv == nil {
		_ = json.Unmarshal(jsonData, &inv)

		// Ищем целевой объект в массиве detected_apps
		foundIdx := -1
		for idx, app := range inv.DetectedApps {
			if !isAptService && app.Port == extractedPort && extractedPort > 0 {
				foundIdx = idx
				targetServiceName = app.Name
				break
			}
			if isAptService && strings.ToLower(app.Name) == targetServiceName {
				foundIdx = idx
				extractedPort = app.Port
				break
			}
		}

		// v8.2.0 (1.9): транзакционное обновление через пакет config —
		// инвентарь не перезаписывается обрезанной структурой,
		// ScanTime/OS/ProtectedPorts сохраняются.
		if foundIdx != -1 {
			if errUpd := config.UpdateDetectedAppArmorStatus(targetServiceName, extractedPort, action == "ON"); errUpd != nil {
				sys.LogAction("ARMOR_ROUTER", "INVENTORY_UPDATE_FAIL", "ERROR",
					fmt.Sprintf(i18n.T.ArmorRouterInventoryUpdateFail, extractedPort, errUpd))
			}
		}
	}

	// БЛОК 4.4: Маршрутизация квитанции через генераторы путей.
	// PURPOSE: Определить дисковый маркер для записи состояния брони.
	// CONTRACT: Если target — это порт (число), используем числовой маркер.
	//           Если target — это имя сервиса (строка), используем именованный.
	if extractedPort > 0 {
		receiptPath = config.ArmorMarkerPath(extractedPort)
	} else if targetServiceName != "" {
		receiptPath = config.ArmorServiceMarkerPath(targetServiceName)
	}

	return extractedPort, targetServiceName, receiptPath
}

// executeAutoAction исполняет действие (ON / OFF / CHECK) для эшелона авто-подхвата.
// Управляет портами (UFW Rate Limit) и беспортными службами (systemctl),
// записывает/удаляет файлы-квитанции и выполняет двухфакторную верификацию.
//
// КОНТРАКТ (п. 4.6 аудита — «обработка порта, обработка сервиса, запись квитанции»):
//   - (true, nil)   → действие ON/OFF/CHECK исполнено успешно.
//   - (true, err)   → действие распознано, но вернуло ошибку исполнения.
//   - (false, nil)  → действие не распознано (не ON/OFF/CHECK);
//     вызывающий должен обработать фолбэк неизвестного рубежа.
//
// ПОБОЧНЫЕ ЭФФЕКТЫ:
//   - Вызывает SetCustomRateLimit (UFW) или sys.RunTimeout для управления системными службами.
//   - Создаёт/удаляет файлы-квитанции через sys.WriteAtomic / os.Remove.
//   - Сбрасывает кэш UFW-снапшота (ResetUfwCacheSnapshot).
//   - Логирует события через событийную модель.
//
// ОБНОВЛЕНО В v8.2.2 (п. 4.6): Извлечено из ExecuteArmorRoute для декомпозиции монолита.
func executeAutoAction(action string, extractedPort int, targetServiceName, receiptPath string) (handled bool, err error) {
	switch action {
	case "ON":
		if extractedPort > 0 {
			// Накладываем Rate Limit UFW с именным корпоративным комментарием
			autoSysTag := fmt.Sprintf("auto_port_%d", extractedPort)
			// Корпоративный стиль имен: apache2 -> Apache2, nginx -> Nginx
			autoName := sys.PrettyServiceName(targetServiceName)
			// Передаем красивое имя в движок, чтобы UFW создал лояльный комментарий
			if err := SetCustomRateLimit(extractedPort, autoSysTag, autoName, "ON"); err != nil {
				sys.LogAction("ARMOR_ROUTER", "AUTO_PORT_SHIELD", "ERROR", err.Error())
				return true, fmt.Errorf("критический сбой авто-рубежа на порту %d: %w", extractedPort, err)
			}
		} else if targetServiceName != "" {
			// v8.2.0 (2.5): валидация имени службы перед systemctl.
			if !isValidServiceName(targetServiceName) {
				sys.LogAction("ARMOR_ROUTER", "SERVICE_NAME_INVALID", "WARNING",
					fmt.Sprintf(i18n.T.ArmorRouterServiceInvalidEnable, targetServiceName))
				return true, fmt.Errorf(i18n.T.ArmorRouterServiceInvalidErr, targetServiceName)
			}
			// Беспортная локальная служба (fail2ban) — управляем через systemctl.
			//
			// v8.2.3 (R-2c): заменяем прямой exec.Command на sys.RunTimeout.
			//
			// Что это даёт:
			//   - таймаут 10 секунд защищает роутер от зависшего systemd/D-Bus;
			//   - единый интерфейс исполнения команд во всём проекте;
			//   - автоматическое логирование сбоя с выводом stderr.
			//
			// Почему ошибка не прерывает поток (LogWarnIfErr, а не return):
			//   - дальше по коду идёт запись квитанции (receiptPath) на диск;
			//   - если systemd упал, но мы записали маркер, система попытается
			//     самовосстановиться при следующем тике или ребуте.
			enableRes := sys.RunTimeout(10*time.Second, "systemctl", "enable", "--now", targetServiceName)
			enableRes.LogWarnIfErr("ARMOR_ROUTER", "SERVICE_ENABLE", "event=systemd.service_enable_failed")
		}
		if receiptPath != "" {
			_ = os.MkdirAll(filepath.Dir(receiptPath), 0755)
			_ = sys.WriteAtomic(receiptPath, []byte("ON"), 0644)
		}
		ResetUfwCacheSnapshot()
		sys.LogAction("ARMOR_ROUTER", "AUTO_SHIELD_ACTIVATE", "SUCCESS",
			fmt.Sprintf(i18n.T.ArmorRouterAutoShieldActivate, extractedPort, targetServiceName))
		return true, nil

	case "OFF":
		if extractedPort > 0 {
			// Полностью вырезаем правила лимитации пакетов Netfilter
			autoSysTag := fmt.Sprintf("auto_port_%d", extractedPort)
			autoName := sys.PrettyServiceName(targetServiceName)
			_ = SetCustomRateLimit(extractedPort, autoSysTag, autoName, "OFF")
		} else if targetServiceName != "" {
			// v8.2.0 (2.5): валидация имени службы перед systemctl.
			if !isValidServiceName(targetServiceName) {
				sys.LogAction("ARMOR_ROUTER", "SERVICE_NAME_INVALID", "WARNING",
					fmt.Sprintf(i18n.T.ArmorRouterServiceInvalidDisable, targetServiceName))
				return true, fmt.Errorf(i18n.T.ArmorRouterServiceInvalidErr, targetServiceName)
			}
			// Беспортная локальная служба — тушим через systemctl.
			//
			// v8.2.3 (R-2c): заменяем прямой exec.Command на sys.RunTimeout.
			//
			// Что это даёт:
			//   - таймаут 10 секунд на снятие службы с автозагрузки и остановку;
			//   - если systemd завис, мы не повесим горутину TUI/бэкенда;
			//   - сбой фиксируется в журнале для последующего разбора.
			disableRes := sys.RunTimeout(10*time.Second, "systemctl", "disable", "--now", targetServiceName)
			disableRes.LogWarnIfErr("ARMOR_ROUTER", "SERVICE_DISABLE", "event=systemd.service_disable_failed")
		}
		// Уничтожаем файл-маркер квитанции
		if receiptPath != "" {
			_ = os.Remove(receiptPath)
		}
		ResetUfwCacheSnapshot()
		sys.LogAction("ARMOR_ROUTER", "AUTO_SHIELD_DEACTIVATE", "SUCCESS",
			fmt.Sprintf(i18n.T.ArmorRouterAutoShieldDeactivate, extractedPort, targetServiceName))
		return true, nil

	case "CHECK":
		// ДВУХФАКТОРНАЯ ВЕРИФИКАЦИЯ ИБ v6.1.0: Исключаем ложные статусы после жесткого ребута
		if receiptPath == "" {
			return true, fmt.Errorf("%s", i18n.T.ArmorRouterCheckNoTrajectory)
		}
		// Фактор 1: Наличие квитанции на диске
		if _, errStat := os.Stat(receiptPath); os.IsNotExist(errStat) {
			return true, fmt.Errorf("%s", i18n.T.ArmorRouterCheckReceiptMissing)
		}
		// Фактор 2: Реальное наличие правила в ядре Linux / Статус в systemd
		if extractedPort > 0 {
			commentCheck := fmt.Sprintf("Amber Guard Lab. DDoS Rate Limit Port %d", extractedPort)
			// Если правила UFW физически нет в ядре — у нас рассинхрон после ребута
			if !VerifyUfwRulePresence(commentCheck, true) {
				_ = os.Remove(receiptPath) // Самовосстановление: стираем мертвый маркер
				return true, fmt.Errorf("%s", i18n.T.ArmorRouterCheckUfwDesync)
			}
		} else if targetServiceName != "" {
			// v8.2.3 (R-2c): заменяем прямой exec.Command на sys.RunTimeout.
			//
			// ДВУХФАКТОРНАЯ ВЕРИФИКАЦИЯ (Фактор 2):
			// Мы уже проверили квитанцию на диске (Фактор 1). Теперь спрашиваем ядро ОС.
			//
			// Таймаут 5 секунд. Если systemd завис и не отвечает за 5 секунд,
			// checkRes.Stdout будет пустым. strings.TrimSpace("") != "active" сработает,
			// и мы удалим маркер. Это принцип fail closed: нет ответа — считаем службу мёртвой.
			checkRes := sys.RunTimeout(5*time.Second, "systemctl", "is-active", targetServiceName)
			if strings.TrimSpace(checkRes.Stdout) != "active" {
				_ = os.Remove(receiptPath) // Самовосстановление: стираем мертвый маркер
				return true, fmt.Errorf("%s", i18n.T.ArmorRouterCheckServiceDesync)
			}
		}
		return true, nil // Проверка пройдена, рубеж ACTIVE
	}
	// Действие не распознано (не ON/OFF/CHECK) — передаём управление вызывающему.
	return false, nil
}
