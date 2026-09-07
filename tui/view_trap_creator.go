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
		" Сетевой порт ловушки    (Port)       │ [ %d ] │ Свободный TCP-порт хоста Linux",
		CurrentTrapDraft.Port))

	menuRows = append(menuRows, fmt.Sprintf(
		" Базовое время удержания (DelayStep)  │ [ %d сек ] │ Время задержки одного шага цикла",
		CurrentTrapDraft.DelayStep))

	// ОБНОВЛЕНО В v8.0.1: Метка изменена с «Jitter» на «DelayVariance»
	menuRows = append(menuRows, fmt.Sprintf(
		" Вариативность разброса  (Variance)   │ [ %d сек ] │ Случайный разброс сна (Delay ± Variance)",
		CurrentTrapDraft.DelayVariance))

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
		" Имитируемый профиль     (SSHVersion) │ [ %s ] │ Нажми ENTER для смены шаблона службы",
		presetName))

	menuRows = append(menuRows, fmt.Sprintf(
		" Лимит сессий в ОЗУ      (MaxConn)    │ [ %d ] │ Защита RAM от перегрузки ботнетом",
		CurrentTrapDraft.MaxConn))

	menuRows = append(menuRows,
		" [ GENERATE AND ACTIVATE ]            │ ЗАПИСАТЬ В JSON И ЗАПУСТИТЬ РУБЕЖ ОБОРОНЫ")
	menuRows = append(menuRows,
		" [ CANCEL ]                           │ Отменить создание ловушки и вернуться назад")

	// -------------------------------------------------------------
	// 1. ФИКСИРОВАННАЯ ШАПКА МАСТЕРА (Высота 3 строки)
	// -------------------------------------------------------------
	header := widgets.NewParagraph()
	header.Title = " [ Мастер Развертывания Нового Рубежа │ TRAP_CREATOR ] "
	timeStr := time.Now().Format("15:04:05")
	dateStr := time.Now().Format("2006-01-02")
	header.Text = fmt.Sprintf(
		"  Текущая дата: [%s](fg:yellow,mod:bold)  │  Системное время: [%s](fg:yellow,mod:bold)\n"+
			"  Конфигурация параметров горутины-воркера оборонительного пула Multi-Tarpit.\n"+
			"  Все ловушки этого мастера имеют флаг is_core = false (мульти-ловушки).",
		dateStr, timeStr)
	header.SetRect(2, 1, 102, 5)
	header.BorderStyle.Fg = ui.ColorCyan
	header.TitleStyle.Fg = ui.ColorYellow
	header.TitleStyle.Modifier = ui.ModifierBold
	header.WrapText = false

	// -------------------------------------------------------------
	// 2. БЛОК СПИСКА ПАРАМЕТРОВ (Высота 9 строк)
	// -------------------------------------------------------------
	paramList := widgets.NewList()
	paramList.Title = " Линейно-интерактивный список изменяемых параметров воркера: "
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
		helpText = " Укажите легитимный, не занятый другими службами порт хоста (1-65535).\n" +
			" Рекомендуется использовать порты популярных уязвимых сервисов (например: 23, 2222, 8022).\n" +
			" Запрещено указывать порты, занятые легитимным ПО (см. экран PORT_MAP).\n" +
			" При активации система автоматически проверит инвентарь периметра на занятость порта."
	case 1:
		helpText = " Задает базовый интервал удержания входящего TCP-соединения бота на одну операцию.\n" +
			" Чем выше значение, тем дольше атакующий скрипт будет находиться в состоянии ожидания,\n" +
			" расходуя ресурсы атакующей стороны. Рекомендуемый диапазон: от 5 до 60 секунд."
	case 2:
		// ОБНОВЛЕНО В v8.0.1: Терминология изменена на `DelayVariance`
		helpText = " Добавляет криптографическую псевдослучайность в интервалы ответов ловушки.\n" +
			" Защищает капкан от обнаружения продвинутыми ИБ-сканерами по таймингам (Timing Attacks).\n" +
			" Алгоритм ядра автоматически вычисляет итоговый сон горутины в пределах:\n" +
			" (DelayStep ± DelayVariance). Значение не может превышать базовый сон DelayStep."
	case 3:
		helpText = fmt.Sprintf(
			" Выбор текстового баннера, который ловушка отдаст боту сразу после Handshake.\n"+
				" Текущая строка отправки в сокет:\n [ %s ](fg:cyan,mod:bold)",
			SSHVersionPresets[CurrentTrapDraft.BannerIndex])
	case 4:
		helpText = " Максимальное количество одновременных горутин-потоков для данного порта ловушки.\n" +
			" Защищает сервер от DDoS-атак, направленных на исчерпание дескрипторов файлов (FD)\n" +
			" и перегрузку ОЗУ. При достижении лимита новые сессии ботов будут ожидать освобождения слота."
	case 5:
		helpText = " [ВНИМАНИЕ]: Запуск генерации запишет декларативный стейт в /etc/amberguard/tarpit_inventory.json.\n" +
			" Демон подхватит декларацию и активирует сокет автоматически в течение 15 секунд.\n" +
			" Маркер состояния будет создан ДЕМОНОМ после реального поднятия слушателя.\n" +
			" Клиент НЕ создаёт и НЕ удаляет маркеры."
	default:
		helpText = " Отмена операции. Все текущие рантайм-изменения параметров черновика в памяти будут стерты,\n" +
			" физическая конфигурация JSON-инвентаря останется в исходном неизмененном виде."
	}

	infoBox := widgets.NewParagraph()
	infoBox.Title = " Контекстная криминалистическая справка параметра "
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
		sysConsole.Title = " [ РЕЖИМ ИЗМЕНЕНИЯ ПАРАМЕТРА ЛОВУШКИ ] "
		var pName string
		switch selectedRow {
		case 0:
			pName = "Port (Номер сетевого порта)"
		case 1:
			pName = "DelayStep (Базовое время сна)"
		case 2:
			pName = "DelayVariance (Разброс вариативности)" // ОБНОВЛЕНО В v8.0.1
		case 4:
			pName = "MaxConn (Лимит соединений)"
		}
		sysConsole.Text = fmt.Sprintf(
			"  Редактируемый узел: %s\n  Введите новое числовое значение: [ %s_ ](fg:white,mod:bold)",
			pName, CurrentTrapInputBuf)
		sysConsole.BorderStyle.Fg = ui.ColorGreen
		sysConsole.TextStyle.Fg = ui.ColorWhite
		sysConsole.WrapText = false
	} else {
		// Режим навигации: отображаем подсказки и алармы
		sysConsole.Title = " Консоль интерактивного изменения и навигации Laboratory "
		if TrapAlertMessage != "" {
			if strings.HasPrefix(TrapAlertMessage, "[УСПЕХ]") {
				sysConsole.Text = "\n " + TrapAlertMessage
				sysConsole.BorderStyle.Fg = ui.ColorGreen
				sysConsole.TextStyle.Fg = ui.ColorWhite
			} else {
				sysConsole.Text = "\n  ⚠️ [ОТКАЗ ВАЛИДАЦИИ](fg:red,mod:bold): " + TrapAlertMessage
				sysConsole.BorderStyle.Fg = ui.ColorRed
				sysConsole.TextStyle.Fg = ui.ColorYellow
			}
		} else {
			sysConsole.Text = "  [Навигация]: Стрелки [Up/Down] — перемещение по опциям. [Enter] — редактировать / переключить.\n" +
				"  [Управление]: Нажмите ENTER на строке параметра для изменения значения.\n" +
				"  [Действие]  : Выберите строку GENERATE для активации или ABORT для отмены операции."
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
		return "Вводимое значение не может быть пустым!"
	}

	// Проверка на числовой формат
	if !trapDigitOnlyRegex.MatchString(value) {
		return "Ошибка валидации типа: Допускаются только целые положительные числа!"
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
			return "Недопустимый сетевой порт! Разрешенный диапазон: от 1 до 65535."
		}
		// ИБ-ПРЕДОХРАНИТЕЛЬ: Запрещаем создавать мульти-ловушку на порту 22.
		// Порт 22 — это базовая ловушка (is_core = true), управляемая отдельно.
		// ОБНОВЛЕНО В v8.0.1.
		if valInt == config.C.StandardSshPort {
			return "Порт 22 зарезервирован для базовой ловушки (is_core). Используйте экран TARPIT → TARPIT_ON."
		}
		// C8 (v8.1.3): канал спасения и скрытый SSH не могут быть капканами.
		if valInt == config.C.WebRescuePort {
			return fmt.Sprintf("Порт %d зарезервирован под канал спасения веб-стука (геном: «Каналы спасения обязательны»). Ловушка на нём заперла бы сейф навсегда.", valInt)
		}
		if _, errM := os.Stat(config.MarkerSshMoved); errM == nil && valInt == config.C.RealSshPort {
			return fmt.Sprintf("Порт %d — текущий скрытый порт SSH (рокировка активна). Ловушка на порту управления запрещена.", valInt)
		}
		CurrentTrapDraft.Port = valInt

	case 1:
		// -------------------------------------------------------------
		// ВАЛИДАЦИЯ БАЗОВОГО СНА (DelayStep)
		// Допустимый диапазон: от 1 до 300 секунд.
		// -------------------------------------------------------------
		if valInt < 1 || valInt > 300 {
			return "Критическое значение задержки! Допускается диапазон от 1 до 300 секунд."
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
				"DelayVariance не может быть отрицательным или превышать базовый сон DelayStep (%d сек)!",
				CurrentTrapDraft.DelayStep)
		}
		CurrentTrapDraft.DelayVariance = valInt // ОБНОВЛЕНО В v8.0.1

	case 4:
		// -------------------------------------------------------------
		// ВАЛИДАЦИЯ ЛИМИТА СЕССИЙ (MaxConn)
		// Допустимый диапазон: от 1 до 1000 горутин.
		// -------------------------------------------------------------
		if valInt < 1 || valInt > 1000 {
			return "Недопустимый лимит сессий! Установите границы от 1 до 1000 горутин."
		}
		CurrentTrapDraft.MaxConn = valInt
	}

	return "" // Валидация пройдена успешно
}
