// tui/view_armor.go
// ============================================================================
// КОМПОНЕНТ: Экран управления сетевой бронёй периметра (ANTIDDOS / ARMOR)
// ВЕРСИЯ: v8.0.2
// РОЛЬ: Динамическая панель отображения защищённых портов, авто-обнаруженных
//
//	служб и глобальных триггеров брони. Содержит кнопку пересканирования
//	периметра безопасности хоста.
//
// ============================================================================
//
// АРХИТЕКТУРНАЯ ПОЗИЦИЯ:
//
//	Этот файл — TUI-компонент экрана «Броня» (ARMOR).
//	Вызывается из `view_router.go` при навигации на экран `ARMOR`.
//
// ФИКСИРОВАННАЯ РАСКЛАДКА ЭКРАНА (24 строки):
//
//	y: 1-4   → Шапка панели (ArmorHeader)          [3 строки]
//	y: 4-18  → Список меню (armorList)             [14 строк]
//	y: 18-23 → Блок подсказок (infoBox)            [5 строк]
//	y: 23-27 → Системная консоль (sysConsole)      [4 строки]
//
// ### [2026-08-23] — Перенос SCAN_PERIMETER в раздел ARMOR (v8.0.2)
//   - Кнопка SCAN_PERIMETER перенесена из SETTINGS в ANTIDDOS.
//   - Фактический порядок строк: SYSCTL=0, SCAN_PERIMETER=1, CORE_SETTINGS=2,
//     далее динамические рубежи, хвост: ARMOR_ON / ARMOR_OFF / BACK.
//   - v8.2.2: подсказки матчатся по тегу строки, а не по индексу.
//   - CORE_SETTINGS размещён на строке 1.
//   - Все блоки имеют фиксированную высоту (отказ от динамических размеров).
//   - Добавлена визуальная заглушка «Сканирую...» при запуске сканера.
//
// ============================================================================
package tui

import (
	"agl/config"
	"agl/i18n"
	"agl/internal/service"
	"agl/internal/sys"
	"fmt"
	"os"
	"strings"
	"time"

	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
)

// ============================================================================
// ГЛОБАЛЬНЫЕ ПЕРЕМЕННЫЕ
// ============================================================================

// GlobalArmorMenuRows удерживает актуальный динамический маппинг строк меню
// для корректной маршрутизации кликов в view_router.go.
var GlobalArmorMenuRows []string

// ArmorHeader — глобальный указатель на шапку панели (синглтон).
var ArmorHeader *widgets.Paragraph

