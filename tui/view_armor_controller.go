// tui/view_armor_controller.go
package tui

import (
	"agl/config"
	"agl/i18n"
	"agl/internal/service"
	"fmt"
	"os"
	"strings"
	"time"

	ui "github.com/gizak/termui/v3"
)

// Константы рантайм-состояний экранов для глобального роутера панели
const (
	ScreenArmor        = "ARMOR_MAIN"
	ScreenCoreSettings = "ARMOR_CORE_SETTINGS"
)

// ActiveScreen управляет тем, какой экран сейчас рендерится в главном цикле ui.Event
var ActiveScreen = ScreenArmor

// tarpitOwnsPort возвращает true, если порт занят ЗАПУЩЕННОЙ ловушкой.
// Маркер на диске — факт трёхфакторного статуса; дисплей (TARPIT в
// view_armor.go) читает тот же маркер, поэтому исполнение не разъезжается
// с картинкой. Инвентарь сознательно не трогаем: декларация без маркера — не факт.
func tarpitOwnsPort(port int) bool {
	_, err := os.Stat(config.TarpitMarkerPath(port))
	return err == nil
}

// HandleArmorMenuClick инкапсулирует всю тяжелую логику кликов экрана ANTIDDOS v6.3.8
func HandleArmorMenuClick(currentRow int, selectedRowStr string) string {
	var hintText string

	switch {
	// --- ПЕРЕХОД В ПОДМЕНЮ: Перехватываем клик на CORE_SETTINGS ---
	case strings.Contains(selectedRowStr, "CORE_SETTINGS"):
		clearCoreAlert()                  // гасим старый аларм при входе в меню настроек ядра
		ActiveScreen = ScreenCoreSettings // Переключаем глобальный флаг экрана для ui.Event роутера
		return ""

	case strings.Contains(selectedRowStr, "ARMOR_ON"):
		// v9.0.0: проверка вердикта ДО открытия транзакции
		_, _, verdict := service.PlanArmorOperation("ON")
		if verdict == "ALREADY_ON" {
			// Явное уведомление: бокс на 1000 мс, потом возврат в ARMOR
			ui.Clear()
			renderArmorMessage(i18n.T.ViewArmorCtlHintAlreadyOn)
			time.Sleep(armorHintHold)
			hintText = i18n.T.ViewArmorCtlHintAlreadyOn
			return hintText
		}
		if verdict == "INVENTORY_UNREADABLE" {
			ui.Clear()
			renderArmorMessage(i18n.T.ViewArmorCtlHintInventoryError)
			time.Sleep(armorHintHold)
			hintText = i18n.T.ViewArmorCtlHintInventoryError
			return hintText
		}
		// Инициализируем RAM-канал трансляции шагов
		progChan := make(chan string, 10)
		// Запускаем тяжелый 15-секундный взвод Брони в фоновом асинхронном потоке
		go func() {
			// v9.0.0: предохранитель строит план операции до открытия транзакции
			filteredPorts, filteredApps, verdict := service.PlanArmorOperation("ON")
			if verdict != "OK" {
				progChan <- i18n.T.ViewArmorCtlProgSysctlTune
				service.ResetUfwCacheSnapshot()
				_ = service.ToggleSysctlProtocol("ON")
				close(progChan)
				return
			}

			progChan <- i18n.T.ViewArmorCtlProgSysctlTune
			service.ResetUfwCacheSnapshot()
			_ = service.ToggleSysctlProtocol("ON")

			// Итерируем готовые цели из плана — все guard-ы уже применены
			processed := make(map[int]bool)
			for _, p := range filteredPorts {
				processed[p.Port] = true
				progChan <- fmt.Sprintf(i18n.T.ViewArmorCtlProgArmPort, p.Port)
				dynamicID := fmt.Sprintf("port_auto_%d", p.Port)
				_ = service.ToggleArmorRoute(dynamicID, "ON")
			}

			for _, app := range filteredApps {
				// Дедуп: IPv4+IPv6 слушатель того же порта
				if processed[app.Port] {
					continue
				}
				processed[app.Port] = true
				progChan <- fmt.Sprintf(i18n.T.ViewArmorCtlProgArmService, app.Name, app.Port)
				dynamicID := fmt.Sprintf("port_auto_%d", app.Port)
				_ = service.ToggleArmorRoute(dynamicID, "ON")
			}
			progChan <- i18n.T.ViewArmorCtlProgNetfilterReload
			_ = service.ReloadFirewall()
			close(progChan) // Закрываем канал, давая сигнал окну прогресса
		}()

		// Запускаем графическое окно прогресса, оно заблокирует UI-поток до завершения канала
		ShowTransactionWindow(i18n.T.ViewArmorCtlProgressTitleOn, i18n.T.ViewArmorProgressInitKernel, progChan, buildArmorFinalReport())
		hintText = i18n.T.ViewArmorCtlHintArmorOnDone

	case strings.Contains(selectedRowStr, "ARMOR_OFF"):
		// v9.0.0: проверка вердикта ДО открытия транзакции
		_, _, verdict := service.PlanArmorOperation("OFF")
		if verdict == "ALREADY_OFF" {
			// Явное уведомление: бокс на 1000 мс, потом возврат в ARMOR
			ui.Clear()
			renderArmorMessage(i18n.T.ViewArmorCtlHintAlreadyOff)
			time.Sleep(armorHintHold)
			hintText = i18n.T.ViewArmorCtlHintAlreadyOff
			return hintText
		}
		if verdict == "INVENTORY_UNREADABLE" {
			ui.Clear()
			renderArmorMessage(i18n.T.ViewArmorCtlHintInventoryError)
			time.Sleep(armorHintHold)
			hintText = i18n.T.ViewArmorCtlHintInventoryError
			return hintText
		}
		progChan := make(chan string, 10)
		go func() {
			// v9.0.0: предохранитель строит план операции до открытия транзакции
			filteredPorts, filteredApps, verdict := service.PlanArmorOperation("OFF")
			if verdict != "OK" {
				progChan <- i18n.T.ViewArmorCtlProgSysctlReset
				service.ResetUfwCacheSnapshot()
				_ = service.ToggleSysctlProtocol("OFF")
				close(progChan)
				return
			}

			progChan <- i18n.T.ViewArmorCtlProgSysctlReset
			service.ResetUfwCacheSnapshot()
			_ = service.ToggleSysctlProtocol("OFF")

			// Итерируем готовые цели из плана — все guard-ы уже применены
			processed := make(map[int]bool)
			for _, p := range filteredPorts {
				processed[p.Port] = true
				progChan <- fmt.Sprintf(i18n.T.ViewArmorCtlProgDisarmPort, p.SysTag, p.Port)
				dynamicID := fmt.Sprintf("port_auto_%d", p.Port)
				_ = service.ToggleArmorRoute(dynamicID, "OFF")
			}

			for _, app := range filteredApps {
				// Дедуп: IPv4+IPv6 слушатель того же порта
				if processed[app.Port] {
					continue
				}
				processed[app.Port] = true
				progChan <- fmt.Sprintf(i18n.T.ViewArmorCtlProgDeactivateService, app.Name, app.Port)
				dynamicID := fmt.Sprintf("port_auto_%d", app.Port)
				_ = service.ToggleArmorRoute(dynamicID, "OFF")
			}
			progChan <- i18n.T.ViewArmorCtlProgNetfilterCleanup
			_ = service.ReloadFirewall()
			close(progChan)
		}()

		ShowTransactionWindow(i18n.T.ViewArmorCtlProgressTitleOff, i18n.T.ViewArmorProgressInitKernel, progChan, buildArmorFinalReport())
		hintText = i18n.T.ViewArmorCtlHintArmorOffDone

	case strings.Contains(selectedRowStr, "SYSCTL"):
		// Одиночный переключатель системных оптимизаций ядра (ручной боевой тумблер)
		// Тумблер обязан спрашивать «включена ли броня», а не «совпадает ли ядро».
		// Иначе после правки параметра CHECK давал ошибку → вместо OFF накатывался ON,
		// и админ не мог снять броню с первого нажатия.
		if service.IsSysctlArmorActive() {
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
					hintText = fmt.Sprintf(i18n.T.ViewArmorCtlHintTarpitReject, targetPort)
				} else {
					if errCheck := service.CheckArmorRouteStatus(targetUUIDOrTag); errCheck != nil {
						if errAct := service.ToggleArmorRoute(targetUUIDOrTag, "ON"); errAct != nil {
							hintText = i18n.T.ViewArmorCtlErrorPrefix + errAct.Error()
						} else {
							_ = service.ReloadFirewall()
							hintText = fmt.Sprintf(i18n.T.ViewArmorCtlHintPerimeterOn, targetSysTag)
						}
					} else {
						if errDeact := service.ToggleArmorRoute(targetUUIDOrTag, "OFF"); errDeact != nil {
							hintText = i18n.T.ViewArmorCtlErrorPrefix + errDeact.Error()
						} else {
							_ = service.ReloadFirewall()
							hintText = fmt.Sprintf(i18n.T.ViewArmorCtlHintPerimeterOff, targetSysTag)
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
		// === СБРОС К ЗАВОДСКОМУ ЭТАЛОНУ AGL ===
		// БЫЛО: Armor := Default* (честный Linux) → 128/128/0, то есть
		// минимум защиты и выключенный anti-spoofing под маркером «броня ON».
		// СТАЛО: единый ведущий эталон — тот же, что при первом старте.
		// Сброс к чистому Linux остаётся только через ARMOR_OFF (базлайн).
		if err := config.Update(config.FileConfig, func(c *config.Config) {
			c.SysctlCore = config.FactorySysctlCore()
		}); err != nil {
			setCoreAlertError(i18n.T.ViewArmorCtlCoreSaveFailPrefix + err.Error())
			return
		}

		// Накат по пробнику активности, не по сверке значений (см. H5).
		if service.IsSysctlArmorActive() {
			_ = service.ToggleSysctlProtocol("ON")
		}

		setCoreAlertSuccess(i18n.T.ViewArmorCtlCoreResetSuccess)
		return
	}

	// 3. ЛОГИКА ИЗМЕНЕНИЯ ЦИФРОВЫХ ПАРАМЕТРОВ (Строки 0-6)
	// При клике на строки с цифровыми параметрами (0-6) — активируем встроенный режим ввода
	if currentRow >= 0 && currentRow < 7 {
		IsCoreEditMode = true
		clearCoreAlert()

		// Подтягиваем текущее значение в буфер ввода по умолчанию
		// Буфер ввода наполняется по индексу спеки — без дубля switch.
		cfg := config.C.SysctlCore
		currentVal, _ := cfg.ArmorByIndex(currentRow)
		CurrentCoreInputBuf = fmt.Sprintf("%d", currentVal)
	}
}
