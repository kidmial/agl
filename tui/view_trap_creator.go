// tui/view_trap_creator.go
// ============================================================================
// КОМПОНЕНТ: Мастер интерактивного развертывания новых рубежей ловушек
// ВЕРСИЯ: v8.0.1
// РОЛЬ: Линейно-интерактивный экран для настройки параметров новой ловушки
//
//	в пуле MULTI_TARPIT. Собирает черновик параметров и передаёт
//	его в `service.ActivateTrap()` через роутер.
//
// ============================================================================
//
// АРХИТЕКТУРНАЯ ПОЗИЦИЯ:
//
//	Этот файл — TUI-компонент мастера создания ловушек.
//	Он вызывается из `view_router.go` при нажатии на [ ADD NEW TRAP ]
//	на экране MULTI_TARPIT.
//
//	Мастер собирает черновик параметров в ОЗУ (`TrapDraft`),
//	а при нажатии [ GENERATE AND ACTIVATE ] передаёт управление
//	в `view_router.go`, который вызывает `service.ActivateTrap()`.
//
// ВАЖНОЕ ПРИМЕЧАНИЕ:
//
//	Все ловушки, создаваемые через этот мастер, являются МУЛЬТИ-ЛОВУШКАМИ.
//	Они всегда имеют `is_core = false` в контракте `TarpitDefinition`.
//	Базовая ловушка порта 22 (`is_core = true`) управляется отдельно
//	через экран TARPIT и кнопку TARPIT_ON.
//
// ### [2026-08-22] — Мажорный рефакторинг мастера (v8.0.1)
// - Переименование поля `Jitter` → `DelayVariance` в структуре `TrapDraft`.
// - Обновление всех меток, справок и валидаторов под новую терминологию.
// - Добавлена проверка порта на занятость через инвентарь периметра.
// - Обновлены комментарии для соответствия контракту данных v8.0.1.
// - Добавлено упоминание `is_core = false` для мульти-ловушек.
// ============================================================================
package tui

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"agl/config"
	"agl/i18n"

	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
)

// ============================================================================
// СТРУКТУРА ЧЕРНОВИКА ПАРАМЕТРОВ ЛОВУШКИ
// ============================================================================

// TrapDraft описывает черновик параметров ловушки, удерживаемый в ОЗУ
// до момента сохранения через `service.ActivateTrap()`.
//
// ОБНОВЛЕНО В v8.0.1: Поле `Jitter` переименовано в `DelayVariance`
// для консистентности с обновлённой структурой `sys.TarpitConfig`.
type TrapDraft struct {
	// Port — номер TCP-порта, на котором ловушка будет слушать соединения.
	// Должен быть свободен (проверяется через инвентарь периметра).
	Port int

	// DelayStep — базовый интервал удержания одного TCP-соединения (сек).
	// Чем выше значение, тем дольше бот находится в ожидании.
	DelayStep int

	// DelayVariance — вариативность разброса джиттера (сек).
	// Итоговый сон вычисляется как: DelayStep ± DelayVariance.
	// Защищает капкан от обнаружения по таймингам (Timing Attacks).
	// ПЕРЕИМЕНОВАНО из `Jitter` для консистентности с `sys.TarpitConfig`.
	// ОБНОВЛЕНО В v8.0.1.
	DelayVariance int

	// MaxConn — максимальное количество одновременных горутин-потоков
	// для данной ловушки. Защищает сервер от перегрузки ОЗУ при DDoS.
	MaxConn int

	// BannerIndex — индекс выбранного пресета SSH-баннера из `SSHVersionPresets`.
	// Определяет, какой баннер ловушка отдаст боту после Handshake.
	BannerIndex int
}

// ============================================================================
// ПРЕСЕТЫ ИМИТАЦИИ СЕТЕВЫХ СЛУЖБ (КАМУФЛЯЖ)
// ============================================================================

