// tui/view_router.go
package tui

import (
	"context"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"agl/i18n"
	"agl/internal/service"
	"agl/internal/sys"

	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
)

// =====================================================================
// ГЕНЕРАТОР ПОДСКАЗОК (HINTS)
// =====================================================================

// getArmorHint осуществляет динамический подхват подсказок для меню Брони.
func getArmorHint(row int) string {
	if row < 0 || row >= len(GlobalArmorMenuRows) {
		return ""
	}
	selectedRowStr := GlobalArmorMenuRows[row]
	switch {
	case strings.Contains(selectedRowStr, "SYSCTL"):
		return i18n.T.ArmorHintSysctl
	case strings.Contains(selectedRowStr, "ARMOR_ON"):
		return i18n.T.ArmorHintOn
	case strings.Contains(selectedRowStr, "ARMOR_OFF"):
		return i18n.T.ArmorHintOff
	case strings.Contains(selectedRowStr, "BACK"):
		return i18n.T.ArmorHintBack
	default:
		// ДИНАМИЧЕСКИЙ РУБЕЖ: стандартная подсказка, показывающая действие при нажатии ENTER
		return i18n.T.ViewArmorHintDynamicPerimeter
	}
}

// getTarpitHint осуществляет строго детерминированный подхват локализованных подсказок для Дегтярной Ямы.
// 0 — LIVE_BOX (Таблица ловушки / Инциденты в реальном времени)
// 1 — SETTINGS (Параметры / Настройки ловушки)
// 2 — MOVE_PORT (Рокировка / Кастлинг портов SSH)
// 3 — TARPIT_ON (Включить Дегтярную Яму на порту 22)
// 4 — TARPIT_OFF (Выключить Дегтярную Яму / Демонтировать капкан)
// 5 — INFO_PROTOCOL (Документация / Спецификация ямы)
// 6 — BACK (Вернуться в Главное меню комплекса)
func getTarpitHint(row int) string {
	switch row {
	case 0:
		return i18n.T.TarpitHintLive
	case 1:
		// ФИКС v6.3.1: Возвращаем уникальную подсказку для экрана конфигурации параметров
		return i18n.T.TarpitHintSettings
	case 2:
		return i18n.T.TarpitHintMove
	case 3:
		return i18n.T.TarpitHintOn
	case 4:
		return i18n.T.TarpitHintOff
	case 5:
		// ФИКС v6.3.1: Возвращаем уникальную подсказку для вывода технической спецификации обмана
		return i18n.T.TarpitHintProtocol
	case 6:
		return i18n.T.TarpitHintBack
	default:
		return " "
	}
}

// getAnalyticsHint осуществляет подхват локализованных подсказок для меню Аналитики.
// v6.4.0: Все сторонние анализаторы переведены в режим консервации.
func getAnalyticsHint(row int) string {
	return i18n.T.ViewRouterAnalyticsStatus
}

// =====================================================================
// СОСТОЯНИЕ ПАНЕЛИ (v8.2.2 п. 4.6)
// =====================================================================

// PanelState инкапсулирует локальное состояние TUI-панели.
// Введено в v8.2.2 (п. 4.6) для декомпозиции монолита StartTarpitControlPanel:
// вместо 4 отдельных локальных переменных — единая структура, которую
// удобно передавать в извлечённые фазы (инициализация/события/тикер).
//
// ПОЛЯ:
//   - currentScreen        — активный экран ("MAIN", "ARMOR", "TARPIT", ...).
//   - currentRow           — индекс выбранной строки в текущем меню.
//   - formAlert            — текст ошибки/успеха для экрана SETTINGS.
//   - settingsParentScreen — экран, из которого открыты настройки (для BACK).
//   - scanInProgress — флаг активного сканирования периметра (v9.0.0).
//   - scanStartedAt — момент показа заглушки (для проверки минимальной выдержки) (v9.0.0).
type PanelState struct {
	currentScreen        string
	currentRow           int
	formAlert            string
	settingsParentScreen string
	scanInProgress       bool
	scanStartedAt        time.Time
}

// =====================================================================
// МОДАЛЬНАЯ ЗАГЛУШКА СКАНИРОВАНИЯ ПЕРИМЕТРА (v9.0.0)
// =====================================================================

// scanLoadingMinVisible — нижняя граница видимости заглушки.
// Тикер идёт раз в секунду, поэтому фактически кадр живёт 500..1500 мс.
const scanLoadingMinVisible = 500 * time.Millisecond

func renderScanLoadingBox() {
	loadingBox := widgets.NewParagraph()
	loadingBox.Text = "\n  [" + i18n.T.ViewRouterScanLoadingText + "](fg:white,mod:bold)\n  " + i18n.T.ViewRouterWaitText
	loadingBox.SetRect(33, 10, 68, 16)
	loadingBox.BorderStyle.Fg = ui.ColorYellow
	loadingBox.WrapText = false
	ui.Render(loadingBox)
}

// renderArmorOperationBox отрисовывает информационный бокс "ОПЕРАЦИЯ ВЫПОЛНЯЕТСЯ".
// Показывается на всё время выполнения операции активации/деактивации брони.
// CONTRACT: заголовок из i18n.ViewRouterArmorOpTitle, вторая строка из
// i18n.ViewRouterWaitText (переиспользование существующего ключа).
func renderArmorOperationBox() {
	opBox := widgets.NewParagraph()
	opBox.Text = "\n  [" + i18n.T.ViewRouterArmorOpTitle + "](fg:white,mod:bold)\n  " + i18n.T.ViewRouterWaitText
	opBox.SetRect(33, 10, 68, 16)
	opBox.BorderStyle.Fg = ui.ColorYellow
	opBox.WrapText = false
	ui.Render(opBox)
}

// armorHintHold — выдержка бокса вердикта предохранителя: секунда на чтение фразы.
// PURPOSE: четыре ветки вердиктов (ALREADY_ON / ALREADY_OFF / 2×INVENTORY_UNREADABLE)
// выражают ОДНО UX-решение; константа фиксирует его в одной точке — правка выдержки
// физически не может «забыть» одну из веток (защита от дрейфа дубликатов).
const armorHintHold = 1000 * time.Millisecond

