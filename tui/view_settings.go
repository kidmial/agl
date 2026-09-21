// tui/view_settings.go
package tui

import (
	// Добавлен для маршалинга структуры в текстовый JSON-формат
	"fmt"
	"os" // Добавлен для нативной работы с дескрипторами и POSIX Atomic Rename

	// "path/filepath" // Добавлен для вычисления абсолютных траекторий config.json
	"agl/config"
	"agl/internal/buildinfo"
	"agl/internal/service" // Интегрируем пакет брони для проверки active-маркеров рантайма
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/crypto/bcrypt"

	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"

	"agl/i18n"
)

type SettingItem struct {
	Key         string
	Label       func() string
	Group       string
	IsHeader    bool
	ValueGetter func() string
}

var (
	SettingsRegistry []SettingItem
	ActiveRowIndex   = 0
	IsEditMode       = false
	CurrentInputBuf  = ""
)

var digitOnlyRegex = regexp.MustCompile(`^[0-9]+$`)

// InitSettingsRegistry собирает расширенную матрицу конфигурации периметра Amber Guard Lab v6.3.9
func InitSettingsRegistry() {
	if len(SettingsRegistry) > 0 {
		return
	}
	SettingsRegistry = []SettingItem{
		// --- ГРУППА 1: РЕГЛАМЕНТ ЛОКАЛИЗАЦИИ И ИНТЕРФЕЙСА ---
		{IsHeader: true, Group: "LANG"},
		{Key: "lang", Label: func() string { return i18n.T.SettingsLabelLang }, Group: "LANG", ValueGetter: func() string { return strings.ToUpper(config.C.Lang) }},
		// --- ГРУППА 2: ПАРАМЕТРЫ ЯДРА ДЕГТЯРНОЙ ЯМЫ SSH ---
		{IsHeader: true, Group: "TARPIT"},
		{Key: "standard_ssh_port", Label: func() string { return i18n.T.SettingsLabelStandardSshPort }, Group: "TARPIT", ValueGetter: func() string { return fmt.Sprintf("%d", config.C.StandardSshPort) }},
		{Key: "real_ssh_port", Label: func() string { return i18n.T.SettingsLabelRealSshPort }, Group: "TARPIT", ValueGetter: func() string { return fmt.Sprintf("%d", config.C.RealSshPort) }},
		{Key: "delay_step", Label: func() string { return i18n.T.SettingsLabelDelayStep }, Group: "TARPIT", ValueGetter: func() string { return fmt.Sprintf("%d", config.C.DelayStep) }},
		{Key: "delay_variance", Label: func() string { return i18n.T.SettingsLabelDelayVariance }, Group: "TARPIT", ValueGetter: func() string { return fmt.Sprintf("%d", config.C.DelayVariance) }},
		{Key: "tarpit_max_conn", Label: func() string { return i18n.T.SettingsLabelTarpitMaxConn }, Group: "LIMITS", ValueGetter: func() string { return fmt.Sprintf("%d", config.C.TarpitMaxConn) }},
		// --- ГРУППА 3: СИСТЕМНЫЕ ЛИМИТЫ И АНТИДОС-ОБОРОНА ---
		{IsHeader: true, Group: "LIMITS"},
		{Key: "admin_idle_timeout", Label: func() string { return i18n.T.SettingsLabelAdminIdleTimeout }, Group: "LIMITS", ValueGetter: func() string { return fmt.Sprintf("%d", config.C.AdminIdleTimeout) }},
		{Key: "web_rescue_enabled", Label: func() string { return i18n.T.SettingsLabelWebRescueEnabled }, Group: "LIMITS", ValueGetter: func() string {
			if config.C.WebRescueEnabled {
				return "1"
			}
			return "0"
		}},
		{Key: "web_rescue_port", Label: func() string { return i18n.T.SettingsLabelWebRescuePort }, Group: "LIMITS", ValueGetter: func() string { return fmt.Sprintf("%d", config.C.WebRescuePort) }},
		{Key: "web_rescue_password", Label: func() string { return i18n.T.SettingsLabelWebRescuePassword }, Group: "LIMITS", ValueGetter: func() string {
			if config.C.WebRescuePasswordHash != "" {
				return i18n.T.ViewSettingsPasswordSet
			}
			return i18n.T.ViewSettingsPasswordNotSet
		}},
		// --- ГРУППА 4: ИНВЕНТАРИЗАЦИЯ И СЕРВИСНЫЕ КОМАНДЫ ---
		{IsHeader: true, Group: "SYSTEM"},
		// --- M5 (v8.4.0): Автообновления сервера (бинарные тумблеры, стрелки ←/→) ---
		{Key: "auto_update_enabled", Label: func() string { return i18n.T.SettingsLabelAutoUpdateEnabled }, Group: "SYSTEM", ValueGetter: func() string {
			if config.C.AutoUpdateEnabled {
				return "1"
			}
			return "0"
		}},
		{Key: "auto_update_reboot", Label: func() string { return i18n.T.SettingsLabelAutoUpdateReboot }, Group: "SYSTEM", ValueGetter: func() string {
			if config.C.AutoUpdateReboot {
				return "1"
			}
			return "0"
		}},
		{Key: "BACK", Label: func() string { return i18n.T.SettingsLabelBack }, Group: "SYSTEM"},
	}
}