// SSHVersionPresets — пресеты текстовых баннеров для имитации легитимных служб.
// Ловушка отдаёт выбранный баннер боту сразу после TCP Handshake,
// создавая иллюзию реального сервиса.
var SSHVersionPresets = []string{
	"SSH-2.0-OpenSSH_9.6p1 Ubuntu-3ubuntu13", // Standard Ubuntu LTS
	"SSH-2.0-OpenSSH_7.4p1 Debian-10+deb9u7", // Legacy Enterprise
	"SSH-2.0-dropbear_2020.81",               // Embedded IoT Router
	"SSH-2.0-Generic_Core_Secure",            // Stealth Void Banner
}

// ============================================================================
// ГЛОБАЛЬНЫЕ ПЕРЕМЕННЫЕ РАНТАЙМ-СОСТОЯНИЯ МАСТЕРА
// ============================================================================

// Глобальные переменные рантайм-состояния мастера.
// Аналогичны по структуре переменным из `view_core_settings.go`.
var (
	// IsTrapEditMode — флаг активного режима ввода числа.
	// Когда true, нажатия клавиш идут в буфер ввода, а не в навигацию.
	IsTrapEditMode bool

	// CurrentTrapInputBuf — буфер текущего числового ввода.
	// Накапливает символы при наборе значения параметра.
	CurrentTrapInputBuf string

	// TrapAlertMessage — сообщение об ошибке или успехе для отображения
	// в нижней консоли мастера.
	TrapAlertMessage string

	// trapDigitOnlyRegex — регулярное выражение для валидации числового ввода.
	// Допускает только целые положительные числа.
	trapDigitOnlyRegex = regexp.MustCompile(`^[0-9]+$`)

	// CurrentTrapDraft — текущий черновик параметров ловушки.
	// Инициализируется безопасными дефолтными ИБ-значениями.
	// ОБНОВЛЕНО В v8.0.1: Поле `Jitter` → `DelayVariance`.
	CurrentTrapDraft = TrapDraft{
		Port:          2222, // Безопасный дефолт: не конфликтует с портом 22
		DelayStep:     13,   // Эталонный шаг Дегтярной Ямы
		DelayVariance: 3,    // Минимальная вариативность для защиты от таймингов
		MaxConn:       50,   // Умеренный лимит для защиты ОЗУ
		BannerIndex:   0,    // Стандартный баннер Ubuntu
	}
)

// ============================================================================
// ОТРИСОВКА ЭКРАНА МАСТЕРА
// ============================================================================