// renderArmorMessage отрисовывает короткий информационный бокс для вердиктов
// предохранителя (ALREADY_ON, ALREADY_OFF, INVENTORY_UNREADABLE).
// CONTRACT: сообщение из i18n, жирный заголовок, жёлтая рамка ожидания.
// Используется для явной обратной связи при «пустых» операциях.
func renderArmorMessage(msg string) {
	msgBox := widgets.NewParagraph()
	msgBox.Text = "\n  [" + msg + "](fg:white,mod:bold)"
	msgBox.SetRect(33, 10, 68, 15)
	msgBox.BorderStyle.Fg = ui.ColorYellow
	msgBox.WrapText = false
	ui.Render(msgBox)
}

// =====================================================================
// ОСНОВНОЙ ДВИЖОК TUI (VIEW_ROUTER)
// =====================================================================

// StartTarpitControlPanel — запускает и обрабатывает главный цикл интерфейса.
//
// ОБНОВЛЕНО В v8.2.2 (п. 4.6 аудита):
//   - Монолит (~600 строк) декомпозирован на 3 фазы:
//     initializePanel() / handlePanelEvent() / handlePanelTick().
//   - Добавлен renderScreen() для устранения дублирования рендеринга.
//   - Публичный контракт НЕ изменён: клиенты (main.go, tui/*.go)
//     ничего не заметили.
func StartTarpitControlPanel() {
	// ═══════════════════════════════════════════════════════════════
	// ФАЗА 1: ИНИЦИАЛИЗАЦИЯ
	// ═══════════════════════════════════════════════════════════════
	state := initializePanel()
	defer ui.Close()

	// Таймер для обновления динамических данных (tick)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	// Очередь событий TUI
	uiEvents := ui.PollEvents()

	// ═══════════════════════════════════════════════════════════════
	// ФАЗА 2+3: ГЛАВНЫЙ ЦИКЛ (СОБЫТИЯ + ТИКЕР)
	// ═══════════════════════════════════════════════════════════════
	for {
		select {
		case e := <-uiEvents:
			if handlePanelEvent(e, state) {
				return // получен сигнал выхода из панели
			}
		case <-ticker.C:
			handlePanelTick(state)
		}
	}
}

// initializePanel выполняет фазу инициализации TUI-панели:
//   - Инициализирует графический движок termui
//   - Создаёт начальное состояние (MAIN, row=0)
//   - Отрисовывает главное меню
//
// КОНТРАКТ (п. 4.6 аудита — «инициализация»):
//   - При сбое ui.Init() завершает процесс через log.Fatalf.
//   - Возвращает полностью инициализированный PanelState.
//
// ПОБОЧНЫЕ ЭФФЕКТЫ:
//   - Перехватывает управление терминалом (termui).
//   - Очищает экран (ui.Clear).
//   - Рендерит DrawMainMenuGrid.
//
// ОБНОВЛЕНО В v8.2.2 (п. 4.6): Извлечено из StartTarpitControlPanel.
func initializePanel() *PanelState {
	if err := ui.Init(); err != nil {
		log.Fatalf("Не удалось запустить графический движок TUI: %v", err)
	}

	state := &PanelState{
		currentScreen:        "MAIN",
		currentRow:           0,
		formAlert:            "",
		settingsParentScreen: "MAIN",
	}

	ui.Clear()
	menuWidgets, _ := DrawMainMenuGrid(state.currentRow)
	ui.Render(menuWidgets...)

	return state
}

// renderScreen отрисовывает текущий экран из PanelState.
// Устраняет дублирование кода рендеринга в Resize/FocusIn, навигации и тикере.
//
// КОНТРАКТ (п. 4.6 аудита — «рендеринг»):
//   - Читает currentScreen / currentRow / formAlert из state.
//   - Вызывает соответствующий DrawXxxGrid для активного экрана.
//   - Не возвращает ошибок — best-effort отрисовка.
//
// ПОБОЧНЫЕ ЭФФЕКТЫ:
//   - Очищает экран (ui.Clear).
//   - Вызывает ui.Render с виджетами выбранного экрана.
//
// ОБНОВЛЕНО В v8.2.2 (п. 4.6): Извлечено для устранения дублирования.
func renderScreen(state *PanelState) {
	ui.Clear()
	switch state.currentScreen {
	case "MAIN":
		w, _ := DrawMainMenuGrid(state.currentRow)
		ui.Render(w...)
	case "ARMOR":
		w, _ := DrawArmorMenuGrid(state.currentRow, getArmorHint(state.currentRow))
		ui.Render(w...)
	case "TARPIT":
		w, _ := DrawTarpitMenuGrid(state.currentRow, getTarpitHint(state.currentRow))
		ui.Render(w...)
	case "MULTI_TARPIT":
		w, _ := DrawMultiTarpitGrid(state.currentRow)
		ui.Render(w...)
	case "TRAP_CARD":
		w, _ := DrawTrapCardGrid(state.currentRow)
		ui.Render(w...)
	case "TRAP_EDITOR":
		w, _ := DrawTrapEditorGrid(state.currentRow)
		ui.Render(w...)
	case "TRAP_CREATOR":
		w, _ := DrawTrapCreatorGrid(state.currentRow)
		ui.Render(w...)
	case "SETTINGS":
		ui.Render(DrawSettingsGrid(state.formAlert)...)
	case "PORT_MAP":
		w, _, _ := DrawPortMapGrid(state.currentRow)
		ui.Render(w...)
	case "LIVE_BOX":
		ui.Render(DrawTarpitLiveBoxGrid()...)
	case "ARMOR_CORE_SETTINGS":
		w, _ := DrawCoreSettingsGrid(state.currentRow)
		ui.Render(w...)
	}
}

