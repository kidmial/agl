// tui/view_armor_controller.go
package tui

import (
	"agl/config"
	"agl/internal/service"
	"fmt"
	"os"
	"strings"
)

// Константы рантайм-состояний экранов для глобального роутера панели
const (
	ScreenArmor        = "ARMOR_MAIN"
	ScreenCoreSettings = "ARMOR_CORE_SETTINGS"
)

// ActiveScreen управляет тем, какой экран сейчас рендерится в главном цикле ui.Event
var ActiveScreen = ScreenArmor

// HandleArmorMenuClick инкапсулирует всю тяжелую логику кликов экрана ANTIDDOS v6.3.8
func HandleArmorMenuClick(currentRow int, selectedRowStr string) string {
	var hintText string

	switch {
	// --- ПЕРЕХОД В ПОДМЕНЮ: Перехватываем клик на CORE_SETTINGS ---
	case strings.Contains(selectedRowStr, "CORE_SETTINGS"):
		CoreAlertMessage = ""             // Очищаем старые алармы при входе в меню настроек ядра
		ActiveScreen = ScreenCoreSettings // Переключаем глобальный флаг экрана для ui.Event роутера
		return ""

	case strings.Contains(selectedRowStr, "ARMOR_ON"):
		// Инициализируем RAM-канал трансляции шагов
		progChan := make(chan string, 10)
		// Запускаем тяжелый 15-секундный взвод Брони в фоновом асинхронном потоке
		go func() {
			progChan <- "Оптимизация SYSCTL параметров..."
			service.ResetUfwCacheSnapshot()
			_ = service.ToggleSysctlProtocol("ON")

			_, isSshMoved := os.Stat(config.MarkerSshMoved) // РЕБРЕНДИНГ
			// v8.2.3 (R-3b): прямой вызов config.LoadInventory() заменяем на сервисный слой.
			// TUI больше не знает о деталях чтения JSON-инвентаря с диска.
			ports, _ := service.LoadInventory()
			for _, p := range ports {
				tarpitMarker := config.TarpitMarkerPath(p.Port) // БЛОК 4.4: генератор путей из config/paths.go
				if _, errT := os.Stat(tarpitMarker); errT == nil {
					continue
				}
				if p.Port == 22 && isSshMoved == nil {
					continue
				}

				progChan <- fmt.Sprintf("Взвод лимитов порта %d", p.Port)
				dynamicID := fmt.Sprintf("port_auto_%d", p.Port)
				_ = service.ToggleArmorRoute(dynamicID, "ON")
			}

			// v8.2.3 (R-3b): прямой вызов config.LoadDetectedApps() заменяем на сервисный слой.
			apps, _ := service.LoadDetectedApps()
			for _, app := range apps {
				if app.Port > 0 {
					tarpitMarker := config.TarpitMarkerPath(app.Port) // БЛОК 4.4: генератор путей из config/paths.go
					if _, errT := os.Stat(tarpitMarker); errT == nil {
						continue
					}
					if app.Port == 22 && isSshMoved == nil {
						continue
					}

					// ЛОГ: Имя службы выводится в прогресс-бар
					progChan <- fmt.Sprintf("Защита службы: %s (порт: %d)", app.Name, app.Port)
					dynamicID := fmt.Sprintf("port_auto_%d", app.Port)
					_ = service.ToggleArmorRoute(dynamicID, "ON")
				}
			}
			progChan <- "Перезапуск таблиц Netfilter..."
			_ = service.ReloadFirewall()
			close(progChan) // Закрываем канал, давая сигнал окну прогресса
		}()

		// Запускаем графическое окно прогресса, оно заблокирует UI-поток до завершения канала
		ShowArmorProgressWindow("Armor Activation", progChan)
		hintText = "Все защитные рубежи периметра успешно активированы."

	case strings.Contains(selectedRowStr, "ARMOR_OFF"):
		progChan := make(chan string, 10)
		go func() {
			progChan <- "Сброс тюнинга SYSCTL"
			service.ResetUfwCacheSnapshot()
			_ = service.ToggleSysctlProtocol("OFF")

			_, isSshMoved := os.Stat(config.MarkerSshMoved) // РЕБРЕНДИНГ
			// v8.2.3 (R-3b): прямой вызов config.LoadInventory() заменяем на сервисный слой.
			ports, _ := service.LoadInventory()
			for _, p := range ports {
				if p.Port == 22 && isSshMoved == nil {
					continue
				}
				// ЛОГ: Вывод портов ручного инвентаря
				progChan <- fmt.Sprintf("Снятие лимитов службы: %s (порт: %d)", p.SysTag, p.Port)
				dynamicID := fmt.Sprintf("port_auto_%d", p.Port)
				_ = service.ToggleArmorRoute(dynamicID, "OFF")
			}

			// v8.2.3 (R-3b): прямой вызов config.LoadDetectedApps() заменяем на сервисный слой.
			apps, _ := service.LoadDetectedApps()
			for _, app := range apps {
				if app.Port > 0 {
					if app.Port == 22 && isSshMoved == nil {
						continue
					}
					// ЛОГ: Вывод авто-служб
					progChan <- fmt.Sprintf("Деактивация защиты %s (порт: %d)", app.Name, app.Port)
					dynamicID := fmt.Sprintf("port_auto_%d", app.Port)
					_ = service.ToggleArmorRoute(dynamicID, "OFF")
				}
			}
			progChan <- "Очистка таблиц Netfilter..."
			_ = service.ReloadFirewall()
			close(progChan)
		}()

		ShowArmorProgressWindow("Armor Deactivation", progChan)
		hintText = "Сетевой щит полностью демонтирован из ядра Linux."

	case strings.Contains(selectedRowStr, "SYSCTL"):
		// Одиночный переключатель системных оптимизаций ядра (ручной боевой тумблер)
		if errCheck := service.CheckSysctlProtocol(); errCheck == nil {
			_ = service.ToggleSysctlProtocol("OFF")
		} else {
			_ = service.ToggleSysctlProtocol("ON")
		}

	default:
		// v8.2.3 (R-3b): прямые вызовы config.* заменяем на сервисный слой.
		// TUI работает только с контрактами service.*, соблюдая архитектурный инкапсулятор.
		ports, _ := service.LoadInventory()
		apps, _ := service.LoadDetectedApps()
		parts := strings.Split(strings.TrimSpace(selectedRowStr), "│")
		if len(parts) > 0 {
			targetSysTag := strings.TrimSpace(parts[0]) // Получаем "PORT_CUSTOM_443", "PORT_CUSTOM_22" и т.д.
			targetUUIDOrTag := ""
			var targetPort int
			for _, p := range ports {
				if strings.ToUpper(p.SysTag) == targetSysTag {
					targetUUIDOrTag = p.UUID
					targetPort = p.Port
					break
				}
			}
			if targetUUIDOrTag == "" {
				for _, app := range apps {
					// C6 (v8.1.3): принимаем и маршрутный тег, и короткий дисплейный.
					// Дисплей может оставаться "PORT N" — роутинг кликов не ломается.
					dynamicTag := fmt.Sprintf("PORT_AUTO_%d", app.Port)
					displayTag := fmt.Sprintf("PORT %d", app.Port)
					if dynamicTag == targetSysTag || displayTag == targetSysTag {
						targetUUIDOrTag = strings.ToLower(dynamicTag)
						targetPort = app.Port
						break
					}
				}
			}
			if targetUUIDOrTag != "" {
				// ВЕРСИЯ 6.0.0: ИБ-Предохранитель от конфликта с капканом в ручном режиме
				tarpitMarker := config.TarpitMarkerPath(targetPort) // РЕБРЕНДИНГ
				if _, errT := os.Stat(tarpitMarker); errT == nil {
					hintText = fmt.Sprintf("[ОТКАЗ]: На порту %d уже запущена Дегтярная яма! Установка лимитов невозможна.", targetPort)
				} else {
					if errCheck := service.CheckArmorRouteStatus(targetUUIDOrTag); errCheck != nil {
						if errAct := service.ToggleArmorRoute(targetUUIDOrTag, "ON"); errAct != nil {
							hintText = "[Ошибка]: " + errAct.Error()
						} else {
							_ = service.ReloadFirewall()
							hintText = fmt.Sprintf("Рубеж %s успешно активирован.", targetSysTag)
						}
					} else {
						if errDeact := service.ToggleArmorRoute(targetUUIDOrTag, "OFF"); errDeact != nil {
							hintText = "[Ошибка]: " + errDeact.Error()
						} else {
							_ = service.ReloadFirewall()
							hintText = fmt.Sprintf("Рубеж %s успешно отключен.", targetSysTag)
						}
					}
				}
			}
		}
	}

	return hintText
}

