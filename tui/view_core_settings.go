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

// formatCoreHelpText собирает справку параметра ядра: скелет в коде, подписи в i18n (TD-2).
func formatCoreHelpText(doc i18n.SysctlHelpDoc, curVal int) string {
	if doc.ParamName == "" {
		return i18n.T.CoreHelpSelectParam
	}
	return fmt.Sprintf(
		" Параметр: %s \n"+
			" %s %d (%s %s)\n"+
			" %s [ %d ] до [ %d ]\n\n"+
			"%s\n %s\n\n"+
			"%s\n %s\n\n"+
			"%s\n %s ",
		doc.ParamName, i18n.T.CoreDocCurVal, curVal, i18n.T.CoreDocFactory, doc.DefaultVal, i18n.T.CoreDocLimit, doc.MinVal, doc.MaxVal, i18n.T.CoreDocDesc, doc.Description, i18n.T.CoreDocAttack, doc.OnAttack, i18n.T.CoreDocRisks, doc.Risks,
	)
}

// DrawCoreSettingsGrid отрисовывает фиксированный экран тюнинга ядра (CORE_SETTINGS)
func DrawCoreSettingsGrid(selectedRow int) ([]ui.Drawable, int) {
	var menuRows []string
	cfg := config.C.SysctlCore
	// Динамически подставляем числовые параметры из глобального ОЗУ-состояния синглтона.
	// TD-1: геометрия (номера, теги, [ %d ], │) живёт в коде; описание — в i18n.
	menuRows = append(menuRows, fmt.Sprintf(" 1. tcp_syncookies      │ [ %d ]  │ %s", cfg.ArmorNetIpv4TcpSyncookies, i18n.T.ViewCoreSettingsRowSyncookies))
	menuRows = append(menuRows, fmt.Sprintf(" 2. tcp_fin_timeout     │ [ %d ] │ %s", cfg.ArmorNetIpv4TcpFinTimeout, i18n.T.ViewCoreSettingsRowFinTimeout))
	menuRows = append(menuRows, fmt.Sprintf(" 3. tcp_syn_retries     │ [ %d ]  │ %s", cfg.ArmorNetIpv4TcpSynRetries, i18n.T.ViewCoreSettingsRowSynRetries))
	menuRows = append(menuRows, fmt.Sprintf(" 4. tcp_synack_retries  │ [ %d ]  │ %s", cfg.ArmorNetIpv4TcpSynackRetries, i18n.T.ViewCoreSettingsRowSynackRetries))
	menuRows = append(menuRows, fmt.Sprintf(" 5. somaxconn           │ [%d] │ %s", cfg.ArmorNetCoreSomaxconn, i18n.T.ViewCoreSettingsRowSomaxconn))
	menuRows = append(menuRows, fmt.Sprintf(" 6. tcp_max_syn_backlog │ [%d] │ %s", cfg.ArmorNetIpv4TcpMaxSynBacklog, i18n.T.ViewCoreSettingsRowMaxSynBacklog))
	menuRows = append(menuRows, fmt.Sprintf(" 7. rp_filter (All)     │ [ %d ]  │ %s", cfg.ArmorNetIpv4ConfAllRpFilter, i18n.T.ViewCoreSettingsRowRpFilter))
	menuRows = append(menuRows, " RESET_TO_FACTORY       │ "+i18n.T.ViewCoreSettingsRowResetToFactory)
	menuRows = append(menuRows, " BACK_TO_ARMOR          │ "+i18n.T.ViewCoreSettingsRowBackToArmor)
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
	mainText := fmt.Sprintf(i18n.T.CoreHeaderDesc, dateStr, timeStr)

	// Добавляем предупреждение внизу основного текста
	CoreHeader.Text = mainText + "\n" + i18n.T.CoreHeaderWarning

	CoreHeader.Title = i18n.T.CoreTitleText
	CoreHeader.WrapText = false // Отключает автоперенос

	// 2. Верхний блок: Слайдер-список параметров (Высота 7 строк)
	coreList := widgets.NewList()
	coreList.Title = i18n.T.CoreListTitle
	coreList.Rows = menuRows
	coreList.SetRect(2, 5, 102, 13)
	coreList.BorderStyle.Fg = ui.ColorCyan
	coreList.SelectedRow = selectedRow
	coreList.SelectedRowStyle = ui.NewStyle(ui.ColorBlack, ui.ColorGreen, ui.ModifierBold)

	// Вычисляем интерактивный текст справки
	var helpText string
	if selectedRow < 7 {
		doc := i18n.T.SysctlDocs[selectedRow]
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
		helpText = formatCoreHelpText(doc, currentVal)
	} else if selectedRow == 7 {
		helpText = i18n.T.CoreResetWarning
	} else {
		helpText = i18n.T.CoreBackText
	}

	// 3. Средний блок: Широкоформатная Справка (Высота 9 строк)
	infoBox := widgets.NewParagraph()
	infoBox.Title = i18n.T.CoreInfoBoxTitle
	infoBox.Text = helpText
	infoBox.SetRect(2, 13, 102, 24)
	infoBox.BorderStyle.Fg = ui.ColorYellow
	infoBox.TextStyle.Fg = ui.ColorCyan
	infoBox.WrapText = false // Отключает автоперенос

	// 4. Нижний блок: Консоль навигации и встроенного интерактивного изменения
	sysConsole := widgets.NewParagraph()
	sysConsole.SetRect(2, 24, 102, 30)

	if IsCoreEditMode {
		sysConsole.Title = i18n.T.ViewCoreSettingsEditModeTitle

		paramName := config.GetSysctlParamNameByIndex(selectedRow)
		sysConsole.Text = fmt.Sprintf("  %s: %s\n  %s: [ %s_ ](fg:white,mod:bold)", i18n.T.ViewCoreSettingsLabelParam, paramName, i18n.T.ViewCoreSettingsLabelEnterNew, CurrentCoreInputBuf)
		sysConsole.BorderStyle.Fg = ui.ColorGreen // Рамка становится зеленой при вводе
		sysConsole.TextStyle.Fg = ui.ColorWhite
		sysConsole.WrapText = false
	} else {
		sysConsole.Title = i18n.T.CoreSysConsoleTitle
		if CoreAlertMessage != "" {
			// Выводим алармы ошибок или сообщения об успешной записи
			if strings.HasPrefix(CoreAlertMessage, "[УСПЕХ]") || strings.HasPrefix(CoreAlertMessage, "[SUCCESS]") {
				sysConsole.Text = "\n " + CoreAlertMessage
				sysConsole.BorderStyle.Fg = ui.ColorGreen
				sysConsole.TextStyle.Fg = ui.ColorWhite
			} else {
				sysConsole.Text = "\n  ⚠️ [" + i18n.T.ViewCoreSettingsErrWriteRejectPrefix + "](fg:red,mod:bold): " + CoreAlertMessage
				sysConsole.BorderStyle.Fg = ui.ColorRed // Красная подсветка при ошибке границ
				sysConsole.TextStyle.Fg = ui.ColorYellow
			}
		} else {
			sysConsole.Text = i18n.T.CoreSysConsoleBody
			sysConsole.BorderStyle.Fg = ui.ColorGreen
			sysConsole.TextStyle.Fg = ui.ColorCyan
		}
	}

	return []ui.Drawable{CoreHeader, coreList, infoBox, sysConsole}, len(coreList.Rows)
}

// SaveCoreSettingToDisk выполняет валидацию ИБ-границ и атомарную перезапись config.json
func SaveCoreSettingToDisk(index int, value string) string {
	value = strings.TrimSpace(value)

	if value == "" {
		return i18n.T.ViewCoreSettingsErrEmptyValue
	}
	if !coreDigitOnlyRegex.MatchString(value) {
		return i18n.T.SettingsErrDigits
	}

	valInt, _ := strconv.Atoi(value)
	doc := i18n.T.SysctlDocs[index]

	// Проверяем жесткие ИБ-границы безопасности для конкретного параметра ядра
	if valInt < doc.MinVal || valInt > doc.MaxVal {
		return fmt.Sprintf(i18n.T.ViewCoreSettingsErrOutOfBounds, doc.MinVal, doc.MaxVal)
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
		return i18n.T.ViewCoreSettingsErrSaveFailPrefix + err.Error()
	}

	// Если брандмауэр ядра сейчас активен, мгновенно накатываем новые лимиты в рантайм
	if errCheck := service.CheckSysctlProtocol(); errCheck == nil {
		_ = service.ToggleSysctlProtocol("ON")
	}

	return i18n.T.ViewCoreSettingsSuccessSaved
}