// handlePanelEvent обрабатывает одно событие TUI (клавишу, клик, ресайз).
//
// КОНТРАКТ (п. 4.6 аудита — «цикл обработки событий»):
//   - Возвращает true, если панель должна завершиться (пользователь вышел).
//   - Возвращает false для продолжения цикла.
//   - Модифицирует state in-place (currentScreen / currentRow / formAlert).
//
// ПОБОЧНЫЕ ЭФФЕКТЫ:
//   - Вызывает обработчики контроллеров (HandleArmorMenuClick и др.).
//   - Вызывает сервисный слой (service.ActivateTrap, service.DeleteTrap и др.).
//   - Логирует события через sys.LogAction.
//
// ОБНОВЛЕНО В v8.2.2 (п. 4.6): Извлечено из StartTarpitControlPanel.
func handlePanelEvent(e ui.Event, state *PanelState) (shouldQuit bool) {
	// === БЛОКИРОВКА СОБЫТИЙ ВО ВРЕМЯ ОТЛОЖЕННЫХ ОПЕРАЦИЙ (v9.0.0) ===
	// Пока висит модальная заглушка (скан периметра), игнорируем все клавиши.
	// Это защищает от случайных нажатий, которые могут сбить состояние UI.
	if state.scanInProgress {
		return false
	}

	// --- ОБРАБОТКА ИЗМЕНЕНИЯ РАЗМЕРА И ВОЗВРАТА ФОКУСА ОКНА ---
	if e.ID == "<Resize>" || e.ID == "<FocusIn>" {
		renderScreen(state)
		return false
	}

	// --- ОБРАБОТКА НАСТРОЕК (Settings) ---
	if state.currentScreen == "SETTINGS" {
		state.formAlert = ""
		isBackTriggered, errAlert := HandleSettingsNavigation(e.ID)
		if isBackTriggered {
			state.currentScreen = state.settingsParentScreen
			renderScreen(state)
			return false
		}
		state.formAlert = errAlert
		ui.Clear()
		ui.Render(DrawSettingsGrid(state.formAlert)...)
		return false
	}

	// --- ОБРАБОТКА LIVE BOX (Exit) ---
	if state.currentScreen == "LIVE_BOX" {
		if e.ID == "<Escape>" || e.ID == "q" {
			state.currentScreen = "TARPIT"
			state.currentRow = 0
			renderScreen(state)
			return false
		}
	}

	// --- СПЕЦ-КОМАНДЫ (Кнопки выхода) ---
	switch e.ID {
	case "<C-c>":
		return true // Принудительный выход

	// =====================================================================
	// 1. ОБНОВЛЕННАЯ ЛОГИКА <ESCAPE> (Переход на один уровень вверх)
	// =====================================================================
	case "<Escape>":
		// v9.0.0: ВЫХОД ИЗ МАСТЕРА СОЗДАНИЯ ЛОВУШКИ
		if state.currentScreen == "TRAP_CREATOR" {
			if IsTrapEditMode {
				// В режиме ввода: отменить ввод, очистить буфер, вернуться к навигации
				IsTrapEditMode = false
				CurrentTrapInputBuf = ""
				TrapAlertMessage = ""
			} else {
				// В режиме навигации: возврат в MULTI_TARPIT
				state.currentScreen = "MULTI_TARPIT"
				state.currentRow = 0
			}
			renderScreen(state)
			return false
		} else if state.currentScreen == "TRAP_CARD" {
			if IsTrapEditMode {
				// В режиме ввода: отменить ввод, очистить буфер, вернуться к навигации
				IsTrapEditMode = false
				CurrentTrapInputBuf = ""
				TrapAlertMessage = ""
			} else {
				// В режиме навигации: возврат в MULTI_TARPIT
				state.currentScreen = "MULTI_TARPIT"
				state.currentRow = 0
			}
			renderScreen(state)
			return false
		} else if state.currentScreen == "TRAP_EDITOR" {
			if IsTrapEditMode {
				// В режиме ввода: отменить ввод, очистить буфер, вернуться к навигации
				IsTrapEditMode = false
				CurrentTrapInputBuf = ""
				TrapAlertMessage = ""
			} else {
				// В режиме навигации: возврат в карточку ловушки
				state.currentScreen = "TRAP_CARD"
				state.currentRow = 0
			}
			renderScreen(state)
			return false
		} else if state.currentScreen == "ARMOR_CORE_SETTINGS" {
			if IsCoreEditMode {
				IsCoreEditMode = false
				CurrentCoreInputBuf = ""
			} else {
				state.currentScreen = "ARMOR"
				ActiveScreen = ScreenArmor
				state.currentRow = 0
			}
			renderScreen(state)
			return false
		} else if state.currentScreen == "ARMOR" {
			// Назад в главное меню из раздела "Броня"
			state.currentScreen = "MAIN"
			state.currentRow = 1
			renderScreen(state)
			return false
		} else if state.currentScreen == "TARPIT" {
			// Назад в главное меню из раздела "Дегтярная Яма"
			state.currentScreen = "MAIN"
			state.currentRow = 2 // ФИКС НАВИГАЦИИ: Возвращаем фокус ровно на ту строку, откуда зашли
			renderScreen(state)
			return false
		} else if state.currentScreen == "MULTI_TARPIT" {
			state.currentScreen = "MAIN"
			state.currentRow = 3 // Фокус на пункт меню "MULTI_TARPIT"
			renderScreen(state)
			return false
		} else if state.currentScreen == "PORT_MAP" {
			// Назад в главное меню из карты портов хоста
			state.currentScreen = "MAIN"
			state.currentRow = 4
			renderScreen(state)
			return false
		}

	// =====================================================================
	// ОБРАБОТКА ВВОДА СИМВОЛОВ (Default case)
	// =====================================================================
	default:
		// Режим ввода в настройках ядра (Core Settings)
		if state.currentScreen == "ARMOR_CORE_SETTINGS" && IsCoreEditMode {
			if e.ID == "<Backspace>" || e.ID == "C-8" {
				if len(CurrentCoreInputBuf) > 0 {
					CurrentCoreInputBuf = CurrentCoreInputBuf[:len(CurrentCoreInputBuf)-1]
				}
			} else if len(e.ID) == 1 { // Если нажата обычная одиночная клавиша
				CurrentCoreInputBuf += e.ID
			}
			ui.Clear()
			widgets, _ := DrawCoreSettingsGrid(state.currentRow)
			ui.Render(widgets...)
			return false
		}
		// --- НОВЫЙ БЛОК: Режим ввода в Мастере/Редакторе Ловушек ---
		if IsTrapEditMode {
			// Проверка на Backspace или функциональную клавишу удаления
			if e.ID == "<Backspace>" || e.ID == "C-8" {
				if len(CurrentTrapInputBuf) > 0 {
					CurrentTrapInputBuf = CurrentTrapInputBuf[:len(CurrentTrapInputBuf)-1]
				}
			} else if len(e.ID) == 1 {
				CurrentTrapInputBuf += e.ID
			}
			ui.Clear()
			// НОВОЕ В v8.0.3: Рендер зависит от активного экрана
			if state.currentScreen == "TRAP_EDITOR" {
				edW, _ := DrawTrapEditorGrid(state.currentRow)
				ui.Render(edW...)
			} else {
				widgets, _ := DrawTrapCreatorGrid(state.currentRow)
				ui.Render(widgets...)
			}
			return false // Обязательный выход, чтобы событие не провалилось ниже
		}
		return false

	// =====================================================================
	// НАВИГАЦИЯ (Arrows)
	// =====================================================================
	case "<Down>":
		// Навигация внутри таблицы Live Box
		if state.currentScreen == "LIVE_BOX" {
			if LiveTable != nil {
				if LiveTable.SelectedRow < len(LiveTable.Rows)-1 {
					LiveTable.SelectedRow++
					ui.Render(LiveTable)
				}
			}
			return false
		}
		// Навигация по меню
		state.currentRow++
		if state.currentScreen == "MAIN" {
			if state.currentRow > 6 {
				state.currentRow = 6
			}
			renderScreen(state)
		} else if state.currentScreen == "ARMOR" {
			maxRows := len(GlobalArmorMenuRows) - 1
			if maxRows < 0 {
				maxRows = 0
			}
			if state.currentRow > maxRows {
				state.currentRow = maxRows
			}
			renderScreen(state)
		} else if state.currentScreen == "TARPIT" {
			_, totalRows := DrawTarpitMenuGrid(0, "")
			if state.currentRow >= totalRows-1 {
				state.currentRow = totalRows - 1
			}
			renderScreen(state)
		} else if state.currentScreen == "MULTI_TARPIT" {
			_, totalRows := DrawMultiTarpitGrid(0)
			if state.currentRow >= totalRows-1 {
				state.currentRow = totalRows - 1
			}
			renderScreen(state)
		} else if state.currentScreen == "PORT_MAP" {
			_, totalRows, _ := DrawPortMapGrid(0)
			if state.currentRow >= totalRows-1 {
				state.currentRow = totalRows - 1
			}
			renderScreen(state)
		} else if state.currentScreen == "TRAP_CREATOR" {
			_, totalRows := DrawTrapCreatorGrid(0)
			if state.currentRow >= totalRows-1 {
				state.currentRow = totalRows - 1
			}
			renderScreen(state)
		} else if state.currentScreen == "TRAP_CARD" {
			_, totalRows := DrawTrapCardGrid(0)
			if state.currentRow >= totalRows-1 {
				state.currentRow = totalRows - 1
			}
			renderScreen(state)
		} else if state.currentScreen == "TRAP_EDITOR" {
			_, totalRows := DrawTrapEditorGrid(0)
			if state.currentRow >= totalRows-1 {
				state.currentRow = totalRows - 1
			}
			renderScreen(state)
		} else if state.currentScreen == "ARMOR_CORE_SETTINGS" {
			maxRows := len(GlobalCoreMenuRows) - 1
			if maxRows < 0 {
				maxRows = 0
			}
			if state.currentRow > maxRows {
				state.currentRow = maxRows
			}
			renderScreen(state)
		}
		return false

	case "<Up>":
		// Навигация внутри Live Box
		if state.currentScreen == "LIVE_BOX" {
			if LiveTable != nil {
				if LiveTable.SelectedRow > 0 {
					LiveTable.SelectedRow--
					ui.Render(LiveTable)
				}
			}
			return false
		}
		state.currentRow--
		if state.currentRow < 0 {
			state.currentRow = 0
		}
		renderScreen(state)
		return false

	// =====================================================================
	// ДИСПЕТЧЕРИЗАЦИЯ КЛИКОВ (Enter)
	// =====================================================================
	case "<Enter>":
		return handleEnterClick(state)
	}

	return false
}