// HandleCoreSettingsClick обрабатывает логику нажатия ENTER внутри подменю параметров ядра
func HandleCoreSettingsClick(currentRow int, selectedRowStr string) {
	// 1. Кнопка ВОЗВРАТА в главное меню Брони
	if strings.Contains(selectedRowStr, "BACK_TO_ARMOR") {
		ActiveScreen = ScreenArmor // Возвращаем флаг на главное меню
		return
	}

	// 2. Кнопка СБРОСА К ЗАВОДСКИМ ЭТАЛОНАМ AMBER GUARD LAB
	if strings.Contains(selectedRowStr, "RESET_TO_FACTORY") {
		// v8.2.2 (волна гонки 2.3): мутация через config.Update —
		// клон снепшота, применение дефолтов, публикация, атомарная запись на диск.
		// Устраняет in-place мутации опубликованного снепшота и дублирует
		// явный SaveConfig единой точкой saveConfigLocked.
		if err := config.Update(config.FileConfig, func(c *config.Config) {
			s := &c.SysctlCore
			s.ArmorNetIpv4TcpSyncookies = s.DefaultNetIpv4TcpSyncookies
			s.ArmorNetIpv4TcpFinTimeout = s.DefaultNetIpv4TcpFinTimeout
			s.ArmorNetIpv4TcpSynRetries = s.DefaultNetIpv4TcpSynRetries
			s.ArmorNetIpv4TcpSynackRetries = s.DefaultNetIpv4TcpSynackRetries
			s.ArmorNetCoreSomaxconn = s.DefaultNetCoreSomaxconn
			s.ArmorNetIpv4TcpMaxSynBacklog = s.DefaultNetIpv4TcpMaxSynBacklog
			s.ArmorNetIpv4ConfAllRpFilter = s.DefaultNetIpv4ConfAllRpFilter
			s.ArmorNetIpv4ConfDefaultRpFilter = s.DefaultNetIpv4ConfDefaultRpFilter // СИНХРОНИЗАЦИЯ: Сбрасываем добавленный параметр rp_filter
		}); err != nil {
			CoreAlertMessage = "[ОШИБКА]: Сбой сохранения конфигурации: " + err.Error()
			return
		}

		// Мгновенно накатываем в рантайм, если Броня включена
		if errCheck := service.CheckSysctlProtocol(); errCheck == nil {
			_ = service.ToggleSysctlProtocol("ON")
		}

		CoreAlertMessage = "[УСПЕХ]: Параметры сброшены к эталонам Amber Guard Lab." // РЕБРЕНДИНГ
		if config.C.Lang == "en" {
			CoreAlertMessage = "[SUCCESS]: Parameters reset to Amber Guard Lab templates." // РЕБРЕНДИНГ
		}
		return
	}

	// 3. ЛОГИКА ИЗМЕНЕНИЯ ЦИФРОВЫХ ПАРАМЕТРОВ (Строки 0-6)
	// При клике на строки с цифровыми параметрами (0-6) — активируем встроенный режим ввода
	if currentRow >= 0 && currentRow < 7 {
		IsCoreEditMode = true
		CoreAlertMessage = ""

		// Подтягиваем текущее значение в буфер ввода по умолчанию
		cfg := config.C.SysctlCore
		var currentVal int
		switch currentRow {
		case 0:
			currentVal = cfg.ArmorNetIpv4TcpSyncookies
		case 1:
			currentVal = cfg.ArmorNetIpv4TcpFinTimeout
		case 2:
			currentVal = cfg.ArmorNetIpv4TcpSynRetries
		case 3:
			currentVal = cfg.ArmorNetIpv4TcpSynackRetries
		case 4:
			currentVal = cfg.ArmorNetCoreSomaxconn
		case 5:
			currentVal = cfg.ArmorNetIpv4TcpMaxSynBacklog
		case 6:
			currentVal = cfg.ArmorNetIpv4ConfAllRpFilter
		}
		CurrentCoreInputBuf = fmt.Sprintf("%d", currentVal)
	}
}