// settingsGroupHeader возвращает локализованный заголовок группы настроек по машинному тегу группы.
func settingsGroupHeader(group string) string {
	switch group {
	case "LANG":
		return i18n.T.SettingsGroupLang
	case "TARPIT":
		return i18n.T.SettingsGroupTarpit
	case "LIMITS":
		return i18n.T.SettingsGroupLimits
	case "SYSTEM":
		return i18n.T.SettingsGroupSystem
	default:
		return i18n.T.SettingsGroupDefault
	}
}

// === ФИКС v8.4.1: Нормализация позиции курсора SETTINGS ===
// ensureActiveRowSelectable гарантирует, что ActiveRowIndex указывает
// на выбираемый пункт, а не на заголовок группы (у заголовков ValueGetter = nil,
// вызов которого раньше ронял панель в nil pointer dereference).
// Вызывается при отрисовке и при обработке событий:
//   - при первом входе курсор встаёт на первый пункт (lang), а не «в пустоту»;
//   - запомненная между визитами позиция СОХРАНЯЕТСЯ — коррекция срабатывает
//     только если индекс указывает на заголовок или вышел за границы.
func ensureActiveRowSelectable() {
	if ActiveRowIndex < 0 || ActiveRowIndex >= len(SettingsRegistry) {
		ActiveRowIndex = 0
	}
	if !SettingsRegistry[ActiveRowIndex].IsHeader {
		return
	}
	for i := ActiveRowIndex + 1; i < len(SettingsRegistry); i++ {
		if !SettingsRegistry[i].IsHeader {
			ActiveRowIndex = i
			return
		}
	}
}

