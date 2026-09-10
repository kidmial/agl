// tui/view_tarpit_controller.go
// ============================================================================
// КОМПОНЕНТ: Доменный контроллер экрана Дегтярной Ямы (TUI)
// ВЕРСИЯ: v8.0.1
// РОЛЬ: Координирует переключение ловушек, рокировку портов и навигацию
//
//	по экрану управления Дегтярной Ямой.
//
// ============================================================================
//
// АРХИТЕКТУРНАЯ ПОЗИЦИЯ:
//
//	Этот файл — TUI-контроллер экрана «Дегтярная Яма».
//	Он обрабатывает клики по пунктам меню и вызывает соответствующие
//	протоколы управления через `service.ExecuteArmorRoute("TARPIT", ...)`.
//
// КЛЮЧЕВОЕ ИЗМЕНЕНИЕ v8.0.1:
//   - Проверка статуса ловушки идёт через `service.SetTarpitProtocol("CHECK")`,
//     который опрашивает `tarpitctl.IsDaemonAlive()` и маркер на диске.
//   - Убрано ручное удаление маркера в `uiCloseAndStopTarpitDaemon`.
//     Маркер удаляет ДЕМОН после реального снятия слушателя.
//   - Клиент НЕ создаёт и НЕ удаляет маркеры. Только читает.
//
// ### [2026-08-22] — Мажорный рефакторинг контроллера (v8.0.1)
// - Проверка статуса ловушки заменена на `service.SetTarpitProtocol("CHECK")`.
// - Убрано ручное удаление маркера `agl_tarpit_listener_port.22`.
// - Обновлены комментарии и добавлена история изменений.
// ============================================================================
package tui

import (
	"fmt"
	"os"
	"time"

	"agl/config"
	"agl/i18n"
	"agl/internal/service"

	ui "github.com/gizak/termui/v3"
)

// ============================================================================
// ГЛАВНЫЙ ДИСПЕТЧЕР КЛИКОВ ЭКРАНА ДЕГТЯРНОЙ ЯМЫ
// ============================================================================

// HandleTarpitMenuClick координирует переключение ловушек и рокировку портов.
// Вызывается из `view_router.go` при нажатии ENTER на экране TARPIT.
//
// МАППИНГ СТРОК МЕНЮ:
//
//	0 — LIVE_BOX:    Просмотр живой таблицы инцидентов
//	1 — SETTINGS:    Глобальные настройки параметров
//	2 — MOVE_PORT:   Рокировка портов (Кастлинг)
//	3 — TARPIT_ON:   Активация ловушки на порту 22
//	4 — TARPIT_OFF:  Деактивация ловушки на порту 22
//	5 — HELP:        Справка по протоколу
//	6 — BACK:        Возврат в главное меню
//
// ВОЗВРАЩАЕТ:
//   - Строка следующего экрана для роутера
//   - Флаг `true` если нужно продолжить без дополнительных действий
//
// ОБНОВЛЕНО В v8.0.1: Проверка статуса ловушки идёт через
// `service.SetTarpitProtocol("CHECK")`, а не через прямое `os.Stat` маркера.
func HandleTarpitMenuClick(currentRow int) (string, bool) {
	switch currentRow {

	// -------------------------------------------------------------
	// СТРОКА 0: LIVE_BOX — Просмотр живой таблицы инцидентов
	// -------------------------------------------------------------
	case 0:
		return "LIVE_BOX", true

	// -------------------------------------------------------------
	// СТРОКА 1: SETTINGS — Глобальные настройки параметров
	// -------------------------------------------------------------
	case 1:
		return "SETTINGS", true

	// -------------------------------------------------------------
	// СТРОКА 2: MOVE_PORT — Рокировка портов (Кастлинг)
	//
	// ИБ-ПРЕДОХРАНИТЕЛЬ: Если Дегтярная Яма активна на порту 22,
	// рокировка блокируется. Нельзя переносить OpenSSH, пока
	// ловушка держит порт 22.
	//
	// ОБНОВЛЕНО В v8.0.1: Проверка статуса через
	// `service.SetTarpitProtocol("CHECK")`, который опрашивает
	// демон и маркер через `tarpitctl`.
	// -------------------------------------------------------------
	case 2:
		// Проверяем, активна ли Дегтярная Яма на порту 22.
		// Если `CHECK` возвращает nil — ловушка активна.
		// ОБНОВЛЕНО В v8.0.1.
		if errCheck := service.CheckTarpitProtocol(); errCheck == nil {
			uiCloseAndPrintTarpitLock()
			return "TARPIT", true
		}
		// Ловушка не активна — запускаем рокировку
		uiCloseAndRunCastling()
		return "TARPIT", false

	// -------------------------------------------------------------
	// СТРОКА 3: TARPIT_ON — Активация ловушки на порту 22
	//
	// ИБ-ПРЕДОХРАНИТЕЛЬ: Если ловушка уже активна, повторная
	// инициализация блокируется для защиты от коллизии.
	//
	// ОБНОВЛЕНО В v8.0.1: Проверка статуса через
	// `service.SetTarpitProtocol("CHECK")`.
	// -------------------------------------------------------------
	case 3:
		// Проверяем, активна ли Дегтярная Яма на порту 22.
		// Если `CHECK` возвращает nil — ловушка уже активна.
		// ОБНОВЛЕНО В v8.0.1.
		if errCheck := service.CheckTarpitProtocol(); errCheck == nil {
			uiCloseAndPrintTarpitActiveWarning()
			return "TARPIT", true
		}
		// Ловушка не активна — запускаем
		uiCloseAndStartTarpitDaemon()
		return "TARPIT", false

	// -------------------------------------------------------------
	// СТРОКА 4: TARPIT_OFF — Деактивация ловушки на порту 22
	// -------------------------------------------------------------
	case 4:
		uiCloseAndStopTarpitDaemon()
		return "TARPIT", false

	// -------------------------------------------------------------
	// СТРОКА 5: HELP — Справка по протоколу
	// -------------------------------------------------------------
	case 5:
		uiCloseAndPrintProtocolInfo()
		return "TARPIT", false

	// -------------------------------------------------------------
	// СТРОКА 6: BACK — Возврат в главное меню
	// -------------------------------------------------------------
	case 6:
		return "MAIN", true
	}

	return "TARPIT", false
}