// ============================================================================
// ОТРИСОВКА ЭКРАНА БРОНИ
// ============================================================================
// DrawArmorMenuGrid отрисовывает интерактивную суб-панель Брони комплекса Amber Guard Lab v6.3.8.
// ОБНОВЛЕНО: Добавлена динамическая генерация подсказок (infoBox) в зависимости от выбранной строки.
// Сохранена старая сигнатура для совместимости с view_router.go.
//
// ПАРАМЕТРЫ:
//   - selectedRow: индекс текущей выбранной строки
//   - hintText: (Игнорируется) старый параметр для совместимости. Подсказка генерируется автоматически.
//
// ВОЗВРАЩАЕТ:
//   - Срез виджетов для рендеринга
//   - Общее количество строк в списке меню
//
// Жесткая фиксация координат сохранена.
func DrawArmorMenuGrid(selectedRow int, hintText string) ([]ui.Drawable, int) {
	var rows []string

	// --- ГЕНЕРАТОР ПОДСКАЗОК (v8.2.2: контентный матчинг вместо индексов) ---
	// ФИКС: индексный матчинг (case 0/1/2/3) «съезжал» при любом изменении
	// состава списка (динамические порты, хвостовые триггеры), а case 3
	// держал фантомную подсказку «Статистика». Теперь пункт распознаётся
	// по собственному тегу в тексте строки — паттерн getArmorHint (view_router.go).
	// Замечание: детальные тексты пока хардкодом (RU); перенос в i18n —
	// в твою волну миграции языковых пакетов.
	generateHint := func(index int) string {
		if index < 0 || index >= len(rows) {
			return ""
		}
		rowStr := rows[index]
		switch {
		case strings.Contains(rowStr, "SYSCTL"):
			return i18n.T.ViewArmorHintSysctl
		case strings.Contains(rowStr, "SCAN_PERIMETER"):
			return i18n.T.ViewArmorHintScanPerimeter
		case strings.Contains(rowStr, "CORE_SETTINGS"):
			return i18n.T.ViewArmorHintCoreSettings
		case strings.Contains(rowStr, "ARMOR_ON"):
			return i18n.T.ViewArmorHintArmorOn
		case strings.Contains(rowStr, "ARMOR_OFF"):
			return i18n.T.ViewArmorHintArmorOff
		case strings.Contains(rowStr, "BACK"):
			return i18n.T.ViewArmorHintBack
		default:
			// ДИНАМИЧЕСКИЙ РУБЕЖ: ручные и авто-обнаруженные порты/службы
			return i18n.T.ViewArmorHintDynamicPerimeter
		}
	}

	// --------------------------------------------------------------------------

	// 1. Системный рубеж оптимизации ядра Linux (SYSCTL)
	statusSysctl := "[[ ACTIVE ]](fg:green,mod:bold) "
	if errSysctl := service.CheckSysctlProtocol(); errSysctl != nil {
		statusSysctl = "[[ DISARM ]](fg:red,mod:bold)"
	}
	rows = append(rows, fmt.Sprintf(i18n.T.ViewArmorListSysctl, statusSysctl))

	// 2. НОВОЕ В v8.0.2: Кнопка пересканирования периметра (перенесена из SETTINGS)
	// Размещена сразу после SYSCTL для быстрого доступа к актуализации инвентаря.
	rows = append(rows, i18n.T.ViewArmorListScanPerimeter)

	// 3. Ссылка на подменю детальной настройки (CORE_SETTINGS)
	rows = append(rows, i18n.T.ViewArmorListCoreSettings)

	// -------------------------------------------------------------
	// АВТО-ОБНАРУЖЕННЫЕ ПОРТЫ ИЗ ИНВЕНТАРЯ ПЕРИМЕТРА
	// Читает /etc/amberguard/perimeter_inventory.json и формирует
	// строки для каждого обнаруженного приложения.
	// -------------------------------------------------------------
	// Исключение дублирования портов при слиянии инвентаря
	processedPorts := make(map[int]bool)

	// 4. Загружаем ручные кастомные порты из JSON-реестра.
	//
	// v8.2.3 (R-3c): прямой вызов config.LoadInventory() заменяем на сервисный слой.
	// TUI больше не знает о деталях чтения JSON-инвентаря с диска,
	// соблюдая архитектурный контракт "TUI -> Service -> Config".
	ports, err := service.LoadInventory()
	if err == nil {
		for _, target := range ports {
			processedPorts[target.Port] = true
			// БЛОК 4.4: Используем генератор путей из config/paths.go
			tarpitRuntimeMarker := config.TarpitMarkerPath(target.Port)
			var statusPort string
			if _, errTarpit := os.Stat(tarpitRuntimeMarker); errTarpit == nil {
				statusPort = "[[ TARPIT ]](fg:magenta,mod:bold)"
			} else {
				if errCheck := service.CheckArmorRouteStatus(target.UUID); errCheck == nil {
					statusPort = "[[ ACTIVE ]](fg:green,mod:bold)"
				} else {
					statusPort = "[[ DISARM ]](fg:red,mod:bold) "
				}
			}
			paddedSysTag := fmt.Sprintf("%-15s", strings.ToUpper(target.SysTag))
			if len(paddedSysTag) > 15 {
				paddedSysTag = paddedSysTag[:15]
			}
			rows = append(rows, fmt.Sprintf(i18n.T.ViewArmorListManualPort, paddedSysTag, statusPort, target.Port, target.Service))
		}
	}

	// 5. АВТО-ОБОГАЩЕНИЕ: Подтягиваем реально обнаруженный на сервере софт.
	//
	// v8.2.3 (R-3c): прямой вызов config.LoadDetectedApps() заменяем на сервисный слой.
	// Устаревший комментарий "БЛОК 4.3" удалён, так как вызов больше не идёт из config/.
	apps, errApp := service.LoadDetectedApps()
	if errApp == nil {
		for _, app := range apps {
			if app.Port <= 0 {
				continue
			}
			defPort := app.Port
			// Если этот порт уже выведен из ручного инвентаря — не дублируем его
			if processedPorts[defPort] {
				continue
			}
			processedPorts[defPort] = true

			// Формируем динамический тег авто-обнаружения рубежа строго под реальный порт
			// Отображаем первые значения найденных служб
			dynamicID := fmt.Sprintf("PORT %d", defPort)
			// Траектории квитанций состояний мажорного стандарта v6.2.3
			// БЛОК 4.4: Используем генераторы путей из config/paths.go
			tarpitRuntimeMarker := config.TarpitMarkerPath(defPort)
			armorRuntimeMarker := config.ArmorMarkerPath(defPort)
			var statusPort string
			// Приоритет статусов: TARPIT > ARMOR > DISARM
			if _, errTarpit := os.Stat(tarpitRuntimeMarker); errTarpit == nil {
				statusPort = "[[ TARPIT ]](fg:magenta,mod:bold)"
			} else {
				// Опрашиваем исключительно быстрый файл-маркер квитанции.
				// Нагрузка на CPU падает до нуля, навигация становится мгновенной.
				if _, errArmor := os.Stat(armorRuntimeMarker); errArmor == nil {
					statusPort = "[[ ACTIVE ]](fg:green,mod:bold)"
				} else {
					statusPort = "[[ DISARM ]](fg:red,mod:bold) "
				}
			}

			formattedAppName := sys.PrettyServiceName(app.Name)
			// C9 (v8.1.3): порт канала спасения получает собственную метку,
			// чтобы не маскироваться под обычную службу Agl.
			displayName := i18n.T.ViewArmorPrefixService + formattedAppName
			if defPort == config.C.WebRescuePort {
				displayName = i18n.T.ViewArmorListWebKnockPort
			}
			// Определяем метку состояния приложения
			appStateLabel := "LISTEN"
			if !app.IsActive {
				appStateLabel = "STOPPED"
			}
			paddedID := fmt.Sprintf("%-15s", strings.ToUpper(dynamicID))
			if len(paddedID) > 15 {
				paddedID = paddedID[:15]
			}
			// Прежняя версия вывода информации
			// rows = append(rows, fmt.Sprintf(" %s │ %s │ Найдена служба %s на порту %d (%s)",
			// 	paddedID, statusPort, formattedAppName, defPort, appStateLabel))
			// Новая версия вывода
			rows = append(rows, fmt.Sprintf(i18n.T.ViewArmorListAutoService,
				paddedID, statusPort, appStateLabel, displayName))
		}
	}

	// -------------------------------------------------------------
	// 6. ГЛОБАЛЬНЫЕ ТРИГГЕРЫ (Всегда в конце списка)
	// -------------------------------------------------------------
	rows = append(rows, i18n.T.ViewArmorListArmorOn)
	rows = append(rows, i18n.T.ViewArmorListArmorOff)
	rows = append(rows, i18n.T.ViewArmorListBack)

	// Синхронизируем строки с глобальной памятью для роутера клавиш
	GlobalArmorMenuRows = rows

	// -------------------------------------------------------------
	// ИНИЦИАЛИЗАЦИЯ ШАПКИ (Синглтон, Высота 3 строки: y: 1 - 4)
	// -------------------------------------------------------------
	if ArmorHeader == nil {
		ArmorHeader = widgets.NewParagraph()
		ArmorHeader.SetRect(2, 1, 102, 4) // ЖЕСТКАЯ ФИКСАЦИЯ
		ArmorHeader.BorderStyle.Fg = ui.ColorCyan
		ArmorHeader.TitleStyle.Fg = ui.ColorCyan
		ArmorHeader.TitleStyle.Modifier = ui.ModifierBold
	}
	timeStr := time.Now().Format("15:04:05")
	dateStr := time.Now().Format("2006-01-02")
	ArmorHeader.Title = i18n.T.ViewArmorHeaderTitle
	ArmorHeader.Text = fmt.Sprintf(i18n.T.ViewArmorHeaderTextTpl, dateStr, timeStr)

	// -------------------------------------------------------------
	// СПИСОК МЕНЮ (Фиксированная высота 14 строк: y: 4 - 18)
	// -------------------------------------------------------------
	armorList := widgets.NewList()
	armorList.Title = i18n.T.ViewArmorListTitle
	armorList.Rows = rows
	armorList.SetRect(2, 4, 102, 18) // ЖЕСТКАЯ ФИКСАЦИЯ (было 18, оставлено 18 для совместимости)
	armorList.BorderStyle.Fg = ui.ColorCyan
	armorList.SelectedRow = selectedRow
	armorList.SelectedRowStyle = ui.NewStyle(ui.ColorBlack, ui.ColorGreen, ui.ModifierBold)

	// -------------------------------------------------------------
	// ТЕХНИЧЕСКИЙ БОКС ПОДСКАЗОК (Фиксированная высота)
	// -------------------------------------------------------------
	infoBox := widgets.NewParagraph()
	infoBox.Title = i18n.T.ViewArmorInfoBoxTitle
	infoBox.Text = generateHint(selectedRow) // Используем генератор, а не переданный текст
	infoBox.SetRect(2, 18, 102, 25)          // ЖЕСТКАЯ ФИКСАЦИЯ
	infoBox.BorderStyle.Fg = ui.ColorYellow
	infoBox.TextStyle.Fg = ui.ColorCyan
	infoBox.WrapText = false

	// -------------------------------------------------------------
	// СИСТЕМНАЯ КОНСОЛЬ НАВИГАЦИИ
	// -------------------------------------------------------------
	sysConsole := widgets.NewParagraph()
	sysConsole.Title = i18n.T.ViewArmorConsoleTitle
	sysConsole.Text = i18n.T.ViewArmorConsoleBody
	sysConsole.SetRect(2, 25, 102, 30) // ЖЕСТКАЯ ФИКСАЦИЯ
	sysConsole.BorderStyle.Fg = ui.ColorGreen
	sysConsole.TextStyle.Fg = ui.ColorCyan
	sysConsole.WrapText = false

	return []ui.Drawable{ArmorHeader, armorList, infoBox, sysConsole}, len(armorList.Rows)
}