// handleEnterClick обрабатывает нажатие ENTER — диспетчеризация кликов.
// Вынесено из handlePanelEvent для снижения когнитивной нагрузки:
// это самый крупный блок событийной логики (~400 строк).
//
// КОНТРАКТ:
//   - Возвращает true, если панель должна завершиться.
//   - Возвращает false для продолжения цикла.
//
// ОБНОВЛЕНО В v8.2.2 (п. 4.6): Вынесено из главного обработчика событий.
func handleEnterClick(state *PanelState) (shouldQuit bool) {
	// --- TRAP_CREATOR ---
	if state.currentScreen == "TRAP_CREATOR" {
		if IsTrapEditMode {
			// Фиксация ввода: выключаем режим инпута и прогоняем через ИБ-валидатор
			IsTrapEditMode = false
			TrapAlertMessage = ValidateAndStoreDraftParam(state.currentRow, CurrentTrapInputBuf)
			CurrentTrapInputBuf = ""
		} else {
			// Режим обычной навигации по строкам мастера
			switch state.currentRow {
			case 0, 1, 2, 4:
				// Активируем режим ввода текста в консоли для числовых параметров
				IsTrapEditMode = true
				CurrentTrapInputBuf = ""
				TrapAlertMessage = ""
			case 3:
				// Циклический переключатель пресетов SSH-баннеров (от 0 до 3)
				CurrentTrapDraft.BannerIndex = (CurrentTrapDraft.BannerIndex + 1) % len(SSHVersionPresets)
				TrapAlertMessage = ""
			case 5:
				// =================================================================
				// КНОПКА: GENERATE_AND_ACTIVATE (v8.0.1 Final)
				// =================================================================
				// 1. Формируем конфиг с ЗАЩИТОЙ ОТ НУЛЕЙ
				dStep := CurrentTrapDraft.DelayStep
				if dStep <= 0 {
					dStep = 13
				}
				dVar := CurrentTrapDraft.DelayVariance
				if dVar < 0 {
					dVar = 3
				}
				mConn := CurrentTrapDraft.MaxConn
				if mConn <= 0 {
					mConn = 50
				}
				newTrapConfig := sys.TarpitConfig{
					SSHVersion:    SSHVersionPresets[CurrentTrapDraft.BannerIndex],
					DelayStep:     dStep,
					DelayVariance: dVar,
					MaxConn:       mConn,
				}
				// v8.0.3 (Аудит №7): предохранитель дублирования порта в инвентаре
				if invDup, errDup := sys.LoadTarpitInventory(sys.TarpitInventoryPath); errDup == nil {
					if _, dup := sys.FindTrapByPort(invDup, CurrentTrapDraft.Port); dup {
						TrapAlertMessage = fmt.Sprintf(i18n.T.ViewRouterTrapPortAlreadyInInventory, CurrentTrapDraft.Port)
						ui.Clear()
						widgets, _ := DrawTrapCreatorGrid(state.currentRow)
						ui.Render(widgets...)
						return false
					}
				}
				// 2. Активируем через контроллер (запись в JSON + запуск демона)
				if err := service.ActivateTrap(context.Background(), CurrentTrapDraft.Port, newTrapConfig); err != nil {
					TrapAlertMessage = i18n.T.ViewRouterErrActivatePrefix + err.Error()
					ui.Clear()
					widgets, _ := DrawTrapCreatorGrid(state.currentRow)
					ui.Render(widgets...)
					return false
				}
				// 3. НОВОЕ В v8.0.1: Открываем шлюз UFW для мульти-ловушки
				_ = service.ToggleMultiTarpitProtocol(CurrentTrapDraft.Port, "ON")
				// 4. Успех: переходим в меню управления ловушками
				TrapAlertMessage = i18n.T.ViewRouterTrapActivatedSuccess
				state.currentScreen = "MULTI_TARPIT"
				state.currentRow = 2
				ui.Clear()
				mWidgets, _ := DrawMultiTarpitGrid(state.currentRow)
				ui.Render(mWidgets...)
				return false
			case 6:
				// ABORT
				CurrentTrapDraft = TrapDraft{Port: 2222, DelayStep: 13, DelayVariance: 3, MaxConn: 50, BannerIndex: 0}
				state.currentScreen = "MULTI_TARPIT"
				state.currentRow = 2
				ui.Clear()
				widgets, _ := DrawMultiTarpitGrid(state.currentRow)
				ui.Render(widgets...)
				return false
			}
		}
		// Перерисовка экрана мастера (если не было return выше)
		ui.Clear()
		widgets, _ := DrawTrapCreatorGrid(state.currentRow)
		ui.Render(widgets...)
		return false
	}

	// --- ARMOR_CORE_SETTINGS ---
	if state.currentScreen == "ARMOR_CORE_SETTINGS" {
		if IsCoreEditMode {
			IsCoreEditMode = false
			// Передаем буфер в функцию валидации и сохранения
			// Тип аларма определяем сравнением с эталоном успеха.
			// Сигнатуру SaveCoreSettingToDisk не меняем: она возвращает строку,
			// а роутер решает, будет сообщение с TTL или sticky.
			alertMsg := SaveCoreSettingToDisk(state.currentRow, CurrentCoreInputBuf)
			if alertMsg == i18n.T.ViewCoreSettingsSuccessSaved {
				setCoreAlertSuccess(alertMsg) // успех: исчезнет через 5 сек
			} else {
				setCoreAlertError(alertMsg) // отказ: живёт до следующего действия
			}
			CurrentCoreInputBuf = ""
		} else {
			if state.currentRow >= 0 && state.currentRow < len(GlobalCoreMenuRows) {
				HandleCoreSettingsClick(state.currentRow, GlobalCoreMenuRows[state.currentRow])
				if ActiveScreen == ScreenArmor {
					state.currentScreen = "ARMOR"
					state.currentRow = 0
				}
			}
		}
		ui.Clear()
		widgets, _ := DrawCoreSettingsGrid(state.currentRow)
		ui.Render(widgets...)
		return false
	}

	// --- MAIN MENU ---
	if state.currentScreen == "MAIN" {
		switch state.currentRow {
		case 0:
			state.currentScreen = "SETTINGS"
			state.settingsParentScreen = "MAIN"
			ui.Clear()
			ui.Render(DrawSettingsGrid("")...)
			return false
		case 1:
			state.currentScreen = "ARMOR"
			state.currentRow = 0
			ui.Clear()
			widgets, _ := DrawArmorMenuGrid(state.currentRow, getArmorHint(state.currentRow))
			ui.Render(widgets...)
		case 2:
			state.currentScreen = "TARPIT"
			state.currentRow = 0
			ui.Clear()
			widgets, _ := DrawTarpitMenuGrid(state.currentRow, getTarpitHint(state.currentRow))
			ui.Render(widgets...)
		case 3:
			// АКТИВАЦИЯ КОНТУРА MULTI_TARPIT v8.0.2
			state.currentScreen = "MULTI_TARPIT"
			state.currentRow = 0
			ui.Clear()
			w, _ := DrawMultiTarpitGrid(state.currentRow)
			ui.Render(w...)
			return false
		case 4:
			// СДВИГ ИНДЕКСА: Теперь PORT_MAP находится на строке 4
			state.currentScreen = "PORT_MAP"
			state.currentRow = 0
			ui.Clear()
			widgets, _, _ := DrawPortMapGrid(state.currentRow)
			ui.Render(widgets...)
			return false
		case 5:
			// СДВИГ ИНДЕКСА: Теперь BACKUP находится на строке 5
			HandleBackupMenuClick()
			state.currentScreen = "MAIN"
			ui.Clear()
			menuWidgets, _ := DrawMainMenuGrid(state.currentRow)
			ui.Render(menuWidgets...)
			return false
		case 6:
			// СДВИГ ИНДЕКСА: Теперь EXIT находится на строке 6
			return true // Выход
		}
	} else if state.currentScreen == "PORT_MAP" {
		_, totalRows, _ := DrawPortMapGrid(0)
		if state.currentRow == totalRows-1 { // Нажатие ENTER на кнопке BACK
			state.currentScreen = "MAIN"
			state.currentRow = 4 // Возвращаем фокус на новую позицию PORT_MAP (индекс 4)
			ui.Clear()
			menuWidgets, _ := DrawMainMenuGrid(state.currentRow)
			ui.Render(menuWidgets...)
			return false
		}
		// Экран PORT_MAP строго пассивный, клики по строкам портов игнорируются
		return false
	} else if state.currentScreen == "TARPIT" {
		// ВЫЗОВ ДОМЕННОГО КОНТРОЛЛЕРА ДЕГТЯРНОЙ ЯМЫ И КАСТЛИНГА
		nextScreen, shouldCont := HandleTarpitMenuClick(state.currentRow)
		if shouldCont {
			state.currentScreen = nextScreen
			ui.Clear()
			switch state.currentScreen {
			case "LIVE_BOX":
				ui.Render(DrawTarpitLiveBoxGrid()...)
			case "SETTINGS":
				ui.Render(DrawSettingsGrid("")...)
			case "MAIN":
				// Фокусируем курсор на строке TARPIT (индекс 2), откуда только что вышли
				state.currentRow = 2
				mainWidgets, _ := DrawMainMenuGrid(state.currentRow)
				ui.Render(mainWidgets...)
			}
			return false
		}
		ui.Clear()
		tarpitWidgets, _ := DrawTarpitMenuGrid(state.currentRow, getTarpitHint(state.currentRow))
		ui.Render(tarpitWidgets...)
	} else if state.currentScreen == "MULTI_TARPIT" {
		// =================================================================
		// ДИСПЕТЧЕР КЛИКОВ ПУЛА ЛОВУШЕК (MULTI_TARPIT) v8.0.1
		// =================================================================
		// 1. Получаем реальное количество строк динамического меню пула
		_, totalRows := DrawMultiTarpitGrid(0)
		// 2. Индексы хвоста v9.0.0: ADD, START_ALL, STOP_ALL, BACK
		addTrapRowIndex := totalRows - 4 // Кнопка [ ADD NEW TRAP ]
		startAllRowIndex := totalRows - 3
		stopAllRowIndex := totalRows - 2
		backRowIndex := totalRows - 1 // Кнопка BACK

		// Действие А: Нажатие ENTER на терминальной кнопке BACK
		if state.currentRow == backRowIndex {
			state.currentScreen = "MAIN"
			state.currentRow = 3 // Возвращаем фокус на пункт меню MULTI_TARPIT главного окна
			ui.Clear()
			menuWidgets, _ := DrawMainMenuGrid(state.currentRow)
			ui.Render(menuWidgets...)
			return false
		}
		// Действие Б: Нажатие ENTER на кнопке [ ADD NEW TRAP ]
		if state.currentRow == addTrapRowIndex {
			state.currentScreen = "TRAP_CREATOR"
			state.currentRow = 0 // Сбрасываем курсор на первый параметр мастера
			IsTrapEditMode = false
			TrapAlertMessage = ""
			ui.Clear()
			widgets, _ := DrawTrapCreatorGrid(state.currentRow)
			ui.Render(widgets...)
			return false
		}
		// Действие Б2: пакетные тумблеры пула (v9.0.0) — внутри event-sink:
		// клавиши во время пакета глотаются, устаревший Enter не дёрнет операцию дважды.
		if state.currentRow == startAllRowIndex || state.currentRow == stopAllRowIndex {
			direction := "START"
			if state.currentRow == stopAllRowIndex {
				direction = "STOP"
			}
			done := make(chan struct{}, 1)
			go func() {
				HandleMultiTarpitBatch(direction)
				done <- struct{}{}
			}()
			evCh := ui.PollEvents()
		batchSink:
			for {
				select {
				case <-done:
					break batchSink
				case <-evCh:
					// проглатываем: модалка без органов управления
				}
			}
			ui.Clear()
			bw, _ := DrawMultiTarpitGrid(state.currentRow)
			ui.Render(bw...)
			return false
		}
		// Действие В: Клик по реальной ловушке в списке (Переключение статуса)
		inv, errInv := sys.LoadTarpitInventory(sys.TarpitInventoryPath)
		if errInv != nil {
			// Если инвентарь недоступен, просто перерисовываем
			ui.Clear()
			widgets, _ := DrawMultiTarpitGrid(state.currentRow)
			ui.Render(widgets...)
			return false
		}
		var sortedTrapIDs []string
		for id := range inv.Traps {
			sortedTrapIDs = append(sortedTrapIDs, id)
		}
		sort.Strings(sortedTrapIDs)
		// Проверяем, находится ли курсор над реальной ловушкой
		if state.currentRow < len(sortedTrapIDs) {
			targetTrapID := sortedTrapIDs[state.currentRow]
			targetTrap := inv.Traps[targetTrapID]
			// НОВОЕ В v8.0.3: ENTER больше не переключает ловушку.
			// Пункт списка стал ПАССИВНЫМ — он только проваливает в карточку.
			SelectedTrapPort = targetTrap.Port
			state.currentScreen = "TRAP_CARD"
			state.currentRow = 0
			ui.Clear()
			cardW, _ := DrawTrapCardGrid(state.currentRow)
			ui.Render(cardW...)
			sys.LogAction("TUI_ROUTER", "TRAP_CARD_OPEN", "INFO", i18n.TrEventf("tui_router.trap_card_open", targetTrapID))
			return false
		}
		// Перерисовываем экран пула ловушек
		ui.Clear()
		widgets, _ := DrawMultiTarpitGrid(state.currentRow)
		ui.Render(widgets...)
		return false
	} else if state.currentScreen == "TRAP_CARD" {
		// =================================================================
		// НОВОЕ В v8.0.3: ДИСПЕТЧЕР КАРТОЧКИ ЛОВУШКИ
		// =================================================================
		inv, errInv := sys.LoadTarpitInventory(sys.TarpitInventoryPath)
		if errInv != nil {
			state.currentScreen = "MULTI_TARPIT"
			state.currentRow = 0
			ui.Clear()
			multiW, _ := DrawMultiTarpitGrid(state.currentRow)
			ui.Render(multiW...)
			return false
		}
		// v8.2.0 (2.2-fix): карточка всегда читает свежую декларацию.
		if invFresh, errInv := sys.LoadTarpitInventory(sys.TarpitInventoryPath); errInv == nil {
			inv = invFresh
		}
		trapID := sys.GenerateTarpitID(SelectedTrapPort)
		trap, trapExists := inv.Traps[trapID]
		if !trapExists {
			state.currentScreen = "MULTI_TARPIT"
			state.currentRow = 0
			ui.Clear()
			multiW, _ := DrawMultiTarpitGrid(state.currentRow)
			ui.Render(multiW...)
			return false
		}
		// Проверяем маркер
		markerExists := false
		if trap.MarkerFilePath != "" {
			if _, errStat := os.Stat(trap.MarkerFilePath); errStat == nil {
				markerExists = true
			}
		}
		isRunning := trap.State == sys.StateRunning
		switch state.currentRow {
		case TrapCardStart:
			if !isRunning {
				// v8.2.0 (2.7-fix): вердикт предохранителя обязателен к исполнению.
				if err := service.ActivateTrap(context.Background(), trap.Port, trap.ConfigConfig); err != nil {
					sys.LogAction("TUI_ROUTER", "TRAP_CARD_START_BLOCKED", "ALERT", i18n.TrEventf("tui_router.trap_card_start_blocked", trapID, err))
				} else {
					_ = service.ToggleMultiTarpitProtocol(trap.Port, "ON")
					sys.LogAction("TUI_ROUTER", "TRAP_CARD_START", "SUCCESS", i18n.TrEventf("tui_router.trap_card_start", trapID))
				}
			}
		case TrapCardStop:
			if isRunning {
				_ = service.DeactivateTrap(context.Background(), trap.Port)
				_ = service.ToggleMultiTarpitProtocol(trap.Port, "OFF")
				sys.LogAction("TUI_ROUTER", "TRAP_CARD_STOP", "SUCCESS", i18n.TrEventf("tui_router.trap_card_stop", trapID))
			}
		case TrapCardEdit:
			if !isRunning {
				if err := LoadTrapIntoEditor(trap.Port); err == nil {
					state.currentScreen = "TRAP_EDITOR"
					state.currentRow = 0
					IsTrapEditMode = false
					TrapAlertMessage = ""
					ui.Clear()
					edW, _ := DrawTrapEditorGrid(state.currentRow)
					ui.Render(edW...)
					return false
				}
			}
		case TrapCardDelete:
			if !isRunning && !markerExists {
				if err := service.DeleteTrap(context.Background(), trap.Port); err == nil {
					sys.LogAction("TUI_ROUTER", "TRAP_CARD_DELETE", "SUCCESS", i18n.TrEventf("tui_router.trap_card_delete", trapID))
					state.currentScreen = "MULTI_TARPIT"
					state.currentRow = 0
					ui.Clear()
					multiW, _ := DrawMultiTarpitGrid(state.currentRow)
					ui.Render(multiW...)
					return false
				}
			}
		case TrapCardBack:
			state.currentScreen = "MULTI_TARPIT"
			state.currentRow = 0
			ui.Clear()
			multiW, _ := DrawMultiTarpitGrid(state.currentRow)
			ui.Render(multiW...)
			return false
		}
		// Перерисовка карточки
		ui.Clear()
		cardW, _ := DrawTrapCardGrid(state.currentRow)
		ui.Render(cardW...)
		return false
	} else if state.currentScreen == "TRAP_EDITOR" {
		// =================================================================
		// НОВОЕ В v8.0.3: ДИСПЕТЧЕР РЕДАКТОРА ЛОВУШКИ
		// =================================================================
		if IsTrapEditMode {
			IsTrapEditMode = false
			TrapAlertMessage = ValidateAndStoreEditParam(state.currentRow, CurrentTrapInputBuf)
			CurrentTrapInputBuf = ""
		} else {
			switch state.currentRow {
			case 0:
				// Порт — только чтение
				TrapAlertMessage = i18n.T.ViewRouterTrapPortReadOnly
			case 1, 2, 4:
				// Числовой ввод
				IsTrapEditMode = true
				CurrentTrapInputBuf = ""
				TrapAlertMessage = ""
			case 3:
				// Переключение баннера
				TrapEditDraft.BannerIndex = (TrapEditDraft.BannerIndex + 1) % len(SSHVersionPresets)
				TrapAlertMessage = ""
			case 5:
				// SAVE_AND_APPLY_CHANGES
				if err := SaveTrapEditToInventory(); err != nil {
					TrapAlertMessage = i18n.T.ViewRouterErrSavePrefix + err.Error()
				} else {
					sys.LogAction("TUI_ROUTER", "TRAP_EDIT_SAVE", "SUCCESS", i18n.TrEventf("tui_router.trap_edit_save", TrapEditDraft.Port))
					state.currentScreen = "TRAP_CARD"
					state.currentRow = 0
					ui.Clear()
					cardW, _ := DrawTrapCardGrid(state.currentRow)
					ui.Render(cardW...)
					return false
				}
			case 6:
				// CANCEL_AND_DISCARD
				state.currentScreen = "TRAP_CARD"
				state.currentRow = 0
				ui.Clear()
				cardW, _ := DrawTrapCardGrid(state.currentRow)
				ui.Render(cardW...)
				return false
			}
		}
		// Перерисовка редактора
		ui.Clear()
		edW, _ := DrawTrapEditorGrid(state.currentRow)
		ui.Render(edW...)
		return false
	} else if state.currentScreen == "ARMOR" {
		// =============================================================
		// ОБРАБОТКА SCAN_PERIMETER (v8.0.2)
		// =============================================================
		if state.currentRow >= 0 && state.currentRow < len(GlobalArmorMenuRows) {
			selectedRowStr := GlobalArmorMenuRows[state.currentRow]
			// Перехватываем клик по SCAN_PERIMETER ДО вызова HandleArmorMenuClick
			if strings.Contains(selectedRowStr, "SCAN_PERIMETER") {
				ui.Clear()
				renderScanLoadingBox()

				state.scanInProgress = true
				state.scanStartedAt = time.Now()
				return false
			}
		}
		// =============================================================
		// ОБРАБОТКА ARMOR_ON / ARMOR_OFF (v9.0.0)
		// =============================================================
		// =============================================================
		// ДОЛГИЕ ОПЕРАЦИИ БРОНИ (v9.0.0): бокс на всё время операции
		// =============================================================
		// Инверсия рубежа (ACTIVE/DISARM/TARPIT) и глобальные ARMOR_ON/OFF
		// применяют правила Netfilter/UFW до 4 секунд. Бокс рисуем ДО
		// синхронного вызова контроллера — он живёт всё время операции.
		if state.currentRow >= 0 && state.currentRow < len(GlobalArmorMenuRows) {
			selectedRowStr := GlobalArmorMenuRows[state.currentRow]
			// Тег строки — первая ячейка до "│". Сравнение ТОЛЬКО точное:
			// Contains дал бы префиксные коллизии SysTag с статическими тегами.
			tag := strings.TrimSpace(strings.SplitN(selectedRowStr, "│", 2)[0])
			switch tag {
			case "SYSCTL", "SCAN_PERIMETER", "CORE_SETTINGS", "BACK":
				// быстрые операции или уже перехваченные — уходим в стандартную обработку
			default:
				// динамический рубеж (PORT N / SysTag) или ARMOR_ON / ARMOR_OFF
				ui.Clear()
				renderArmorOperationBox()
				// v9.0.0: модальная операция — UI-поток становится event-sink.
				// Операция идёт в воркере; главный поток глотает клавиши, нажатые
				// во время модалки (у неё нет элементов управления по дизайну).
				// Без этого устаревший Escape срабатывает на живом экране ARMOR
				// и уносит админа в MAIN вместо возврата на уровень вверх.
				done := make(chan string, 1)
				go func() {
					done <- HandleArmorMenuClick(state.currentRow, selectedRowStr)
				}()
				evCh := ui.PollEvents()
				var hintText string
			sink:
				for {
					select {
					case hint := <-done:
						hintText = hint
						break sink
					case <-evCh:
						// проглатываем: модальное окно не имеет органов управления
					}
				}
				// v9.0.0: дренируем буфер канала событий. ui.PollEvents() возвращает
				// буферизованный канал; события, нажатые в последние миллисекунды
				// операции, накапливаются в буфере. Без дренирования они срабатывают
				// после возврата из handlePanelEvent на живом экране ARMOR.
				// Неблокирующий select с default: читаем пока канал не пуст.
				for {
					select {
					case <-evCh:
						// глотаем остаточные события
					default:
						goto drained
					}
				}
			drained:
				ui.Clear()
				armorWidgets, _ := DrawArmorMenuGrid(state.currentRow, hintText)
				ui.Render(armorWidgets...)
				return false
			}
		}
		// =============================================================
		// СТАНДАРТНАЯ ОБРАБОТКА КЛИКОВ ARMOR (остальные кнопки)
		// =============================================================
		if state.currentRow >= 0 && state.currentRow < len(GlobalArmorMenuRows) {
			selectedRowStr := GlobalArmorMenuRows[state.currentRow]
			hintText := HandleArmorMenuClick(state.currentRow, selectedRowStr)
			// Перехватываем рантайм-переключение в подменю
			if ActiveScreen == ScreenCoreSettings {
				state.currentScreen = "ARMOR_CORE_SETTINGS"
				state.currentRow = 0
				ui.Clear()
				coreWidgets, _ := DrawCoreSettingsGrid(state.currentRow)
				ui.Render(coreWidgets...)
				return false
			}
			if strings.Contains(selectedRowStr, "BACK") {
				state.currentScreen = "MAIN"
				state.currentRow = 1
				ui.Clear()
				mainWidgets, _ := DrawMainMenuGrid(state.currentRow)
				ui.Render(mainWidgets...)
				return false
			}
			ui.Clear()
			armorWidgets, _ := DrawArmorMenuGrid(state.currentRow, hintText)
			ui.Render(armorWidgets...)
		}
	} else if state.currentScreen == "ARMOR_CORE_SETTINGS" {
		if state.currentRow >= 0 && state.currentRow < len(GlobalCoreMenuRows) {
			selectedRowStr := GlobalCoreMenuRows[state.currentRow]
			HandleCoreSettingsClick(state.currentRow, selectedRowStr)
			if ActiveScreen == ScreenArmor {
				state.currentScreen = "ARMOR"
				state.currentRow = 0
				ui.Clear()
				widgets, _ := DrawArmorMenuGrid(state.currentRow, getArmorHint(state.currentRow))
				ui.Render(widgets...)
				return false
			}
			ui.Clear()
			widgets, _ := DrawCoreSettingsGrid(state.currentRow)
			ui.Render(widgets...)
		}
	}

	return false
}

