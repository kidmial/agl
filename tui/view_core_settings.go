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

	// coreAlertIsError — тип аларма. Заменяет хардкод-проверку префикса
	// "[УСПЕХ]"/"[SUCCESS]" (доктрина 6.7: естественного языка в коде нет).
	coreAlertIsError bool
	// coreAlertUntil — момент истечения аларма. Zero = аларма нет.
	coreAlertUntil time.Time
)

// coreAlertTTL — единый срок жизни любого аларма: и успех, и отказ.
// 3 секунды
const coreAlertTTL = 3 * time.Second

// setCoreAlertSuccess показывает успех с TTL.
// PURPOSE: одноразовая информация не должна висеть вечно.
// SIDE-EFFECTS: только пакетное состояние рендера, I/O нет.
func setCoreAlertSuccess(msg string) {
	CoreAlertMessage = msg
	coreAlertIsError = false // только цвет рамки, на срок жизни не влияет
	coreAlertUntil = time.Now().Add(coreAlertTTL)
}

// setCoreAlertError показывает отказ с тем же TTL.
// CONTRACT: тип аларма влияет ТОЛЬКО на цвет (красный/зелёный).
// Sticky-режим убран: админ не должен искать способ закрыть сообщение.
func setCoreAlertError(msg string) {
	CoreAlertMessage = msg
	coreAlertIsError = true
	coreAlertUntil = time.Now().Add(coreAlertTTL)
}

// clearCoreAlert гасит аларм (вход в меню, начало ввода).
func clearCoreAlert() {
	CoreAlertMessage = ""
	coreAlertIsError = false
	coreAlertUntil = time.Time{}
}

// expireCoreAlert — ленивое истечение TTL в момент отрисовки.
// PURPOSE: без отдельной горутины. Гонки с циклом рендера исключены:
// состояние читает и пишет только UI-поток.
func expireCoreAlert() {
	if CoreAlertMessage == "" || coreAlertUntil.IsZero() {
		return
	}
	if time.Now().After(coreAlertUntil) {
		clearCoreAlert()
	}
}

// GlobalCoreMenuRows хранит идентификаторы строк для роутера кликов
var GlobalCoreMenuRows []string