// DrawSettingsGrid производит динамическую транзакционную отрисовку экрана SETTINGS
func DrawSettingsGrid(alertMessage string) []ui.Drawable {
	InitSettingsRegistry()
	ensureActiveRowSelectable() // ФИКС v8.4.1: курсор всегда на выбираемом пункте

	// 1. Сборка верхней информационной шапки
	header := widgets.NewList()
	// header.Title = i18n.T.SettingsUiTitleBox
	header.Title = fmt.Sprintf(" Amber Guard Laboratory - %s ", buildinfo.Version)
	header.Rows = []string{"                           " + i18n.T.SettingsUiHeaderDesc}
	// ЖЕСТКАЯ ФИКСАЦИЯ КООРДИНАТ
	header.SetRect(2, 1, 102, 4)
	header.BorderStyle.Fg = ui.ColorCyan
	header.TitleStyle.Fg = ui.ColorYellow
	header.TextStyle.Fg = ui.ColorYellow
	header.TextStyle.Modifier = ui.ModifierBold

	// 2. Сборка центрального интерактивного блока параметров
	formBox := widgets.NewList()
	formBox.Title = i18n.T.SettingsUiFormTitle
	// ЖЕСТКАЯ ФИКСАЦИЯ КООРДИНАТ (ВЫСОТА ДО 23 СТРОКИ)
	formBox.SetRect(2, 4, 102, 24)
	formBox.BorderStyle.Fg = ui.ColorCyan
	formBox.SelectedRowStyle = ui.NewStyle(ui.ColorBlack, ui.ColorGreen, ui.ModifierBold)

	var visibleRows []string
	tuiToRegistryMap := make(map[int]int)
	visibleLineCounter := 0
	for regIdx, item := range SettingsRegistry {
		if item.IsHeader {
			headerText := settingsGroupHeader(item.Group)
			// КОСМЕТИКА v8.4.1: пустая строка перед заголовком убрана —
			// меню целиком помещается в кадр (17 строк из 18 доступных).
			// Группировка держится цветом, а не воздухом.
			// Разметка живёт в коде, а не в словаре (доктрина 6.7).
			visibleRows = append(visibleRows, " ["+strings.TrimSpace(headerText)+"](fg:cyan,mod:bold)")
			visibleLineCounter++
			continue
		}
		label := item.Label()
		cursor := "     "
		if regIdx == ActiveRowIndex {
			cursor = " --> "
		}
		var finalRowStr string
		if item.Key == "BACK" {
			finalRowStr = fmt.Sprintf("%-5s%s", cursor, label)
		} else {
			currentVal := item.ValueGetter()
			// M5 (v8.4.0): бинарные тумблеры подсвечиваются жёлтыми скобками, как язык
			if item.Key == "lang" || item.Key == "auto_update_enabled" || item.Key == "auto_update_reboot" {
				finalRowStr = fmt.Sprintf("%-5s%-55s:  [[ %s ]](fg:yellow,mod:bold)", cursor, label, currentVal)
			} else {
				finalRowStr = fmt.Sprintf("%-5s%-55s:  %s", cursor, label, currentVal)
			}
		}
		visibleRows = append(visibleRows, finalRowStr)
		tuiToRegistryMap[visibleLineCounter] = regIdx
		visibleLineCounter++
	}
	formBox.Rows = visibleRows

	tuiTargetFocusRow := 0
	for tuiIdx, regIdx := range tuiToRegistryMap {
		if regIdx == ActiveRowIndex {
			tuiTargetFocusRow = tuiIdx
			break
		}
	}
	formBox.SelectedRow = tuiTargetFocusRow

	// 3. Расширенный нижний бокс консоли (Координаты 23 -> 29, высота ровно 6 символов)
	statusBox := widgets.NewParagraph()
	statusBox.SetRect(2, 24, 102, 30)
	if IsEditMode {
		statusBox.Title = i18n.T.SettingsUiEditingMode
		currentItem := SettingsRegistry[ActiveRowIndex]
		label := currentItem.Label()
		statusBox.Text = fmt.Sprintf("  %s: [ %s ]\n  %s: [ %s_ ](fg:white,mod:bold)",
			i18n.T.SettingsUiEnterNew, label,
			i18n.T.SettingsUiInputPrompt, CurrentInputBuf)
		statusBox.BorderStyle.Fg = ui.ColorGreen
		statusBox.TextStyle.Fg = ui.ColorWhite
		statusBox.WrapText = false
	} else {
		statusBox.Title = i18n.T.SettingsUiStatusConsoleTitle
		if alertMessage != "" {
			// Выводим алармы ошибок или сообщения об успешном сканировании
			if strings.HasPrefix(alertMessage, "[УСПЕХ]") {
				statusBox.Text = "\n " + alertMessage
				statusBox.BorderStyle.Fg = ui.ColorGreen // При успехе рамка становится зеленой
				statusBox.TextStyle.Fg = ui.ColorWhite
			} else {
				statusBox.Text = "\n  ⚠️ [ОТКАЗ ЗАПИСИ](fg:red,mod:bold): " + alertMessage
				statusBox.BorderStyle.Fg = ui.ColorRed
				statusBox.TextStyle.Fg = ui.ColorYellow
			}
		} else {
			statusBox.Text = fmt.Sprintf(
				" %s\n %s\n %s",
				i18n.T.SettingsUiHintNav1,
				i18n.T.SettingsUiHintNav2,
				fmt.Sprintf(i18n.T.SettingsUiHintEsc, "[ESC](fg:yellow,mod:bold)"),
			)
			statusBox.BorderStyle.Fg = ui.ColorYellow
			statusBox.TextStyle.Fg = ui.ColorCyan
			statusBox.WrapText = false
		}
	}
	return []ui.Drawable{header, formBox, statusBox}
}