// ============================================================================
// ВСПОМОГАТЕЛЬНЫЕ МЕТОДЫ: ПЕРЕКЛЮЧЕНИЕ КОНТЕКСТА ТЕРМИНАЛА
// ============================================================================

// uiCloseAndPrintTarpitLock выводит сообщение о блокировке рокировки
// из-за активной Дегтярной Ямы. Закрывает и переинициализирует TUI.
//
// ОБНОВЛЕНО В v8.0.1: Без изменений логики.
func uiCloseAndPrintTarpitLock() {
	service.ResetUfwCacheSnapshot()
	ui.Close()
	fmt.Println(i18n.T.ViewTarpitCtlLockCritical)
	fmt.Println(i18n.T.ViewTarpitCtlLockReason)
	fmt.Println(i18n.T.ViewTarpitCtlPressEnterToReturn)
	var p string
	_, _ = fmt.Scanln(&p)
	_ = ui.Init()
}

// uiCloseAndRunCastling выполняет рокировку портов (Кастлинг) или обратную рокировку.
// Определяет направление по наличию маркера `agl_ssh_moved`.
//
// ОБНОВЛЕНО В v8.0.1: Без изменений логики. Рокировка управляется
// через `service.ExecuteArmorRoute("MOVE_PORT", ...)`.
func uiCloseAndRunCastling() {
	service.ResetUfwCacheSnapshot()
	ui.Close()

	// Персистентный маркер рокировки (кастлинга).
	// Если файл существует → рокировка выполнена → выполняем обратную рокировку.
	// Если файла нет → рокировка не выполнена → выполняем прямую рокировку.
	// ОБНОВЛЕНО В v8.0.1: Путь соответствует регламенту маркеров v6.3.9.
	markerFile := config.MarkerSshMoved

	if _, err := os.Stat(markerFile); err == nil {
		// Маркер существует → рокировка активна → выполняем ОБРАТНУЮ рокировку
		fmt.Println(i18n.T.ViewTarpitCtlReverseCastlingStart)
		_ = service.ToggleArmorRoute("MOVE_PORT", "OFF")
	} else {
		// Маркера нет → рокировка не выполнена → выполняем ПРЯМУЮ рокировку
		fmt.Println(i18n.T.ViewTarpitCtlDirectCastlingStart)
		_ = service.ToggleArmorRoute("MOVE_PORT", "ON")
	}

	fmt.Println(i18n.T.ViewTarpitCtlDemonsRestarted)
	time.Sleep(1 * time.Second)
	_ = ui.Init()
}

