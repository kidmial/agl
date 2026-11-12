package tui

import (
	"context"
	"fmt"
	"sort"
	"time"

	"agl/i18n"
	"agl/internal/service"
	"agl/internal/sys"

	ui "github.com/gizak/termui/v3"
)

// AllTrapWithCore — участвует ли CORE-ловушка (порт 22) в пакетных тумблерах.
// CONTRACT: default false — ядро защищено от пакетных операций и управляется
// экраном TARPIT и своей карточкой.
// PURPOSE - Присутствует вероятность необходимости появления опции меню,
// которая будет позволять управлять пакетным отключением и CORE-ловушки.
// AllTrapWithCore = false - Управление всеми ловушками, кроме CORE-ловушки.
// AllTrapWithCore = true - Управление всеми ловушками, включая CORE-ловушку.
var AllTrapWithCore = false

// HandleMultiTarpitBatch — пакетный запуск/остановка пула ловушек.
// direction: "START" | "STOP".
// CONTRACT: пакет = N × действие карточки TRAP_CARD (та же последовательность
// вызовов, включая шлюз UFW). Цели — по декларации state из inventory:
// маркер отстаёт на 3-4 секунды и для планирования непригоден.
// SIDE-EFFECTS: декларации через сервисный слой, окно транзакции блокирует
// вызывающий поток до закрытия канала; вызывается только внутри event-sink.
func HandleMultiTarpitBatch(direction string) {
	inv, err := sys.LoadTarpitInventory(sys.TarpitInventoryPath)
	if err != nil {
		// fail closed: инвентарь не читается — пакет не запускается вслепую
		ui.Clear()
		renderArmorMessage(i18n.T.ViewMultiTarpitBatchHintInventoryError)
		time.Sleep(armorHintHold)
		return
	}

	var trapIDs []string
	for id := range inv.Traps {
		trapIDs = append(trapIDs, id)
	}
	sort.Strings(trapIDs) // стабильный порядок, зеркало списка

	coreSkipped := false
	corePort := 0
	var targets []string
	for _, id := range trapIDs {
		trap := inv.Traps[id]
		if trap.IsCore && !AllTrapWithCore {
			coreSkipped = true
			corePort = trap.Port
			continue
		}
		if direction == "START" && trap.State != sys.StateRunning {
			targets = append(targets, id)
		}
		if direction == "STOP" && trap.State == sys.StateRunning {
			targets = append(targets, id)
		}
	}

	// Пустая операция: явный хинт, транзакция не открывается (правило 360°)
	if len(targets) == 0 {
		msg := i18n.T.ViewMultiTarpitBatchHintAllRunning
		if direction == "STOP" {
			msg = i18n.T.ViewMultiTarpitBatchHintNoneRunning
		}
		if coreSkipped {
			msg += " " + i18n.T.ViewMultiTarpitBatchHintCoreSeparate
		}
		ui.Clear()
		renderArmorMessage(msg)
		time.Sleep(armorHintHold)
		return
	}

	progChan := make(chan string, 10)
	go func() {
		if coreSkipped {
			progChan <- fmt.Sprintf(i18n.T.ViewMultiTarpitBatchProgCoreSkip, corePort)
		}
		for _, id := range targets {
			trap := inv.Traps[id]
			if direction == "START" {
				progChan <- fmt.Sprintf(i18n.T.ViewMultiTarpitBatchProgStart, trap.Port)
				if err := service.ActivateTrap(context.Background(), trap.Port, trap.ConfigConfig); err != nil {
					// Отказ предохранителя по порту: пакет продолжается, отказ видим
					progChan <- fmt.Sprintf(i18n.T.ViewMultiTarpitBatchProgBlocked, trap.Port)
					sys.LogAction("TUI_BATCH", "TRAP_START_BLOCKED", "ALERT",
						fmt.Sprintf("event=tarpit.batch_start_blocked port=%d err=%v", trap.Port, err))
					continue
				}
				_ = service.ToggleMultiTarpitProtocol(trap.Port, "ON")
				sys.LogAction("TUI_BATCH", "TRAP_START", "SUCCESS",
					fmt.Sprintf("event=tarpit.batch_started port=%d", trap.Port))
			} else {
				progChan <- fmt.Sprintf(i18n.T.ViewMultiTarpitBatchProgStop, trap.Port)
				_ = service.DeactivateTrap(context.Background(), trap.Port)
				_ = service.ToggleMultiTarpitProtocol(trap.Port, "OFF")
				sys.LogAction("TUI_BATCH", "TRAP_STOP", "SUCCESS",
					fmt.Sprintf("event=tarpit.batch_stopped port=%d", trap.Port))
			}
		}
		close(progChan)
	}()

	// Регистр заголовка приводит само окно (strings.ToUpper в helpers).
	title := i18n.T.ViewMultiTarpitBatchTitleStart
	if direction == "STOP" {
		title = i18n.T.ViewMultiTarpitBatchTitleStop
	}
	// Разметка и геометрия отчёта — в коде; словари дают чистый текст (доктрина 6.7).
	// Метка успеха переиспользована из броневого отчёта: одно слово — один ключ.
	finalReport := []string{
		"",
		"",
		"  [[" + i18n.T.ViewMultiTarpitBatchLabelPool + "]](fg:yellow,mod:bold): " + i18n.T.ViewMultiTarpitBatchReportDone,
		"  --------------------------------------------------------------------------",
		"  [[" + i18n.T.ViewArmorProgressLabelSuccess + "]](fg:green,mod:bold) " + i18n.T.ViewMultiTarpitBatchReportDeclarations,
		"",
	}
	ShowTransactionWindow(title, i18n.T.ViewMultiTarpitBatchInit, progChan, finalReport)
}