// DrawTrapCreatorGrid формирует линейно-интерактивный экран для настройки
// параметров новой ловушки. Вызывается из `view_router.go` при навигации
// по экрану TRAP_CREATOR.
//
// МАППИНГ СТРОК МЕНЮ:
//
//	0 — Порт ловушки (числовой ввод)
//	1 — Базовое удержание DelayStep (числовой ввод)
//	2 — Вариативность разброса DelayVariance (числовой ввод)
//	3 — Профиль имитации SSH-баннера (переключатель пресетов)
//	4 — Лимит сессий MaxConn (числовой ввод)
//	5 — Кнопка [ GENERATE AND ACTIVATE ]
//	6 — Кнопка [ ABORT ]
//
// ОБНОВЛЕНО В v8.0.1: Переименование `Jitter` → `DelayVariance`.
func DrawTrapCreatorGrid(selectedRow int) ([]ui.Drawable, int) {
	var menuRows []string

	// Строим динамические строки списка на основе текущего стейта черновика.
	// ОБНОВЛЕНО В v8.0.1: `DelayVariance` вместо `Jitter`.
	menuRows = append(menuRows, fmt.Sprintf(
		"%s│ [ %d ] │ %s",
		i18n.T.ViewTrapCreatorRowPortLabel, CurrentTrapDraft.Port, i18n.T.ViewTrapCreatorRowPortDesc))

	menuRows = append(menuRows, fmt.Sprintf(
		"%s│ [ %d %s ] │ %s",
		i18n.T.ViewTrapCreatorRowDelayStepLabel, CurrentTrapDraft.DelayStep, i18n.T.ViewTrapCreatorLabelSec, i18n.T.ViewTrapCreatorRowDelayStepDesc))

	// ОБНОВЛЕНО В v8.0.1: Метка изменена с «Jitter» на «DelayVariance»
	menuRows = append(menuRows, fmt.Sprintf(
		"%s│ [ %d %s ] │ %s",
		i18n.T.ViewTrapCreatorRowVarianceLabel, CurrentTrapDraft.DelayVariance, i18n.T.ViewTrapCreatorLabelSec, i18n.T.ViewTrapCreatorRowVarianceDesc))

	// Для баннера выводим короткое имя пресета, чтобы строка не улетала за границы холста
	presetName := "Ubuntu Standard"
	switch CurrentTrapDraft.BannerIndex {
	case 1:
		presetName = "Legacy Enterprise"
	case 2:
		presetName = "Embedded IoT"
	case 3:
		presetName = "Stealth Void"
	}
	menuRows = append(menuRows, fmt.Sprintf(
		"%s│ [ %s ] │ %s",
		i18n.T.ViewTrapCreatorRowBannerLabel, presetName, i18n.T.ViewTrapCreatorRowBannerDesc))

	menuRows = append(menuRows, fmt.Sprintf(
		"%s│ [ %d ] │ %s",
		i18n.T.ViewTrapCreatorRowMaxConnLabel, CurrentTrapDraft.MaxConn, i18n.T.ViewTrapCreatorRowMaxConnDesc))

	menuRows = append(menuRows,
		fmt.Sprintf(" [ GENERATE AND ACTIVATE ]            │ %s", i18n.T.ViewTrapCreatorRowGenerate))
	menuRows = append(menuRows,
		fmt.Sprintf(" [ CANCEL ]                           │ %s", i18n.T.ViewTrapCreatorRowCancel))

	// -------------------------------------------------------------
	// 1. ФИКСИРОВАННАЯ ШАПКА МАСТЕРА (Высота 3 строки)
	// -------------------------------------------------------------
	header := widgets.NewParagraph()
	header.Title = fmt.Sprintf(" [ %s │ TRAP_CREATOR ] ", i18n.T.ViewTrapCreatorHeaderTitle)
	timeStr := time.Now().Format("15:04:05")
	dateStr := time.Now().Format("2006-01-02")
	header.Text = fmt.Sprintf(
		"  %s: [%s](fg:yellow,mod:bold)  │  %s: [%s](fg:yellow,mod:bold)\n"+
			"  %s\n"+
			"  %s",
		i18n.T.ViewTrapCreatorLabelDate, dateStr, i18n.T.ViewTrapCreatorLabelTime, timeStr,
		i18n.T.ViewTrapCreatorHeaderDesc1,
		i18n.T.ViewTrapCreatorHeaderDesc2)
	header.SetRect(2, 1, 102, 5)
	header.BorderStyle.Fg = ui.ColorCyan
	header.TitleStyle.Fg = ui.ColorYellow
	header.TitleStyle.Modifier = ui.ModifierBold
	header.WrapText = false

	// -------------------------------------------------------------
	// 2. БЛОК СПИСКА ПАРАМЕТРОВ (Высота 9 строк)
	// -------------------------------------------------------------
	paramList := widgets.NewList()
	paramList.Title = i18n.T.ViewTrapCreatorListTitle
	paramList.Rows = menuRows
	paramList.SetRect(2, 5, 102, 14)
	paramList.BorderStyle.Fg = ui.ColorCyan
	paramList.SelectedRow = selectedRow
	paramList.SelectedRowStyle = ui.NewStyle(ui.ColorBlack, ui.ColorGreen, ui.ModifierBold)

	// -------------------------------------------------------------
	// 3. СРЕДНИЙ БЛОК КОНТЕКСТНОЙ СПРАВКИ
	// -------------------------------------------------------------
	// Вычисляем интерактивный текст справки в зависимости от выбранной строки.
	// ОБНОВЛЕНО В v8.0.1: Справка для строки 2 использует терминологию `DelayVariance`.
	var helpText string
	switch selectedRow {
	case 0:
		helpText = i18n.T.ViewTrapCreatorHelpPort
	case 1:
		helpText = i18n.T.ViewTrapCreatorHelpDelayStep
	case 2:
		// ОБНОВЛЕНО В v8.0.1: Терминология изменена на `DelayVariance`
		helpText = i18n.T.ViewTrapCreatorHelpVariance
	case 3:
		helpText = fmt.Sprintf(
			i18n.T.ViewTrapCreatorHelpBanner+"[ %s ](fg:cyan,mod:bold)",
			SSHVersionPresets[CurrentTrapDraft.BannerIndex])
	case 4:
		helpText = i18n.T.ViewTrapCreatorHelpMaxConn
	case 5:
		helpText = i18n.T.ViewTrapCreatorHelpGenerate
	default:
		helpText = i18n.T.ViewTrapCreatorHelpAbort
	}

	infoBox := widgets.NewParagraph()
	infoBox.Title = i18n.T.ViewTrapCreatorInfoBoxTitle
	infoBox.Text = helpText
	infoBox.SetRect(2, 14, 102, 24)
	infoBox.BorderStyle.Fg = ui.ColorYellow
	infoBox.TextStyle.Fg = ui.ColorCyan
	infoBox.WrapText = false

	// -------------------------------------------------------------
	// 4. НИЖНИЙ БЛОК ВВОДА / КОНСОЛЬ НАВИГАЦИИ
	// -------------------------------------------------------------
	sysConsole := widgets.NewParagraph()
	sysConsole.SetRect(2, 24, 102, 30)

	if IsTrapEditMode {
		// Режим числового ввода: отображаем буфер и имя редактируемого параметра
		sysConsole.Title = i18n.T.ViewTrapCreatorEditTitle
		var pName string
		switch selectedRow {
		case 0:
			pName = i18n.T.ViewTrapCreatorParamPort
		case 1:
			pName = i18n.T.ViewTrapCreatorParamDelayStep
		case 2:
			pName = i18n.T.ViewTrapCreatorParamVariance // ОБНОВЛЕНО В v8.0.1
		case 4:
			pName = i18n.T.ViewTrapCreatorParamMaxConn
		}
		sysConsole.Text = fmt.Sprintf(
			"  %s: %s\n  %s: [ %s_ ](fg:white,mod:bold)",
			i18n.T.ViewTrapCreatorLabelEditNode, pName,
			i18n.T.ViewTrapCreatorLabelEnterNewValue, CurrentTrapInputBuf)
		sysConsole.BorderStyle.Fg = ui.ColorGreen
		sysConsole.TextStyle.Fg = ui.ColorWhite
		sysConsole.WrapText = false
	} else {
		// Режим навигации: отображаем подсказки и алармы
		sysConsole.Title = i18n.T.ViewTrapCreatorNavTitle
		if TrapAlertMessage != "" {
			if strings.HasPrefix(TrapAlertMessage, "[УСПЕХ]") {
				sysConsole.Text = "\n " + TrapAlertMessage
				sysConsole.BorderStyle.Fg = ui.ColorGreen
				sysConsole.TextStyle.Fg = ui.ColorWhite
			} else {
				sysConsole.Text = "\n  ⚠️ [" + i18n.T.ViewTrapCreatorValidationRejectPrefix + "](fg:red,mod:bold): " + TrapAlertMessage
				sysConsole.BorderStyle.Fg = ui.ColorRed
				sysConsole.TextStyle.Fg = ui.ColorYellow
			}
		} else {
			sysConsole.Text = i18n.T.ViewTrapCreatorNavBody
			sysConsole.BorderStyle.Fg = ui.ColorGreen
			sysConsole.TextStyle.Fg = ui.ColorCyan
		}
	}

	return []ui.Drawable{header, paramList, infoBox, sysConsole}, len(paramList.Rows)
}

