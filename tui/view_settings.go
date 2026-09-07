// tui/view_settings.go
package tui

import (
	// Добавлен для маршалинга структуры в текстовый JSON-формат
	"fmt"
	"os" // Добавлен для нативной работы с дескрипторами и POSIX Atomic Rename

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
	LabelRu     string
	LabelEn     string
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
		{Key: "lang", LabelRu: "Язык интерфейса панели (lang)", LabelEn: "Panel interface language (lang)", Group: "LANG", ValueGetter: func() string { return strings.ToUpper(config.C.Lang) }},

		// --- ГРУППА 2: ПАРАМЕТРЫ ЯДРА ДЕГТЯРНОЙ ЯМЫ SSH ---
		{IsHeader: true, Group: "TARPIT"},
		{Key: "standard_ssh_port", LabelRu: "Стандартный порт SSH (Вводной капкан)", LabelEn: "Standard SSH Port (Deception input)", Group: "TARPIT", ValueGetter: func() string { return fmt.Sprintf("%d", config.C.StandardSshPort) }},
		{Key: "real_ssh_port", LabelRu: "Скрытый порт SSH (Точка рокировки)", LabelEn: "Private SSH Port (Castling target)", Group: "TARPIT", ValueGetter: func() string { return fmt.Sprintf("%d", config.C.RealSshPort) }},
		{Key: "delay_step", LabelRu: "Джиттер: базовый интервал сна (сек)", LabelEn: "Jitter: base delay sleep step (sec)", Group: "TARPIT", ValueGetter: func() string { return fmt.Sprintf("%d", config.C.DelayStep) }},
		{Key: "delay_variance", LabelRu: "Джиттер: вариативность разброса (сек)", LabelEn: "Jitter: delay sleep variance (sec)", Group: "TARPIT", ValueGetter: func() string { return fmt.Sprintf("%d", config.C.DelayVariance) }},

		// --- ГРУППА 3: СИСТЕМНЫЕ ЛИМИТЫ И АНТИДОС-ОБОРОНА ---
		{IsHeader: true, Group: "LIMITS"},
		{Key: "tarpit_max_conn", LabelRu: "Макс. параллельных ботов в ОЗУ ловушки", LabelEn: "Max simultaneous bots in Tarpit RAM", Group: "LIMITS", ValueGetter: func() string { return fmt.Sprintf("%d", config.C.TarpitMaxConn) }},
		{Key: "admin_idle_timeout", LabelRu: "Таймаут простоя администратора (мин)", LabelEn: "Admin idle timeout (min)", Group: "LIMITS", ValueGetter: func() string { return fmt.Sprintf("%d", config.C.AdminIdleTimeout) }},
		{Key: "web_rescue_enabled", LabelRu: "Канал спасения: веб-стук (0=выкл / 1=вкл)", LabelEn: "Rescue channel: web-knock (0=off / 1=on)", Group: "LIMITS", ValueGetter: func() string {
			if config.C.WebRescueEnabled {
				return "1"
			}
			return "0"
		}},
		{Key: "web_rescue_port", LabelRu: "Порт автономного сервера веб-стука", LabelEn: "Autonomous web-knock server port", Group: "LIMITS", ValueGetter: func() string { return fmt.Sprintf("%d", config.C.WebRescuePort) }},
		{Key: "web_rescue_password", LabelRu: "Пароль веб-стука (псевдо-2FA)", LabelEn: "Web-knock password (pseudo-2FA)", Group: "LIMITS", ValueGetter: func() string {
			if config.C.WebRescuePasswordHash != "" {
				return "Пароль установлен"
			}
			return "Не задан"
		}},

		// --- ГРУППА 4: ИНВЕНТАРИЗАЦИЯ И СЕРВИСНЫЕ КОМАНДЫ ---
		{IsHeader: true, Group: "SYSTEM"},
		{Key: "BACK", LabelRu: "Назад в главное меню сервера", LabelEn: "Back to main server subsystem menu", Group: "SYSTEM"},
	}
}