// handlePanelTick обрабатывает события таймера (автообновление динамических данных).
// Вызывается каждую 1 секунду из главного цикла панели.
//
// КОНТРАКТ (п. 4.6 аудита — «цикл обработки событий / тикер»):
//   - Читает текущий экран из state.
//   - Вызывает соответствующие Update-функции (UpdateTarpitMenuCounters и др.).
//   - Перерисовывает экран при необходимости.
//   - Не модифицирует state (только читает).
//
// ПОБОЧНЫЕ ЭФФЕКТЫ:
//   - Вызывает Update-функции, которые могут обновлять глобальные счётчики.
//   - Перерисовывает виджеты termui.
//
// ОБНОВЛЕНО В v8.2.2 (п. 4.6): Извлечено из StartTarpitControlPanel.
func handlePanelTick(state *PanelState) {
	// === ОБРАБОТКА ОТЛОЖЕННОГО СКАНИРОВАНИЯ ПЕРИМЕТРА (v9.0.0) ===
	// Если установлен флаг scanInProgress, значит пользователь кликнул SCAN_PERIMETER,
	// но мы отложили выполнение до следующего тика, чтобы заглушка была видна ≥500 мс.
	if state.scanInProgress {
		if time.Since(state.scanStartedAt) < scanLoadingMinVisible {
			ui.Clear()
			renderScanLoadingBox()
			return
		}

		if err := service.RunPerimeterInventoryScan(context.Background()); err != nil {
			sys.LogAction("ARMOR_ROUTER", "SCAN_FAIL", "ERROR", err.Error())
		} else {
			sys.LogAction("ARMOR_ROUTER", "SCAN_SUCCESS", "SUCCESS", i18n.TrEvent("armor_router.scan_success"))
		}

		state.scanInProgress = false
		state.scanStartedAt = time.Time{}
		renderScreen(state)
		return
	}

	// ФИКС v7.1.5: Изолируем тикер главного меню от сетевых структур ловушек
	if state.currentScreen == "MAIN" {
		renderScreen(state)
		return
	} else if state.currentScreen == "TARPIT" {
		UpdateTarpitMenuCounters()
		widgets, _ := DrawTarpitMenuGrid(state.currentRow, getTarpitHint(state.currentRow))
		ui.Render(widgets...)
		return
	} else if state.currentScreen == "LIVE_BOX" {
		UpdateTarpitLiveBoxData()
		if LiveTable != nil {
			ui.Render(LiveTable)
		}
		return
	} else if state.currentScreen == "SETTINGS" {
		if !IsEditMode {
			ui.Render(DrawSettingsGrid("")...)
		}
		return
	} else if state.currentScreen == "PORT_MAP" {
		// Живое обновление карты портов раз в секунду
		widgets, _, _ := DrawPortMapGrid(state.currentRow)
		ui.Render(widgets...)
		return
	} else if state.currentScreen == "MULTI_TARPIT" {
		widgets, _ := DrawMultiTarpitGrid(state.currentRow)
		ui.Render(widgets...)
		return
	} else if state.currentScreen == "TRAP_CARD" {
		// НОВОЕ В v8.0.3: Карточка обновляется каждую секунду
		// чтобы админ видел актуальный трёхфакторный статус
		widgets, _ := DrawTrapCardGrid(state.currentRow)
		ui.Render(widgets...)
		return
	} else if state.currentScreen == "ARMOR" {
		widgets, _ := DrawArmorMenuGrid(state.currentRow, getArmorHint(state.currentRow))
		ui.Render(widgets...)
		return
	} else if state.currentScreen == "ARMOR_CORE_SETTINGS" {
		widgets, _ := DrawCoreSettingsGrid(state.currentRow)
		ui.Render(widgets...)
		return
	} else if state.currentScreen == "TRAP_CREATOR" {
		widgets, _ := DrawTrapCreatorGrid(state.currentRow)
		ui.Render(widgets...)
		return
	} else if state.currentScreen == "TRAP_EDITOR" {
		widgets, _ := DrawTrapEditorGrid(state.currentRow)
		ui.Render(widgets...)
		return
	}
}

// uiInitHelper — вспомогательная функция для инициализации UI.
func uiInitHelper() error {
	return ui.Init()
}