// HandleSettingsNavigation обрабатывает транзакционные перемещения и клики по кнопкам
func HandleSettingsNavigation(key string) (bool, string) {
	InitSettingsRegistry()
	ensureActiveRowSelectable() // ФИКС v8.4.1: Enter по заголовку больше не обработается
	currentItem := SettingsRegistry[ActiveRowIndex]

	if IsEditMode {
		if key == "<Escape>" {
			IsEditMode = false
			CurrentInputBuf = ""
			return false, ""
		}
		if key == "<Enter>" {
			IsEditMode = false
			errStr := SaveSettingValueToDisk(currentItem.Key, CurrentInputBuf)
			CurrentInputBuf = ""
			return true, errStr
		}
		if key == "<Backspace>" || key == "C-8" {
			if len(CurrentInputBuf) > 0 {
				CurrentInputBuf = CurrentInputBuf[:len(CurrentInputBuf)-1]
			}
			return false, ""
		}
		if len(key) == 1 {
			CurrentInputBuf += key
			return false, ""
		}
		return false, ""
	}

	// ОБЫЧНЫЙ РЕЖИМ НАВИГАЦИИ ПО МЕНЮ (Без циклической прокрутки. С триггером для выбора языка стрелками влево/вправо)
	switch key {
	case "<Up>":
		savedIndex := ActiveRowIndex
		for {
			ActiveRowIndex--
			if ActiveRowIndex < 0 {
				ActiveRowIndex = savedIndex
				break
			}
			if !SettingsRegistry[ActiveRowIndex].IsHeader {
				break
			}
		}
	case "<Down>":
		savedIndex := ActiveRowIndex
		for {
			ActiveRowIndex++
			if ActiveRowIndex >= len(SettingsRegistry) {
				ActiveRowIndex = savedIndex
				break
			}
			if !SettingsRegistry[ActiveRowIndex].IsHeader {
				break
			}
		}
	// НОВЫЙ ИНТЕЛЛЕКТУАЛЬНЫЙ ТРИГГЕР ЛОКАЛИЗАЦИИ ПО СТРЕЛКАМ ВЛЕВО/ВПРАВО
	case "<Left>", "<Right>":
		if currentItem.Key == "lang" {
			currentLang := config.C.Lang
			newLang := "en"
			if currentLang == "en" {
				newLang = "ru"
			}
			// v8.3.0 (6.9): config.Update + i18n.InitLanguage внутри SaveSettingValueToDisk.
			// Возвращаем true — сигнал циклу событий на полную перерисовку
			// всех виджетов SETTINGS с новым активным языком.
			errStr := SaveSettingValueToDisk("lang", newLang)
			return true, errStr
		}
		// M5 (v8.4.0): бинарные тумблеры — инверсия 0/1 стрелками без входа в режим ввода
		if currentItem.Key == "auto_update_enabled" || currentItem.Key == "auto_update_reboot" {
			newVal := "1"
			if currentItem.ValueGetter() == "1" {
				newVal = "0"
			}
			errStr := SaveSettingValueToDisk(currentItem.Key, newVal)
			return true, errStr
		}
	case "<Enter>":
		// ФИКС v8.4.1: заголовок группы не выбираем — игнорируем Enter,
		// иначе nil ValueGetter уронит панель (инцидент nil pointer dereference).
		if currentItem.IsHeader {
			return false, ""
		}
		if currentItem.Key == "BACK" {
			return true, ""
		}
		// ПРЕДОХРАНИТЕЛЬ: Запрещаем провал в режим ручного ввода (IsEditMode) для языка!
		if currentItem.Key == "lang" {
			return false, ""
		}
		// Для всех остальных числовых параметров — штатно открываем окно транзакции ввода
		IsEditMode = true
		if currentItem.Key == "web_rescue_password" {
			CurrentInputBuf = "" // M3g: не подставляем старый пароль/хэш в буфер
		} else {
			CurrentInputBuf = currentItem.ValueGetter()
		}
	case "<Escape>":
		return true, ""
	}
	return false, ""
}