// DrawCoreSettingsGrid отрисовывает фиксированный экран тюнинга ядра (CORE_SETTINGS)
func DrawCoreSettingsGrid(selectedRow int) ([]ui.Drawable, int) {
	// Гасим старый успех ДО формирования консоли навигации.
	expireCoreAlert()

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

	// v9.0.0: единая шапка; предупреждение — единственная строка тела.
	// Дата/время живут на линии титула.
	header := newScreenHeader(fmt.Sprintf("%s - %s", HeaderBrandShort, i18n.T.CoreTitleText), 2, 1, 102, 4)
	header.Text = " " + i18n.T.CoreHeaderWarning

	// 2. Верхний блок: Слайдер-список параметров (Высота 7 строк)
	coreList := widgets.NewList()
	coreList.Title = i18n.T.CoreListTitle
	coreList.Rows = menuRows
	coreList.SetRect(2, 4, 102, 13) // v9.0.0: все 9 строк меню без скролла
	coreList.BorderStyle.Fg = ui.ColorCyan

	// КОСМЕТИКА: полоса выделения до правого края. После SetRect.
	// GlobalCoreMenuRows присвоен выше и указывает на исходный слайс —
	// роутер кликов не увидит хвостовых пробелов.
	padListRowsToWidth(coreList)

	coreList.SelectedRow = selectedRow
	coreList.SelectedRowStyle = ui.NewStyle(ui.ColorBlack, ui.ColorGreen, ui.ModifierBold)

	// Вычисляем интерактивный текст справки
	var helpText string
	if spec, ok := config.SysctlSpecByIndex(selectedRow); ok && selectedRow < len(i18n.T.SysctlDocs) {
		// Текущее значение и эталон берём по тому же индексу, что и спека.
		curVal, _ := cfg.ArmorByIndex(selectedRow)
		helpText = formatCoreHelpText(i18n.T.SysctlDocs[selectedRow], curVal, spec.Factory, spec.Min, spec.Max)
	} else if selectedRow == 7 {
		helpText = i18n.T.CoreResetWarning
	} else {
		helpText = i18n.T.CoreBackText
	}

	// 3. Средний блок: Широкоформатная Справка (Высота 9 строк)
	infoBox := widgets.NewParagraph()
	infoBox.Title = i18n.T.CoreInfoBoxTitle
	infoBox.Text = helpText
	infoBox.SetRect(2, 13, 102, 25)
	infoBox.BorderStyle.Fg = ui.ColorYellow
	infoBox.TextStyle.Fg = ui.ColorCyan
	infoBox.WrapText = false // Отключает автоперенос

	// 4. Нижний блок: Консоль навигации и встроенного интерактивного изменения
	sysConsole := widgets.NewParagraph()
	sysConsole.SetRect(2, 25, 102, 30)

	if IsCoreEditMode {
		sysConsole.Title = i18n.T.ViewCoreSettingsEditModeTitle

		paramName := config.GetSysctlParamNameByIndex(selectedRow)
		sysConsole.Text = fmt.Sprintf("  %s: [%s](fg:green)\n  %s: [ %s_ ](fg:white,mod:bold)", i18n.T.ViewCoreSettingsLabelParam, paramName, i18n.T.ViewCoreSettingsLabelEnterNew, CurrentCoreInputBuf)
		sysConsole.BorderStyle.Fg = ui.ColorGreen // Рамка становится зеленой при вводе
		sysConsole.TextStyle.Fg = ui.ColorWhite
		sysConsole.WrapText = false
	} else {
		sysConsole.Title = i18n.T.CoreSysConsoleTitle
		if CoreAlertMessage != "" {
			// Тип аларма — явный флаг. Префиксы "[УСПЕХ]"/"[SUCCESS]"
			// больше не парсятся: естественный язык живёт только в i18n.
			if !coreAlertIsError {
				sysConsole.Text = "\n " + CoreAlertMessage
				sysConsole.BorderStyle.Fg = ui.ColorGreen
				sysConsole.TextStyle.Fg = ui.ColorWhite
				sysConsole.WrapText = false
			} else {
				sysConsole.Text = "\n  ⚠️ [" + i18n.T.ViewCoreSettingsErrWriteRejectPrefix + "](fg:red,mod:bold): " + CoreAlertMessage
				sysConsole.BorderStyle.Fg = ui.ColorRed // Красная подсветка при ошибке границ
				sysConsole.TextStyle.Fg = ui.ColorYellow
				sysConsole.WrapText = false
			}
		} else {
			sysConsole.Text = i18n.T.CoreSysConsoleBody
			sysConsole.BorderStyle.Fg = ui.ColorGreen
			sysConsole.TextStyle.Fg = ui.ColorCyan
			sysConsole.WrapText = false
		}
	}

	return []ui.Drawable{header, coreList, infoBox, sysConsole}, len(coreList.Rows)
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

	// Границы — из единой спеки. Раньше брались из словаря i18n,
	// что давало второй источник правды и дрейф с валидатором LoadConfig.
	spec, ok := config.SysctlSpecByIndex(index)
	if !ok {
		return i18n.T.ViewCoreSettingsErrEmptyValue
	}
	if valInt < spec.Min || valInt > spec.Max {
		return fmt.Sprintf(i18n.T.ViewCoreSettingsErrOutOfBounds, spec.Min, spec.Max)
	}

	// === МУТАЦИЯ КОНФИГА ===
	// config.Update: клон снепшота → применение → публикация → атомарная запись.
	// Прямая мутация config.C запрещена контрактом (снепшот неизменяем).
	if err := config.Update(config.FileConfig, func(c *config.Config) {
		// Запись по индексу спеки: switch больше не дублируется в трёх местах.
		c.SysctlCore.SetArmorByIndex(index, valInt)
	}); err != nil {
		return i18n.T.ViewCoreSettingsErrSaveFailPrefix + err.Error()
	}

	// === НАКАТ В РАНТАЙМ ===
	// Пробник активности по маркеру, а НЕ сверка значений (CHECK).
	// CHECK возвращал ошибку после любой реальной правки → ON не вызывался
	// → значение не доезжало до ядра + ложный ALERT CORE_MISCONFIGURATION.
	if service.IsSysctlArmorActive() {
		_ = service.ToggleSysctlProtocol("ON")
	}

	// Успех: строка уйдёт в CoreAlertMessage и подсветится зелёным.
	return i18n.T.ViewCoreSettingsSuccessSaved
}

// formatCoreHelpText собирает справку: скелет в коде, текст в i18n (TD-2).
// Числа (эталон/границы) приходят из спеки config — в словарях цифр нет.
func formatCoreHelpText(doc i18n.SysctlHelpDoc, curVal, factoryVal, minVal, maxVal int) string {
	if doc.ParamName == "" {
		return i18n.T.CoreHelpSelectParam
	}
	return fmt.Sprintf(
		" %s"+": "+"[%s](fg:green)\n"+
			// Последний глагол = factoryVal (int из спеки). БЫЛО %s — тип сменился
			// вместе с уходом DefaultVal (string) из словаря.
			" %s [[ %d ]](fg:white,mod:bold) ([%s %d](fg:green))\n"+
			" %s [[ %d ]](fg:yellow,mod:bold) %s [[ %d ]](fg:yellow,mod:bold)\n\n"+
			"[%s](fg:yellow)\n [%s](fg:green)\n"+
			"[%s](fg:yellow)\n [%s](fg:white)\n"+
			"[%s](fg:yellow)\n [%s](fg:white) ",
		i18n.T.ViewCoreSettingsLabelParam, doc.ParamName,
		i18n.T.CoreDocCurVal, curVal,
		// Эталон AGL и границы — из ведущего источника, не из словаря.
		i18n.T.CoreDocFactory, factoryVal,
		i18n.T.CoreDocLimit, minVal, i18n.T.CoreDocLimitTo, maxVal,
		i18n.T.CoreDocDesc, doc.Description,
		i18n.T.CoreDocAttack, doc.OnAttack,
		i18n.T.CoreDocRisks, doc.Risks,
	)
}