// uiCloseAndPrintTarpitActiveWarning выводит предупреждение о том, что
// Дегтярная Яма уже активна. Повторная инициализация заблокирована.
//
// ОБНОВЛЕНО В v8.0.1: Без изменений логики.
func uiCloseAndPrintTarpitActiveWarning() {
	ui.Close()
	fmt.Println(i18n.T.ViewTarpitCtlAlreadyActiveWarning)
	fmt.Println(i18n.T.ViewTarpitCtlAlreadyActiveCollision)
	fmt.Println("\n Нажмите ENTER для возврата в пульт управления...")
	var p string
	_, _ = fmt.Scanln(&p)
	_ = ui.Init()
}

// uiCloseAndStartTarpitDaemon запускает Дегтярную Яму на порту 22.
// Вызывает `service.ExecuteArmorRoute("TARPIT", "ON")`, который:
//  1. Проверяет предохранители (рокировка, порт занят)
//  2. Подготавливает UFW-правила
//  3. Делегирует активацию в `tarpitctl.ActivateCoreTarpit()`
//  4. Демон поднимает слушатель и создаёт маркер
//
// ОБНОВЛЕНО В v8.0.1: Без изменений вызова. Управление делегировано
// в `tarpitctl` через обновлённый `SetTarpitProtocol("ON")`.
func uiCloseAndStartTarpitDaemon() {
	ui.Close()
	fmt.Println(i18n.T.ViewTarpitCtlStartInit)

	// Вызываем протокол активации через роутер брони.
	// `SetTarpitProtocol("ON")` делегирует управление в `tarpitctl`.
	// ОБНОВЛЕНО В v8.0.1.
	if errTarpit := service.ToggleArmorRoute("TARPIT", "ON"); errTarpit != nil {
		fmt.Println(i18n.T.ViewTarpitCtlStartCriticalReject)
		fmt.Println(errTarpit.Error())
		fmt.Println("\n Нажмите ENTER для возврата...")
		var p string
		_, _ = fmt.Scanln(&p)
	} else {
		fmt.Println(i18n.T.ViewTarpitCtlStartSuccess)
		time.Sleep(1 * time.Second)
	}
	_ = ui.Init()
}

// uiCloseAndStopTarpitDaemon деактивирует Дегтярную Яму на порту 22.
// Вызывает `service.ExecuteArmorRoute("TARPIT", "OFF")`, который:
//  1. Делегирует деактивацию в `tarpitctl.DeactivateCoreTarpit()`
//  2. Демон читает инвентарь, снимает слушатель, удаляет маркер
//  3. Если это была последняя ловушка — демон останавливается
//  4. Очищает UFW-правила и восстанавливает периметр
//
// ОБНОВЛЕНО В v8.0.1: УБРАНО ручное удаление маркера.
//
// В новой архитектуре маркер удаляет ДЕМОН после реального снятия слушателя.
// Клиент НЕ создаёт и НЕ удаляет маркеры.
func uiCloseAndStopTarpitDaemon() {
	ui.Close()
	fmt.Println(i18n.T.ViewTarpitCtlStopDismantle)

	// Вызываем протокол деактивации через роутер брони.
	// `SetTarpitProtocol("OFF")` делегирует управление в `tarpitctl`.
	// Демон прочитает инвентарь, снимет слушатель и удалит маркер.
	// ОБНОВЛЕНО В v8.0.1.
	_ = service.ToggleArmorRoute("TARPIT", "OFF")

	// =====================================================================
	// ФИКС ПРОБЛЕМЫ 2 (v8.0.1): УБРАНО ручное удаление маркера.
	//
	// В новой архитектуре:
	//   - Клиент записывает декларацию `state=STOPPED` в инвентарь
	//   - Демон читает инвентарь и снимает слушатель
	//   - Демон удаляет маркер после реального снятия слушателя
	//   - Если это была последняя ловушка — демон останавливается
	//
	// Клиент НЕ создаёт и НЕ удаляет маркеры.
	// =====================================================================

	fmt.Println(i18n.T.ViewTarpitCtlStopSuccess)
	time.Sleep(1 * time.Second)
	_ = ui.Init()
}

// uiCloseAndPrintProtocolInfo выводит интерактивную справку по протоколу
// Дегтярной Ямы. Закрывает и переинициализирует TUI.
//
// ОБНОВЛЕНО В v8.0.1: Без изменений логики.
func uiCloseAndPrintProtocolInfo() {
	ui.Close()
	fmt.Println(i18n.T.ViewTarpitCtlProtocolHeader)
	fmt.Println(i18n.T.ViewTarpitCtlProtocolPressEnter)
	var p string
	_, _ = fmt.Scanln(&p)
	_ = ui.Init()
}