// SaveSettingValueToDisk производит атомарную фиксацию измененного параметра на диск по стандартам Amber Guard Lab
func SaveSettingValueToDisk(key, value string) string {
	value = strings.TrimSpace(value)
	if key == "SCAN_PERIMETER" || key == "BACK" {
		return ""
	}
	if value == "" {
		return i18n.T.SettingsUiEmptyVal
	}

	// v8.2.2 (волна гонки 2.3): все мутации — через config.Update
	// (клон снепшота → мутация → публикация → атомарная запись на диск).
	// In-place мутации опубликованного снепшота устранены; явный SaveConfig
	// удалён — он теперь внутри Update (единая точка saveConfigLocked).
	var updateErr error
	if key == "web_rescue_password" {
		// M3g (v8.1.3): минимум 8, технический потолок 128.
		// Храним ТОЛЬКО SHA-256 хэш; plaintext в синглтоне не задерживается.
		if len(value) < 8 {
			return i18n.T.ViewSettingsErrPasswordTooShort
		}
		if len(value) > 128 {
			return i18n.T.ViewSettingsErrPasswordTooLong
		}
		// v8.2.0 (1.7): bcrypt с солью внутри хэша вместо SHA-256.
		hash, errB := bcrypt.GenerateFromPassword([]byte(value), bcrypt.DefaultCost)
		if errB != nil {
			return i18n.T.ViewSettingsErrHashGenerationFail
		}
		updateErr = config.Update(config.FileConfig, func(c *config.Config) {
			c.WebRescuePasswordHash = string(hash)
			c.WebRescuePassword = ""
		})
	} else if key != "lang" {
		if !digitOnlyRegex.MatchString(value) {
			return i18n.T.SettingsErrDigits
		}
		valInt, _ := strconv.Atoi(value)
		// M5 (v8.4.0): бинарные тумблеры валидируются строго 0/1 и минуют
		// общую границу 1..65535 (ноль — легальное значение тумблера).
		// ПОПУТНЫЙ ФИКС: web_rescue_enabled раньше НЕ мог принять "0" через
		// эту границу (valInt <= 0) — канал спасения нельзя было выключить вводом.
		isBinaryKey := key == "auto_update_enabled" || key == "auto_update_reboot" || key == "web_rescue_enabled"
		if isBinaryKey {
			if valInt != 0 && valInt != 1 {
				return i18n.T.SettingsUiOutOfBounds
			}
		} else if valInt <= 0 || valInt > 65535 {
			return i18n.T.SettingsUiOutOfBounds
		}
		// ВЕРСИЯ 6.0.0: ИБ-ПРЕДОХРАНИТЕЛИ И ВАЛИДАЦИЯ ПЕРЕСЕЧЕНИЙ ПОРТОВ
		if key == "standard_ssh_port" || key == "real_ssh_port" {
			// 1. Проверяем дублирование портов в памяти во избежание паники сокетов ядра
			if key == "standard_ssh_port" && valInt == config.C.RealSshPort {
				return i18n.T.ViewSettingsErrPortsConflictStandardReal
			}
			if key == "real_ssh_port" && valInt == config.C.StandardSshPort {
				return i18n.T.ViewSettingsErrPortsConflictRealStandard
			}
			// 2. Блокируем изменение сетевой геометрии, если прямо сейчас развернуты активные рубежи защиты
			if _, errCheck := os.Stat(config.TarpitMarkerPath(22)); errCheck == nil { // РЕБРЕНДИНГ
				return i18n.T.ViewSettingsErrPortsChangeTarpitActive
			}
			if _, errMoved := os.Stat(config.MarkerSshMoved); errMoved == nil { // РЕБРЕНДИНГ
				return i18n.T.ViewSettingsErrPortsChangeCastlingActive
			}
		}
		updateErr = config.Update(config.FileConfig, func(c *config.Config) {
			switch key {
			case "standard_ssh_port":
				c.StandardSshPort = valInt
			case "real_ssh_port":
				c.RealSshPort = valInt
			case "delay_step":
				c.DelayStep = valInt
			case "delay_variance":
				c.DelayVariance = valInt
			case "tarpit_max_conn":
				c.TarpitMaxConn = valInt
			case "admin_idle_timeout":
				c.AdminIdleTimeout = valInt
			case "web_rescue_enabled":
				// M3 (v8.1.2): Канал спасения веб-стук. 0 = выкл, 1 = вкл.
				c.WebRescueEnabled = (valInt == 1)
			case "web_rescue_port":
				// M3b (v8.1.2): Порт автономного HTTP-сервера веб-стука.
				c.WebRescuePort = valInt
			case "auto_update_enabled":
				// M5 (v8.4.0): 0=выкл, 1=вкл. Сверка юнитов — после публикации снепшота.
				c.AutoUpdateEnabled = (valInt == 1)
			case "auto_update_reboot":
				// M5 (v8.4.0): авто-ребут после обновлений ядра.
				c.AutoUpdateReboot = (valInt == 1)
			}
		})
	} else {
		langLower := strings.ToLower(value)
		if langLower != "ru" && langLower != "en" {
			return i18n.T.SettingsUiLangSupport
		}
		updateErr = config.Update(config.FileConfig, func(c *config.Config) {
			c.Lang = langLower
		})
		// v8.3.0 (6.9): Язык TUI переключаем только после успешной публикации снепшота.
		if updateErr == nil {
			i18n.InitLanguage(langLower)
		}
	}

	// Ошибка Update = конфиг не опубликован и не сохранён: выходим раньше,
	// пост-синхронизации не выполняются (та же семантика, что и в оригинале
	// при ошибке SaveConfig).
	if updateErr != nil {
		return i18n.T.ViewSettingsErrSavePrefix + updateErr.Error()
	}

	// M5 (v8.4.0): декларативная сверка systemd-юнитов автообновления.
	// Декларация в конфиге УЖЕ опубликована → приводим факт через сервисный мост.
	if key == "auto_update_enabled" {
		if errUnits := service.SyncAutoUpdateUnits(config.C.AutoUpdateEnabled); errUnits != nil {
			return i18n.T.ViewSettingsErrSavePrefix + errUnits.Error()
		}
	}

	// АКТИВАЦИЯ КОНТУРА SELF-HEALING (САМОВОССТАНОВЛЕНИЯ)
	if key == "standard_ssh_port" || key == "real_ssh_port" {
		service.ResetUfwCacheSnapshot()
	}

	// =================================================================
	// ФИКС v8.0.2: Синхронизация глобальных настроек с инвентарём ловушек
	// Если админ меняет параметры ядра ловушки, обновляем базовую ловушку в инвентаре
	// =================================================================
	if key == "delay_step" || key == "delay_variance" || key == "tarpit_max_conn" {
		// API-forward v8.0.3 (Аудит №1): синхронизация через контроллер,
		// под транзакцией flock. TUI больше не пишет в инвентарь напрямую.
		_ = service.SyncGlobalConfigToCoreTrap()
		// Демон подхватит новые параметры на следующем тике (15 сек)
	}
	return ""
}

// execCommandSelfHealingCheck — вспомогательный низкоуровневый триггер синхронизации ОС при рассинхронизации параметров
func execCommandSelfHealingCheck() error {
	// Самовосстановление логики: проверяем, что sshd_config.d/amberguard.conf соответствует текущему ОЗУ-состоянию
	// В продакшене здесь дергается метод пакета sys/self_healing
	return nil
}