// DrawSettingsGrid производит динамическую транзакционную отрисовку экрана SETTINGS
func DrawSettingsGrid(alertMessage string) []ui.Drawable {
	InitSettingsRegistry()
	isEn := config.C.Lang == "en"

	// 1. Сборка верхней информационной шапки
	header := widgets.NewList()
	header.Title = i18n.GetSettingsStatusText("title_box", isEn)
	header.Rows = []string{"                           " + i18n.GetSettingsStatusText("header_desc", isEn)}
	// ЖЕСТКАЯ ФИКСАЦИЯ КООРДИНАТ ПО ТЗ СИСТЕМНОГО АНАЛИТИКА
	header.SetRect(2, 1, 102, 4)
	header.BorderStyle.Fg = ui.ColorCyan
	header.TextStyle.Fg = ui.ColorYellow
	header.TextStyle.Modifier = ui.ModifierBold

	// 2. Сборка центрального интерактивного блока параметров
	formBox := widgets.NewList()
	formBox.Title = i18n.GetSettingsStatusText("form_title", isEn)
	// ЖЕСТКАЯ ФИКСАЦИЯ КООРДИНАТ ПО ТЗ СИСТЕМНОГО АНАЛИТИКА (ВЫСОТА ДО 23 СТРОКИ)
	formBox.SetRect(2, 4, 102, 23)
	formBox.BorderStyle.Fg = ui.ColorCyan
	formBox.SelectedRowStyle = ui.NewStyle(ui.ColorBlack, ui.ColorGreen, ui.ModifierBold)

	var visibleRows []string
	tuiToRegistryMap := make(map[int]int)
	visibleLineCounter := 0

	for regIdx, item := range SettingsRegistry {
		if item.IsHeader {
			headerText := i18n.GetSettingsGroupHeader(item.Group, isEn)
			visibleRows = append(visibleRows, "", " "+headerText)
			visibleLineCounter += 2
			continue
		}

		label := item.LabelRu
		if isEn {
			label = item.LabelEn
		}

		cursor := "     "
		if regIdx == ActiveRowIndex {
			cursor = " --> "
		}

		var finalRowStr string
		if item.Key == "BACK" {
			finalRowStr = fmt.Sprintf("%-5s%s", cursor, label)
		} else {
			currentVal := item.ValueGetter()
			if item.Key == "lang" {
				finalRowStr = fmt.Sprintf("%-5s%-48s:  [[ %s ]](fg:yellow,mod:bold)", cursor, label, currentVal)
			} else {
				finalRowStr = fmt.Sprintf("%-5s%-48s:  %s", cursor, label, currentVal)
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
	statusBox.SetRect(2, 23, 102, 29)

	if IsEditMode {
		statusBox.Title = i18n.GetSettingsStatusText("editing_mode", isEn)
		currentItem := SettingsRegistry[ActiveRowIndex]
		label := currentItem.LabelRu
		if isEn {
			label = currentItem.LabelEn
		}

		statusBox.Text = fmt.Sprintf("  %s: [ %s ]\n  %s: [ %s_ ](fg:white,mod:bold)",
			i18n.GetSettingsStatusText("enter_new", isEn), label,
			i18n.GetSettingsStatusText("input_prompt", isEn), CurrentInputBuf)
		statusBox.BorderStyle.Fg = ui.ColorGreen
		statusBox.TextStyle.Fg = ui.ColorWhite
		statusBox.WrapText = false
	} else {
		statusBox.Title = i18n.GetSettingsStatusText("status_console_title", isEn)
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
				" %s\n "+
					"%s\n "+
					"Нажмите [ESC](fg:yellow,mod:bold) в любой момент для отмены изменений.",
				i18n.GetSettingsStatusText("hint_nav_1", isEn),
				i18n.GetSettingsStatusText("hint_nav_2", isEn),
			)
			statusBox.BorderStyle.Fg = ui.ColorYellow
			statusBox.TextStyle.Fg = ui.ColorCyan
		}
	}

	return []ui.Drawable{header, formBox, statusBox}
}

// HandleSettingsNavigation обрабатывает транзакционные перемещения и клики по кнопкам
func HandleSettingsNavigation(key string) (bool, string) {
	InitSettingsRegistry()
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
			// v8.2.2 (волна гонки 2.3): in-place мутация устранена —
			// публикация снепшота и переключение i18n теперь внутри
			// SaveSettingValueToDisk (config.Update + i18n.InitLanguage при успехе).
			_ = SaveSettingValueToDisk("lang", newLang)
		}
	case "<Enter>":
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
	isEn := config.C.Lang == "en"

	if key == "SCAN_PERIMETER" || key == "BACK" {
		return ""
	}
	if value == "" {
		return i18n.GetSettingsStatusText("empty_val", isEn)
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
			if isEn {
				return "Password too short! Minimum 8 characters."
			}
			return "Пароль слишком короткий! Минимум 8 символов — это канал спасения."
		}
		if len(value) > 128 {
			if isEn {
				return "Password too long! Technical limit 128 characters."
			}
			return "Пароль слишком длинный! Технический потолок 128 символов."
		}
		// v8.2.0 (1.7): bcrypt с солью внутри хэша вместо SHA-256.
		hash, errB := bcrypt.GenerateFromPassword([]byte(value), bcrypt.DefaultCost)
		if errB != nil {
			if isEn {
				return "Failed to generate password hash."
			}
			return "Сбой генерации хэша пароля."
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
		if valInt <= 0 || valInt > 65535 {
			return i18n.GetSettingsStatusText("out_bounds", isEn)
		}
		// ВЕРСИЯ 6.0.0: ИБ-ПРЕДОХРАНИТЕЛИ И ВАЛИДАЦИЯ ПЕРЕСЕЧЕНИЙ ПОРТОВ
		if key == "standard_ssh_port" || key == "real_ssh_port" {
			// 1. Проверяем дублирование портов в памяти во избежание паники сокетов ядра
			if key == "standard_ssh_port" && valInt == config.C.RealSshPort {
				return "Вводной капкан и скрытый порт OpenSSH не могут занимать один и тот же порт!"
			}
			if key == "real_ssh_port" && valInt == config.C.StandardSshPort {
				return "Скрытый порт OpenSSH не может совпадать с портом вводного капкана!"
			}
			// 2. Блокируем изменение сетевой геометрии, если прямо сейчас развернуты активные рубежи защиты
			if _, errCheck := os.Stat(config.TarpitMarkerPath(22)); errCheck == nil { // РЕБРЕНДИНГ
				return "Запрещено менять порты при активной Дегтярной яме! Сначала остановите капкан."
			}
			if _, errMoved := os.Stat(config.MarkerSshMoved); errMoved == nil { // РЕБРЕНДИНГ
				return "Запрещено менять порты при активном Кастлинге! Выполните обратную рокировку."
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
			}
		})
	} else {
		langLower := strings.ToLower(value)
		if langLower != "ru" && langLower != "en" {
			return i18n.GetSettingsStatusText("lang_support", isEn)
		}
		updateErr = config.Update(config.FileConfig, func(c *config.Config) {
			c.Lang = langLower
		})
		// Язык TUI переключаем только после успешной публикации снепшота
		if updateErr == nil {
			i18n.InitLanguage(langLower)
		}
	}

	// Ошибка Update = конфиг не опубликован и не сохранён: выходим раньше,
	// пост-синхронизации не выполняются (та же семантика, что и в оригинале
	// при ошибке SaveConfig).
	if updateErr != nil {
		return "Ошибка сохранения конфигурации: " + updateErr.Error()
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