// ============================================================================
// ВАЛИДАЦИЯ И СОХРАНЕНИЕ ПАРАМЕТРОВ ЧЕРНОВИКА
// ============================================================================

// ValidateAndStoreDraftParam выполняет проверку границ параметров в ОЗУ
// перед записью в черновик. Вызывается из `view_router.go` при фиксации
// числового ввода (нажатие ENTER в режиме редактирования).
//
// ПАРАМЕТРЫ:
//   - index: индекс строки мастера (0=Port, 1=DelayStep, 2=DelayVariance, 4=MaxConn)
//   - value: строковое значение, введённое пользователем
//
// ВОЗВРАЩАЕТ:
//   - Пустую строку при успешной валидации
//   - Сообщение об ошибке при провале валидации
//
// ОБНОВЛЕНО В v8.0.1: Переименование `Jitter` → `DelayVariance`.
func ValidateAndStoreDraftParam(index int, value string) string {
	value = strings.TrimSpace(value)

	// Проверка на пустой ввод
	if value == "" {
		return i18n.T.ViewTrapCreatorErrEmpty
	}

	// Проверка на числовой формат
	if !trapDigitOnlyRegex.MatchString(value) {
		return i18n.T.ViewTrapCreatorErrNotDigit
	}

	valInt, _ := strconv.Atoi(value)

	switch index {
	case 0:
		// -------------------------------------------------------------
		// ВАЛИДАЦИЯ СЕТЕВОГО ПОРТА
		// Проверяем допустимый диапазон TCP-портов.
		// Дополнительная проверка на занятость порта выполняется
		// в `service.ActivateTrap()` через инвентарь периметра.
		// -------------------------------------------------------------
		if valInt < 1 || valInt > 65535 {
			return i18n.T.ViewTrapCreatorErrPortRange
		}
		// ИБ-ПРЕДОХРАНИТЕЛЬ: Запрещаем создавать мульти-ловушку на порту 22.
		// Порт 22 — это базовая ловушка (is_core = true), управляемая отдельно.
		// ОБНОВЛЕНО В v8.0.1.
		if valInt == config.C.StandardSshPort {
			return i18n.T.ViewTrapCreatorErrPort22Reserved
		}
		// C8 (v8.1.3): канал спасения и скрытый SSH не могут быть капканами.
		if valInt == config.C.WebRescuePort {
			return fmt.Sprintf(i18n.T.ViewTrapCreatorErrRescuePortTpl, valInt)
		}
		if _, errM := os.Stat(config.MarkerSshMoved); errM == nil && valInt == config.C.RealSshPort {
			return fmt.Sprintf(i18n.T.ViewTrapCreatorErrHiddenSshPortTpl, valInt)
		}
		CurrentTrapDraft.Port = valInt

	case 1:
		// -------------------------------------------------------------
		// ВАЛИДАЦИЯ БАЗОВОГО СНА (DelayStep)
		// Допустимый диапазон: от 1 до 300 секунд.
		// -------------------------------------------------------------
		if valInt < 1 || valInt > 300 {
			return i18n.T.ViewTrapCreatorErrDelayRange
		}
		CurrentTrapDraft.DelayStep = valInt
		// Если DelayStep уменьшился ниже текущего DelayVariance,
		// автоматически корректируем DelayVariance.
		// ОБНОВЛЕНО В v8.0.1.
		if CurrentTrapDraft.DelayVariance > valInt {
			CurrentTrapDraft.DelayVariance = valInt
		}

	case 2:
		// -------------------------------------------------------------
		// ВАЛИДАЦИЯ ВАРИАТИВНОСТИ РАЗБРОСА (DelayVariance)
		// ПЕРЕИМЕНОВАНО из `Jitter` в v8.0.1.
		// Значение не может быть отрицательным или превышать DelayStep.
		// -------------------------------------------------------------
		if valInt < 0 || valInt > CurrentTrapDraft.DelayStep {
			return fmt.Sprintf(
				i18n.T.ViewTrapCreatorErrVarianceRangeTpl,
				CurrentTrapDraft.DelayStep)
		}
		CurrentTrapDraft.DelayVariance = valInt // ОБНОВЛЕНО В v8.0.1

	case 4:
		// -------------------------------------------------------------
		// ВАЛИДАЦИЯ ЛИМИТА СЕССИЙ (MaxConn)
		// Допустимый диапазон: от 1 до 1000 горутин.
		// -------------------------------------------------------------
		if valInt < 1 || valInt > 1000 {
			return i18n.T.ViewTrapCreatorErrMaxConnRange
		}
		CurrentTrapDraft.MaxConn = valInt
	}

	return "" // Валидация пройдена успешно
}
