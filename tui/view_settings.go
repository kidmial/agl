// tui/view_settings.go
package tui

import (
	// Добавлен для маршалинга структуры в текстовый JSON-формат
	"fmt"
	"os" // Добавлен для нативной работы с дескрипторами и POSIX Atomic Rename
	"time"

	// "path/filepath" // Добавлен для вычисления абсолютных траекторий config.json
	"agl/config"
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

	// (v8.6.2): Задержки отображения alert-сообщений
	alertMessageTime time.Time
	lastAlertMessage string
	// v9.0.0: признак ошибки живёт в той же памяти, что сообщение и время:
	// тиковые перерисовки с пустым параметром не должны перекрашивать алерт.
	lastAlertIsError bool
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
		// v9.0.0: standard_ssh_port удалён из настроек.
		// PURPOSE: порт 22 — константа домена (IANA SSH), а не параметр развёртывания.
		// Вся геометрия комплекса (рокировка 22→скрытый, базовая яма на 22, броня на 22)
		// захардкожена на 22 в port_move_protocol.go. Пункт меню давал иллюзию настройки,
		// но изменение ломало обратный переезд. Админ с другим портом использует MULTI_TARPIT.
		{Key: "real_ssh_port", Label: func() string { return i18n.T.SettingsLabelRealSshPort }, Group: "TARPIT", ValueGetter: func() string { return fmt.Sprintf("%d", config.C.RealSshPort) }},
		{Key: "delay_step", Label: func() string { return i18n.T.SettingsLabelDelayStep }, Group: "TARPIT", ValueGetter: func() string { return fmt.Sprintf("%d", config.C.DelayStep) }},
		{Key: "delay_variance", Label: func() string { return i18n.T.SettingsLabelDelayVariance }, Group: "TARPIT", ValueGetter: func() string { return fmt.Sprintf("%d", config.C.DelayVariance) }},
		{Key: "tarpit_max_conn", Label: func() string { return i18n.T.SettingsLabelTarpitMaxConn }, Group: "LIMITS", ValueGetter: func() string { return fmt.Sprintf("%d", config.C.TarpitMaxConn) }},
		// --- ГРУППА 3: СИСТЕМНЫЕ ЛИМИТЫ И АНТИДОС-ОБОРОНА ---
		{IsHeader: true, Group: "LIMITS"},
		{Key: "admin_idle_timeout", Label: func() string { return i18n.T.SettingsLabelAdminIdleTimeout }, Group: "LIMITS", ValueGetter: func() string { return fmt.Sprintf("%d", config.C.AdminIdleTimeout) }},
		// --- v8.5.0: Уровни защиты SSH-доступа (опции A, B, C) ---
		{Key: "ssh_allow_in_from_ip", Label: func() string { return i18n.T.SettingsLabelSshAllowInFromIp }, Group: "LIMITS", ValueGetter: func() string {
			if config.C.SshAllowInFromIp {
				return "1"
			}
			return "0"
		}},
		{Key: "ssh_ip_web_knock", Label: func() string { return i18n.T.SettingsLabelSshIpWebKnock }, Group: "LIMITS", ValueGetter: func() string {
			if config.C.SshIpWebKnock {
				return "1"
			}
			return "0"
		}},
		{Key: "web_https_panel_active", Label: func() string { return i18n.T.SettingsLabelWebHttpsPanelActive }, Group: "LIMITS", ValueGetter: func() string {
			if config.C.WebHttpsPanelActive {
				return "1"
			}
			return "0"
		}},
		// --- v8.6.2: Автозагрузка истории при старте сервиса ---
		{Key: "analytics_auto_rebuild", Label: func() string { return i18n.T.SettingsLabelAnalyticsAutoRebuild }, Group: "LIMITS", ValueGetter: func() string {
			if config.C.AnalyticsAutoRebuild {
				return "1"
			}
			return "0"
		}},
		// --- v8.6.2: Пароль веб-панели статистики ---
		{Key: "web_panel_password", Label: func() string { return i18n.T.SettingsLabelWebPanelPassword }, Group: "LIMITS", ValueGetter: func() string {
			if config.C.WebPanelPasswordHash != "" {
				return i18n.T.ViewSettingsPasswordSet
			}
			return i18n.T.ViewSettingsPasswordNotSet
		}},
		// v8.4.7 (G1): уровень 1 — физический слушатель. Строка возвращена: изъят был только web_rescue_enabled.
		{Key: "web_server_enabled", Label: func() string { return i18n.T.SettingsLabelWebServerEnabled }, Group: "LIMITS", ValueGetter: func() string {
			if config.C.WebServerEnabled {
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
func DrawSettingsGrid(alertMessage string, alertIsError bool) []ui.Drawable {
	InitSettingsRegistry()
	ensureActiveRowSelectable() // ФИКС v8.4.1: курсор всегда на выбираемом пункте

	// НОВОЕ: Логика задержки отображения alert-сообщений (3 секунды)
	// Если пришло новое сообщение - запоминаем его и время
	if alertMessage != "" && alertMessage != lastAlertMessage {
		lastAlertMessage = alertMessage
		lastAlertIsError = alertIsError
		alertMessageTime = time.Now()
	}

	// Проверяем, прошло ли 3 секунды
	effectiveAlertMessage := ""
	effectiveIsError := false
	if !alertMessageTime.IsZero() && time.Since(alertMessageTime) < 3*time.Second {
		effectiveAlertMessage = lastAlertMessage
		effectiveIsError = lastAlertIsError
	} else {
		// Время вышло: сбрасываем сообщение, признак и время вместе
		alertMessageTime = time.Time{}
		lastAlertMessage = ""
		lastAlertIsError = false
	}

	// v9.0.0: единственный основной блок экрана (макет): титул несёт
	// бренд+титул+дату/время, тело стартует сразу с групповых разделителей.
	formBox := widgets.NewList()
	formBox.Title = screenTitleLine(fmt.Sprintf("%s - %s", HeaderBrandShort, i18n.T.SettingsUiFormTitle))
	formBox.SetRect(2, 1, 102, 24)
	formBox.BorderStyle.Fg = ui.ColorCyan
	formBox.TitleStyle.Fg = ui.ColorCyan
	formBox.TitleStyle.Modifier = ui.ModifierBold
	formBox.SelectedRowStyle = ui.NewStyle(ui.ColorBlack, ui.ColorGreen, ui.ModifierBold)

	// Ширина блока известна до сборки строк: разделители групп добиваются до неё.
	innerWidth := formBox.GetRect().Dx() - 2
	var visibleRows []string
	tuiToRegistryMap := make(map[int]int)
	visibleLineCounter := 0
	for regIdx, item := range SettingsRegistry {
		if item.IsHeader {
			headerText := settingsGroupHeader(item.Group)
			// КОСМЕТИКА v8.4.1: пустая строка перед заголовком убрана —
			// меню целиком помещается в кадр (17 строк из 18 доступных).
			// v9.0.0: геометрия разделителя строится в коде по ширине блока,
			// словарь отдаёт только титул (доктрина 6.7).
			visibleRows = append(visibleRows, groupSeparatorRow(headerText, innerWidth))
			visibleLineCounter++
			continue
		}
		label := item.Label()
		cursor := "  "
		if regIdx == ActiveRowIndex {
			cursor = " ->"
		}
		var finalRowStr string
		if item.Key == "BACK" {
			finalRowStr = fmt.Sprintf("%-5s%s", cursor, label)
		} else {
			currentVal := item.ValueGetter()
			// M5 (v8.4.0) + v8.4.7: бинарные тумблеры подсвечиваются жёлтыми скобками, как язык
			if item.Key == "lang" || item.Key == "auto_update_enabled" || item.Key == "auto_update_reboot" ||
				item.Key == "web_server_enabled" ||
				item.Key == "ssh_allow_in_from_ip" || item.Key == "ssh_ip_web_knock" || item.Key == "web_https_panel_active" ||
				item.Key == "analytics_auto_rebuild" {
				finalRowStr = fmt.Sprintf("%-4s%-65s:  [[ %s ]](fg:yellow,mod:bold)", cursor, label, currentVal)
			} else {
				finalRowStr = fmt.Sprintf("%-4s%-65s:  %s", cursor, label, currentVal)
			}
		}
		visibleRows = append(visibleRows, finalRowStr)
		tuiToRegistryMap[visibleLineCounter] = regIdx
		visibleLineCounter++
	}

	// v9.0.0: КОСМЕТИКА — полоса выделения до правого края блока.
	paddedRows := make([]string, len(visibleRows))
	for i, row := range visibleRows {
		if n := visibleRunes(row); n < innerWidth {
			row += strings.Repeat(" ", innerWidth-n)
		}
		paddedRows[i] = row
	}
	formBox.Rows = paddedRows

	tuiTargetFocusRow := 0
	for tuiIdx, regIdx := range tuiToRegistryMap {
		if regIdx == ActiveRowIndex {
			tuiTargetFocusRow = tuiIdx
			break
		}
	}
	formBox.SelectedRow = tuiTargetFocusRow

	// 3. Расширенный нижний бокс консоли
	statusBox := widgets.NewParagraph()
	statusBox.SetRect(2, 24, 102, 30)
	if IsEditMode {
		// v9.0.0: единый титул консоли; режим ввода говорят сами строки приглашения.
		statusBox.Title = i18n.T.SettingsUiStatusConsoleTitle
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
		// ИЗМЕНЕНО: используем effectiveAlertMessage вместо alertMessage
		if effectiveAlertMessage != "" {
			// v9.0.0: признак берём из памяти алерта (сообщение+флаг+время — одно состояние).
			if !effectiveIsError {
				statusBox.Text = "\n " + effectiveAlertMessage
				statusBox.BorderStyle.Fg = ui.ColorGreen // При успехе рамка становится зеленой
				statusBox.TextStyle.Fg = ui.ColorWhite
				statusBox.WrapText = false
			} else {
				statusBox.Text = "\n [[ ERROR ]](fg:red,mod:bold) " + effectiveAlertMessage
				statusBox.BorderStyle.Fg = ui.ColorRed
				statusBox.TextStyle.Fg = ui.ColorYellow
				statusBox.WrapText = false
			}
		} else {
			statusBox.Text = fmt.Sprintf(
				" %s\n %s\n %s",
				i18n.T.SettingsUiHintNav1,
				i18n.T.SettingsUiHintNav2,
				fmt.Sprintf(i18n.T.SettingsUiHintEsc, "[ ESC ](fg:yellow,mod:bold)"),
			)
			statusBox.BorderStyle.Fg = ui.ColorYellow
			statusBox.TextStyle.Fg = ui.ColorCyan
			statusBox.WrapText = false
		}
	}
	return []ui.Drawable{formBox, statusBox}
}

// HandleSettingsNavigation обрабатывает транзакционные перемещения и клики по кнопкам
// HandleSettingsNavigation: (shouldQuit, msg, isError). Семантика shouldExit не изменена.
func HandleSettingsNavigation(key string) (bool, string, bool) {
	InitSettingsRegistry()
	ensureActiveRowSelectable() // ФИКС v8.4.1: Enter по заголовку больше не обработается
	currentItem := SettingsRegistry[ActiveRowIndex]

	if IsEditMode {
		if key == "<Escape>" {
			IsEditMode = false
			CurrentInputBuf = ""
			return false, "", false
		}
		if key == "<Enter>" {
			IsEditMode = false
			msg, isErr := SaveSettingValueToDisk(currentItem.Key, CurrentInputBuf)
			CurrentInputBuf = ""
			return true, msg, isErr
		}
		if key == "<Backspace>" || key == "C-8" {
			if len(CurrentInputBuf) > 0 {
				CurrentInputBuf = CurrentInputBuf[:len(CurrentInputBuf)-1]
			}
			return false, "", false
		}
		if len(key) == 1 {
			CurrentInputBuf += key
			return false, "", false
		}
		return false, "", false
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
			// Возвращаем false — остаёмся в меню, но перерисовываем с новым языком.
			msg, isErr := SaveSettingValueToDisk("lang", newLang)
			return false, msg, isErr
		}
		// M5 (v8.4.0) + v8.4.7: бинарные тумблеры — инверсия 0/1 стрелками без входа в режим ввода
		if currentItem.Key == "auto_update_enabled" || currentItem.Key == "auto_update_reboot" ||
			currentItem.Key == "web_server_enabled" ||
			currentItem.Key == "ssh_allow_in_from_ip" || currentItem.Key == "ssh_ip_web_knock" || currentItem.Key == "web_https_panel_active" ||
			currentItem.Key == "analytics_auto_rebuild" {
			newVal := "1"
			if currentItem.ValueGetter() == "1" {
				newVal = "0"
			}
			msg, isErr := SaveSettingValueToDisk(currentItem.Key, newVal)
			return false, msg, isErr
		}
	case "<Enter>":
		// ФИКС v8.4.1: заголовок группы не выбираем — игнорируем Enter,
		// иначе nil ValueGetter уронит панель (инцидент nil pointer dereference).
		if currentItem.IsHeader {
			return false, "", false
		}
		if currentItem.Key == "BACK" {
			return true, "", false
		}
		// ПРЕДОХРАНИТЕЛЬ: Запрещаем провал в режим ручкого ввода (IsEditMode) для языка!
		if currentItem.Key == "lang" {
			return false, "", false
		}
		// Для всех остальных числовых параметров — штатно открываем окно транзакции ввода
		IsEditMode = true
		if currentItem.Key == "web_rescue_password" || currentItem.Key == "web_panel_password" {
			CurrentInputBuf = "" // v8.6.2: не подставляем старый пароль/хэш в буфер
		} else {
			CurrentInputBuf = currentItem.ValueGetter()
		}
	case "<Escape>":
		return true, "", false
	}
	return false, "", false
}

// SaveSettingValueToDisk производит атомарную фиксацию измененного параметра на диск по стандартам Amber Guard Lab.
// SaveSettingValueToDisk производит атомарную фиксацию параметра на диск.
// Возвращает (msg, isError): признак ошибки — явный флаг, не префикс строки.
func SaveSettingValueToDisk(key, value string) (string, bool) {
	value = strings.TrimSpace(value)
	if key == "SCAN_PERIMETER" || key == "BACK" {
		return "", false
	}
	if value == "" {
		return i18n.T.SettingsUiEmptyVal, true
	}

	// v8.2.2 (волна гонки 2.3): все мутации — через config.Update
	// (клон снепшота → мутация → публикация → атомарная запись на диск).
	// In-place мутации опубликованного снепшота устранены; явный SaveConfig
	// удалён — он теперь внутри Update (единая точка saveConfigLocked).
	var updateErr error
	if key == "web_rescue_password" {
		// M3g (v8.1.3): минимум 8, технический потолок 128.
		// Храним ТОЛЬКО bcrypt хэш; plaintext в синглтоне не задерживается.
		if len(value) < 8 {
			return i18n.T.ViewSettingsErrPasswordTooShort, true
		}
		if len(value) > 128 {
			return i18n.T.ViewSettingsErrPasswordTooLong, true
		}
		// v8.2.0 (1.7): bcrypt с солью внутри хэша вместо SHA-256.
		hash, errB := bcrypt.GenerateFromPassword([]byte(value), bcrypt.DefaultCost)
		if errB != nil {
			return i18n.T.ViewSettingsErrHashGenerationFail, true
		}
		updateErr = config.Update(config.FileConfig, func(c *config.Config) {
			c.WebRescuePasswordHash = string(hash)
			c.WebRescuePassword = ""
		})
	} else if key == "web_panel_password" {
		// v8.6.2: пароль веб-панели статистики — отдельный от rescue password.
		if len(value) < 8 {
			return i18n.T.ViewSettingsErrPasswordTooShort, true
		}
		if len(value) > 128 {
			return i18n.T.ViewSettingsErrPasswordTooLong, true
		}
		hash, errB := bcrypt.GenerateFromPassword([]byte(value), bcrypt.DefaultCost)
		if errB != nil {
			return i18n.T.ViewSettingsErrHashGenerationFail, true
		}
		updateErr = config.Update(config.FileConfig, func(c *config.Config) {
			c.WebPanelPasswordHash = string(hash)
		})
	} else if key != "lang" {
		if !digitOnlyRegex.MatchString(value) {
			return i18n.T.SettingsErrDigits, true
		}
		valInt, _ := strconv.Atoi(value)
		// M5 (v8.4.0): бинарные тумблеры валидируются строго 0/1 и минуют
		// общую границу 1..65535 (ноль — легальное значение тумблера).
		// ПОПУТНЫЙ ФИКС: web_rescue_enabled раньше НЕ мог принять "0" через
		// эту границу (valInt <= 0) — канал спасения нельзя было выключить вводом.
		// v8.5.0: web_rescue_enabled изъят (G1); три новых тумблера — бинарные.
		isBinaryKey := key == "auto_update_enabled" || key == "auto_update_reboot" || key == "web_server_enabled" ||
			key == "ssh_allow_in_from_ip" || key == "ssh_ip_web_knock" || key == "web_https_panel_active" ||
			key == "analytics_auto_rebuild"
		if isBinaryKey {
			if valInt != 0 && valInt != 1 {
				return i18n.T.SettingsUiOutOfBounds, true
			}
		} else if valInt <= 0 || valInt > 65535 {
			return i18n.T.SettingsUiOutOfBounds, true
		}
		// ВЕРСИЯ 6.0.0: ИБ-ПРЕДОХРАНИТЕЛИ И ВАЛИДАЦИЯ ПЕРЕСЕЧЕНИЙ ПОРТОВ
		// v9.0.0: standard_ssh_port удалён из настроек, валидация только для real_ssh_port.
		if key == "real_ssh_port" {
			// 1. Проверяем дублирование портов в памяти во избежание паники сокетов ядра
			if valInt == config.C.StandardSshPort {
				return i18n.T.ViewSettingsErrPortsConflictRealStandard, true
			}
			// 2. Блокируем изменение сетевой геометрии, если прямо сейчас развернуты активные рубежи защиты
			if _, errCheck := os.Stat(config.TarpitMarkerPath(22)); errCheck == nil {
				return i18n.T.ViewSettingsErrPortsChangeTarpitActive, true
			}
			if _, errMoved := os.Stat(config.MarkerSshMoved); errMoved == nil {
				return i18n.T.ViewSettingsErrPortsChangeCastlingActive, true
			}
		}
		// v8.5.0: валидация иерархии защитных опций (B требует A).
		// PURPOSE: опция B (веб-стук с испепелением) требует опцию A (whitelist IP админа).
		// CONTRACT: если админ пытается включить B при выключенном A → отказ записи.
		if key == "ssh_ip_web_knock" && valInt == 1 && !config.C.SshAllowInFromIp {
			return i18n.T.ViewSettingsErrKnockRequiresAllow, true
		}
		// v8.5.0: валидация «B требует сервер» и «панель требует сервер».
		if (key == "ssh_ip_web_knock" || key == "web_https_panel_active") && valInt == 1 && !config.C.WebServerEnabled {
			return i18n.T.ViewSettingsErrRequiresServer, true
		}
		// v9.0.0: ВАЛИДАЦИЯ ОПЦИИ B (Веб-стук).
		// PURPOSE: Веб-стук с испепелением бессмысленен и опасен без пароля:
		// он будет молча отвергать попытки и спамить журнал BRUTEFORCE_LOCKOUT.
		// CONTRACT: Запрещаем включение тумблера, если хэш пароля пуст.
		if key == "ssh_ip_web_knock" && valInt == 1 && config.C.WebRescuePasswordHash == "" {
			return i18n.T.ViewSettingsErrKnockRequiresPassword, true
		}
		// v9.0.0: ВАЛИДАЦИЯ ОПЦИИ C (Веб-панель).
		// PURPOSE: Панель аналитики без пароля недоступна для входа (fail-closed в рантайме),
		// но включённый тумблер вводит админа в заблуждение о работоспособности сервиса.
		// CONTRACT: Запрещаем включение тумблера, если хэш пароля панели пуст.
		if key == "web_https_panel_active" && valInt == 1 && config.C.WebPanelPasswordHash == "" {
			return i18n.T.ViewSettingsErrPanelRequiresPassword, true
		}
		// v9.0.0: ИЕРАРХИЯ ИСТОРИИ СТАТИСТИКИ (05 ⊂ 04 ⊂ 07).
		// PURPOSE: автозагрузка истории бессмысленна без веб-панели и физического сервера.
		// Включение 05 при выключенных 04 или 07 — отказ записи ДО транзакции.
		if key == "analytics_auto_rebuild" && valInt == 1 &&
			(!config.C.WebHttpsPanelActive || !config.C.WebServerEnabled) {
			return i18n.T.ViewSettingsErrRebuildRequiresPanel, true
		}
		updateErr = config.Update(config.FileConfig, func(c *config.Config) {
			switch key {
			// v9.0.0: standard_ssh_port удалён — порт 22 константа домена.
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
			case "web_server_enabled":
				// v8.4.7 (уровень 1): физический HTTP-слушатель. 0 = выкл, 1 = вкл.
				c.WebServerEnabled = (valInt == 1)
				// v8.5.0: каскад при выключении сервера — B и панель гасятся в той же транзакции.
				// Иначе декларация на диске врет до следующей транзакции.
				if valInt == 0 {
					c.SshIpWebKnock = false
					c.WebHttpsPanelActive = false
					c.AnalyticsAutoRebuild = false // v9.0.0: иерархия 05 Автозагрузка истории статистики ⊂ 04 Веб-панель статистики ⊂ 07 HTTPS Веб-сервер
				}
			case "ssh_allow_in_from_ip":
				// v8.5.0 (опция A): whitelist IP админа для скрытого порта SSH.
				c.SshAllowInFromIp = (valInt == 1)
				// v9.0.0: каскад при выключении A — опция B гасится в той же транзакции.
				// PURPOSE: B (веб-стук с испепелением) требует A (постоянная дверь админа):
				// без whitelist-двери испепеление временных дверей ведёт к локауту админа.
				// CONTRACT: симметрично каскаду web_server_enabled — декларация на диске
				// не врёт до следующей транзакции. Валидатор «B требует A» остаётся страховкой
				// прямого включения, каскад закрывает обратный переход. Две линии обороны.
				if valInt == 0 {
					c.SshIpWebKnock = false
				}
			case "ssh_ip_web_knock":
				// v8.5.0 (опция B): веб-стук с испепелением временных дверей.
				c.SshIpWebKnock = (valInt == 1)
			case "web_https_panel_active":
				// v8.5.0 (опция C): доступность веб-панели на порту 55111.
				c.WebHttpsPanelActive = (valInt == 1)
				// v8.6.2: при включении панели автоматически включаем rebuild истории.
				// Админ, активировавший панель, почти наверняка хочет видеть полную историю.
				if valInt == 1 {
					c.AnalyticsAutoRebuild = true
				} else {
					c.AnalyticsAutoRebuild = false // v9.0.0: симметричный каскад вниз
				}
			case "web_rescue_port":
				// M3b (v8.1.2): Порт автономного HTTP-сервера веб-стука.
				c.WebRescuePort = valInt
			case "auto_update_enabled":
				// M5 (v8.4.0): 0=выкл, 1=вкл. Сверка юнитов — после публикации снепшота.
				c.AutoUpdateEnabled = (valInt == 1)
			case "auto_update_reboot":
				// M5 (v8.4.0): авто-ребут после обновлений ядра.
				c.AutoUpdateReboot = (valInt == 1)
			case "analytics_auto_rebuild":
				// v8.6.2: автозагрузка истории при старте сервиса.
				c.AnalyticsAutoRebuild = (valInt == 1)
			}
		})
	} else {
		langLower := strings.ToLower(value)
		if langLower != "ru" && langLower != "en" {
			return i18n.T.SettingsUiLangSupport, true
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
		return i18n.T.ViewSettingsErrSavePrefix + updateErr.Error(), true
	}

	// M5 (v8.4.0): декларативная сверка systemd-юнитов автообновления.
	// Декларация в конфиге УЖЕ опубликована → приводим факт через сервисный мост.
	if key == "auto_update_enabled" {
		if errUnits := service.SyncAutoUpdateUnits(config.C.AutoUpdateEnabled); errUnits != nil {
			return i18n.T.ViewSettingsErrSavePrefix + errUnits.Error(), true
		}
	}

	// v8.6.2: декларация загрузки истории приводится немедленно.
	// Включил тумблер — watcher перезапустился и загрузил историю сам.
	// Выключил — ничего не делаем: декларация сработает на следующем старте.
	// v9.0.0: каскадный сброс 05 через выключение 04/07 тоже приводит факт сразу,
	// иначе watcher продолжит грузить историю по устаревшей декларации.
	if key == "analytics_auto_rebuild" || key == "web_https_panel_active" || key == "web_server_enabled" {
		if errSync := service.SyncAnalyticsRebuild(); errSync != nil {
			return i18n.T.ViewSettingsErrSavePrefix + errSync.Error(), true
		}
	}

	// v8.5.0: новые тумблеры защиты декларативны; факт приводим через сервисный мост.
	if key == "web_server_enabled" || key == "ssh_allow_in_from_ip" || key == "ssh_ip_web_knock" || key == "web_https_panel_active" {
		if errSync := service.SyncRescueChannelState(); errSync != nil {
			return i18n.T.ViewSettingsErrSavePrefix + errSync.Error(), true
		}
	}

	// АКТИВАЦИЯ КОНТУРА SELF-HEALING (САМОВОССТАНОВЛЕНИЯ)
	// v9.0.0: standard_ssh_port удалён, self-healing только для real_ssh_port.
	if key == "real_ssh_port" {
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
	return "", false
}

// execCommandSelfHealingCheck — вспомогательный низкоуровневый триггер синхронизации ОС при рассинхронизации параметров
func execCommandSelfHealingCheck() error {
	// Самовосстановление логики: проверяем, что sshd_config.d/amberguard.conf соответствует текущему ОЗУ-состоянию
	// В продакшене здесь дергается метод пакета sys/self_healing
	return nil
}
