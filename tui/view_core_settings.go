// tui/view_core_settings.go
package tui

import (
	"agl/config"
	"agl/i18n"
	"agl/internal/service"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
)

// Переменные рантайм-состояния для встроенного изменения параметров ядра
var (
	IsCoreEditMode      = false
	CurrentCoreInputBuf = ""
	CoreAlertMessage    = ""
	coreDigitOnlyRegex  = regexp.MustCompile(`^[0-9]+$`)
)

// GlobalCoreMenuRows хранит идентификаторы строк для роутера кликов
var GlobalCoreMenuRows []string

// CoreHeader — синглтон шапки экрана настроек ядра
var CoreHeader *widgets.Paragraph

// DrawCoreSettingsGrid отрисовывает фиксированный экран тюнинга ядра (CORE_SETTINGS)
func DrawCoreSettingsGrid(selectedRow int) ([]ui.Drawable, int) {
	var menuRows []string
	cfg := config.C.SysctlCore
	lang := config.C.Lang
	t := i18n.Languages[lang]

	// Динамически подставляем числовые параметры из глобального ОЗУ-состояния синглтона
	menuRows = append(menuRows, fmt.Sprintf(t.ViewCoreSettingsRowSyncookies, cfg.ArmorNetIpv4TcpSyncookies))
	menuRows = append(menuRows, fmt.Sprintf(t.ViewCoreSettingsRowFinTimeout, cfg.ArmorNetIpv4TcpFinTimeout))
	menuRows = append(menuRows, fmt.Sprintf(t.ViewCoreSettingsRowSynRetries, cfg.ArmorNetIpv4TcpSynRetries))
	menuRows = append(menuRows, fmt.Sprintf(t.ViewCoreSettingsRowSynackRetries, cfg.ArmorNetIpv4TcpSynackRetries))
	menuRows = append(menuRows, fmt.Sprintf(t.ViewCoreSettingsRowSomaxconn, cfg.ArmorNetCoreSomaxconn))
	menuRows = append(menuRows, fmt.Sprintf(t.ViewCoreSettingsRowMaxSynBacklog, cfg.ArmorNetIpv4TcpMaxSynBacklog))
	menuRows = append(menuRows, fmt.Sprintf(t.ViewCoreSettingsRowRpFilter, cfg.ArmorNetIpv4ConfAllRpFilter))

	menuRows = append(menuRows, t.ViewCoreSettingsRowResetToFactory)
	menuRows = append(menuRows, t.ViewCoreSettingsRowBackToArmor)

	GlobalCoreMenuRows = menuRows

	// 1. Фиксированная шапка (Высота 3 строки)
	if CoreHeader == nil {
		CoreHeader = widgets.NewParagraph()
		CoreHeader.SetRect(2, 1, 102, 5)
		CoreHeader.BorderStyle.Fg = ui.ColorCyan
		CoreHeader.TitleStyle.Fg = ui.ColorCyan
		CoreHeader.TitleStyle.Modifier = ui.ModifierBold
	}
	timeStr := time.Now().Format("15:04:05")
	dateStr := time.Now().Format("2006-01-02")

	// Форматируем основной текст шапки
	mainText := fmt.Sprintf(t.CoreHeaderDesc, dateStr, timeStr)

	// Добавляем предупреждение внизу основного текста
	CoreHeader.Text = mainText + "\n" + t.CoreHeaderWarning

	CoreHeader.Title = t.CoreTitleText
	CoreHeader.WrapText = false // Отключает автоперенос

	// 2. Верхний блок: Слайдер-список параметров (Высота 7 строк)
	coreList := widgets.NewList()
	coreList.Title = t.CoreListTitle
	coreList.Rows = menuRows
	coreList.SetRect(2, 5, 102, 13)
	coreList.BorderStyle.Fg = ui.ColorCyan
	coreList.SelectedRow = selectedRow
	coreList.SelectedRowStyle = ui.NewStyle(ui.ColorBlack, ui.ColorGreen, ui.ModifierBold)

	// Вычисляем интерактивный текст справки
	var helpText string
	if selectedRow < 7 {
		doc := t.SysctlDocs[selectedRow]
		var currentVal int
		switch selectedRow {
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
		helpText = i18n.FormatTuiHelpText(doc, currentVal, lang)
	} else if selectedRow == 7 {
		helpText = t.CoreResetWarning
	} else {
		helpText = t.CoreBackText
	}

	// 3. Средний блок: Широкоформатная Справка (Высота 9 строк)
	infoBox := widgets.NewParagraph()
	infoBox.Title = t.CoreInfoBoxTitle
	infoBox.Text = helpText
	infoBox.SetRect(2, 13, 102, 24)
	infoBox.BorderStyle.Fg = ui.ColorYellow
	infoBox.TextStyle.Fg = ui.ColorCyan
	infoBox.WrapText = false // Отключает автоперенос

	// 4. Нижний блок: Консоль навигации и встроенного интерактивного изменения
	sysConsole := widgets.NewParagraph()
	sysConsole.SetRect(2, 24, 102, 30)

	if IsCoreEditMode {
		sysConsole.Title = t.ViewCoreSettingsEditModeTitle

		paramName := config.GetSysctlParamNameByIndex(selectedRow)
		sysConsole.Text = fmt.Sprintf(t.ViewCoreSettingsEditModeTextTpl, paramName, CurrentCoreInputBuf)
		sysConsole.BorderStyle.Fg = ui.ColorGreen // Рамка становится зеленой при вводе
		sysConsole.TextStyle.Fg = ui.ColorWhite
		sysConsole.WrapText = false
	} else {
		sysConsole.Title = t.CoreSysConsoleTitle
		if CoreAlertMessage != "" {
			// Выводим алармы ошибок или сообщения об успешной записи
			if strings.HasPrefix(CoreAlertMessage, "[УСПЕХ]") || strings.HasPrefix(CoreAlertMessage, "[SUCCESS]") {
				sysConsole.Text = "\n " + CoreAlertMessage
				sysConsole.BorderStyle.Fg = ui.ColorGreen
				sysConsole.TextStyle.Fg = ui.ColorWhite
			} else {
				sysConsole.Text = t.ViewCoreSettingsErrWriteRejectPrefix + CoreAlertMessage
				sysConsole.BorderStyle.Fg = ui.ColorRed // Красная подсветка при ошибке границ
				sysConsole.TextStyle.Fg = ui.ColorYellow
			}
		} else {
			sysConsole.Text = t.CoreSysConsoleBody
			sysConsole.BorderStyle.Fg = ui.ColorGreen
			sysConsole.TextStyle.Fg = ui.ColorCyan
		}
	}

	return []ui.Drawable{CoreHeader, coreList, infoBox, sysConsole}, len(coreList.Rows)
}

// SaveCoreSettingToDisk выполняет валидацию ИБ-границ и атомарную перезапись config.json
func SaveCoreSettingToDisk(index int, value string) string {
	value = strings.TrimSpace(value)
	lang := config.C.Lang
	t := i18n.Languages[lang]

	if value == "" {
		return t.ViewCoreSettingsErrEmptyValue
	}
	if !coreDigitOnlyRegex.MatchString(value) {
		return i18n.Languages[lang].SettingsErrDigits
	}

	valInt, _ := strconv.Atoi(value)
	doc := t.SysctlDocs[index]

	// Проверяем жесткие ИБ-границы безопасности для конкретного параметра ядра
	if valInt < doc.MinVal || valInt > doc.MaxVal {
		return fmt.Sprintf(t.ViewCoreSettingsErrOutOfBounds, doc.MinVal, doc.MaxVal)
	}

	// v8.2.2 (волна гонки 2.3): мутация через config.Update —
	// клон снепшота, применение, публикация, атомарная запись на диск.
	// Устраняет in-place мутации опубликованного снепшота и дублирует
	// POSIX-контур (tmp + Sync + Rename) единой точкой saveConfigLocked.
	if err := config.Update(config.FileConfig, func(c *config.Config) {
		switch index {
		case 0:
			c.SysctlCore.ArmorNetIpv4TcpSyncookies = valInt
		case 1:
			c.SysctlCore.ArmorNetIpv4TcpFinTimeout = valInt
		case 2:
			c.SysctlCore.ArmorNetIpv4TcpSynRetries = valInt
		case 3:
			c.SysctlCore.ArmorNetIpv4TcpSynackRetries = valInt
		case 4:
			c.SysctlCore.ArmorNetCoreSomaxconn = valInt
		case 5:
			c.SysctlCore.ArmorNetIpv4TcpMaxSynBacklog = valInt
		case 6:
			c.SysctlCore.ArmorNetIpv4ConfAllRpFilter = valInt
		}
	}); err != nil {
		return t.ViewCoreSettingsErrSaveFailPrefix + err.Error()
	}

	// Если брандмауэр ядра сейчас активен, мгновенно накатываем новые лимиты в рантайм
	if errCheck := service.CheckSysctlProtocol(); errCheck == nil {
		_ = service.ToggleSysctlProtocol("ON")
	}

	return t.ViewCoreSettingsSuccessSaved
}
