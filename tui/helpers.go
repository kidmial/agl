// /tui/helpers.go
package tui

import (
	"agl/i18n"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
)

// ============================================================================
// ОБЩИЕ ХЕЛПЕРЫ ДЛЯ TUI-МОДУЛЕЙ
// ============================================================================

// uiTagRe матчит markup-теги termui вида "[текст](fg:...,mod:...)".
// Ленивая группа берёт внутренний текст вместе с экранированными скобками
// (например "[ ACTIVE ]"), отбрасывая невидимый модификатор.
var uiTagRe = regexp.MustCompile(`\[(.*?)\]\([^)]*\)`)

// headerTitleBudget — титульная область шапки в рунах (эмпирика главного экрана):
// дефисы рамки дорисовывают хвост сами, дата встаёт вплотную к правому краю.
const headerTitleBudget = 96

// HeaderBrand — имя продукта для линии титула.
// CONTRACT: версия сборки живёт только на главном экране (экран-паспорт);
// суб-экраны несут бренд + короткий локализованный титул.
const HeaderBrand = "Amber Guard Laboratory"

// HeaderBrandShort — бренд суб-экранов.
const HeaderBrandShort = "AGL"

// screenTitleLine собирает линию титула: левый текст + добивка линиями + дата/время.
// Единый источник для шапок-Paragraph и титулов одиночных блоков (List).
func screenTitleLine(titleLeft string) string {
	now := time.Now()
	right := fmt.Sprintf(" %s: %s | %s: %s ",
		i18n.T.ViewMainLabelDate, now.Format("2006-01-02"),
		i18n.T.ViewMainLabelTime, now.Format("15:04:05"))
	// Длинный титул языка режем по рунам с многоточием: рамка не ломается никогда.
	left := trunc(" "+titleLeft+" ", headerTitleBudget-utf8.RuneCountInString(right)-1)
	pad := headerTitleBudget - utf8.RuneCountInString(left) - utf8.RuneCountInString(right)
	if pad < 1 {
		pad = 1
	}
	return left + strings.Repeat("─", pad) + right
}

// newScreenHeader собирает шапку экрана в едином виде v9.0.0:
// титул = левый текст + добивка линиями + дата/время справа; cyan, без жёлтого.
// CONTRACT: дата/время живут только на линии титула; тело шапки их не дублирует.
// Ключи даты/времени переиспользованы с главного экрана (один источник).
func newScreenHeader(titleLeft string, x0, y0, x1, y1 int) *widgets.Paragraph {
	h := widgets.NewParagraph()
	h.Title = screenTitleLine(titleLeft)
	h.SetRect(x0, y0, x1, y1)
	h.BorderStyle.Fg = ui.ColorCyan
	h.TitleStyle.Fg = ui.ColorCyan
	h.TitleStyle.Modifier = ui.ModifierBold
	h.WrapText = false
	return h
}

// visibleRunes возвращает длину строки ТАК, как её видит терминал:
// markup-модификаторы termui не отображаются и не занимают ячеек экрана.
// PURPOSE: корректное дополнение строк списка пробелами для полосы выделения.
// Используется во всех TUI-модулях с widgets.List (view_armor, view_multi_tarpit, etc).
func visibleRunes(s string) int {
	stripped := uiTagRe.ReplaceAllString(s, "$1")
	return utf8.RuneCountInString(stripped)
}

// padVisible дополняет строку пробелами до заданной ВИДИМОЙ ширины.
// PURPOSE: построение ровных колонок для строк, содержащих markup-теги termui:
// fmt.Sprintf("%-Ns") считает сырую длину с тегами и рвёт колонки.
// CONTRACT: если строка шире width — возвращается без изменений (без обрезки).
func padVisible(s string, width int) string {
	if n := visibleRunes(s); n < width {
		return s + strings.Repeat(" ", width-n)
	}
	return s
}

// padListRowsToWidth добивает строки виджета пробелами до внутренней ширины блока.
// PURPOSE: единая точка геометрии для косметики «полоса выделения до края»:
// termui красит SelectedRowStyle посимвольно, поэтому без добивки полоса
// обрывается на последнем символе текста вместо полосы до рамки.
// CONTRACT:
//   - вызывать СТРОГО ПОСЛЕ SetRect: до него GetRect().Dx()==0 → guard выйдет;
//   - идемпотентна: повторная добивка уже добитой строки — no-op
//     (visibleRunes == width), поэтому перерисовки по тику не копят пробелы;
//   - подменяет l.Rows НОВЫМ слайсом: исходный не мутируется, значит роутер
//     кликов, держащий ссылку на прежние строки, видит чистый текст.
//
// SIDE-EFFECTS: только поле Rows виджета.
func padListRowsToWidth(l *widgets.List) {
	// Минус две вертикальные рамки = Inner.Dx() виджета.
	innerWidth := l.GetRect().Dx() - 2
	if innerWidth <= 0 {
		return // геометрия ещё не задана — паддинг невозможен
	}
	padded := make([]string, len(l.Rows))
	for i, row := range l.Rows {
		// padVisible считает ВИДИМУЮ длину: markup-теги не занимают клеток.
		padded[i] = padVisible(row, innerWidth)
	}
	l.Rows = padded
}

// ShowTransactionWindow — единая механика окна транзакции для всех пакетных
// операций (броня, пул ловушек). Контент (init-строка и финальный отчёт)
// передаётся вызывающим доменом; механика живёт только здесь.
// CONTRACT: окно закрывается само по закрытию progressChan, без клавиш.
func ShowTransactionWindow(actionName, initLine string, progressChan chan string, finalReport []string) {
	ui.Clear()
	var operationHistory []string
	const maxVisibleHistoryLines = 7
	progressBox := widgets.NewParagraph()
	progressBox.Title = fmt.Sprintf(" [ %s: %s ] ", i18n.T.ViewArmorProgressTransactionLabel, strings.ToUpper(actionName))
	progressBox.SetRect(12, 9, 92, 20)
	progressBox.BorderStyle.Fg = ui.ColorRed
	progressBox.TextStyle.Fg = ui.ColorWhite
	progressBox.WrapText = false
	progressBox.Text = "\n  " + initLine
	ui.Render(progressBox)

	for stepLog := range progressChan {
		const maxLogRunes = 64
		if utf8.RuneCountInString(stepLog) > maxLogRunes {
			runes := []rune(stepLog)
			if len(runes) > maxLogRunes {
				stepLog = string(runes[:maxLogRunes-3]) + "..."
			}
		}
		timeTag := time.Now().Format("15:04:05")
		operationHistory = append(operationHistory, fmt.Sprintf(" [%s] %s", timeTag, stepLog))
		if len(operationHistory) > maxVisibleHistoryLines {
			operationHistory = operationHistory[len(operationHistory)-maxVisibleHistoryLines:]
		}
		progressBox.Text = "\n" + strings.Join(operationHistory, "\n")
		ui.Render(progressBox)
	}

	// Финальный отчёт: анимация появления строк, затем автозакрытие
	operationHistory = nil
	progressBox.BorderStyle.Fg = ui.ColorGreen
	for _, line := range finalReport {
		operationHistory = append(operationHistory, line)
		progressBox.Text = "\n" + strings.Join(operationHistory, "\n")
		ui.Render(progressBox)
		time.Sleep(150 * time.Millisecond)
	}
	time.Sleep(1000 * time.Millisecond)
}

// padTagCell собирает тег "[слово]" и приводит его к фиксированной видимой
// ширине width рун: короче — добивка пробелами справа, длиннее — многоточие.
// PURPOSE: колонка описаний меню стоит на фикс-позиции при любой длине тега
// и любом языке; скобки — геометрия в коде, слово — из словаря i18n.
// CONTRACT: width подбирается под самый длинный тег словарей экрана.
func padTagCell(word string, width int) string {
	tag := "[" + word + "]"
	runes := []rune(tag)
	switch {
	case len(runes) < width:
		tag += strings.Repeat(" ", width-len(runes))
	case len(runes) > width:
		tag = string(runes[:width-1]) + "…"
	}
	return tag
}

// SETTNGS
// groupSeparatorRow собирает строку-заголовок группы меню: горизонтальная
// линия с врезанным титулом и добивкой до ширины блока.
// CONTRACT: словарь отдаёт только переводимый титул; вся геометрия (линии,
// врезы, добивка, цвет) живёт здесь (доктрина 6.7). Ширина передаётся
// вызывающим: геометрия блока известна только в момент отрисовки.
func groupSeparatorRow(title string, width int) string {
	const left = "───┤ "
	const right = " ├"
	const minTail = 3 // минимум линий справа: титул не упирается в край
	maxTitle := width - visibleRunes(left) - visibleRunes(right) - minTail
	if maxTitle < 1 {
		maxTitle = 1
	}
	// Титул режем по рунам с многоточием, если язык дал слишком длинную фразу
	line := left + trunc(strings.TrimSpace(title), maxTitle) + right
	if pad := width - visibleRunes(line); pad > 0 {
		line += strings.Repeat("─", pad)
	}
	return "[" + line + "](fg:cyan)"
}

// Alert for tui/view_trap_creator.go tui/view_trap_editor.go
// TrapAlertMessage — inbox ожидающего алерта карточек ловушек (пишет роутер).
var TrapAlertMessage string
var TrapAlertIsError bool // v9.0.0: явный признак ошибки алерта (не префикс строки)

// trapAlertMemory — TTL-память алерта (3 сек), зеркало SETTINGS.
var (
	trapAlertTime        time.Time
	lastTrapAlert        string
	lastTrapAlertIsError bool
)

// setTrapAlert наполняет inbox алерта с явным признаком ошибки.
// Пустое сообщение гасит алерт целиком (успех валидации = тишина).
func setTrapAlert(msg string, isError bool) {
	if msg == "" {
		dismissTrapAlert()
		return
	}
	TrapAlertMessage = msg
	TrapAlertIsError = isError
}

// effectiveTrapAlertMessage защёлкивает inbox в память, гасит inbox и возвращает
// алерт, пока жив его TTL; иначе пусто. Единая точка для creator/editor.
func effectiveTrapAlertMessage() (string, bool) {
	if TrapAlertMessage != "" {
		lastTrapAlert = TrapAlertMessage
		lastTrapAlertIsError = TrapAlertIsError
		trapAlertTime = time.Now()
		TrapAlertMessage = ""
	}
	if !trapAlertTime.IsZero() && time.Since(trapAlertTime) < 3*time.Second {
		return lastTrapAlert, lastTrapAlertIsError
	}
	trapAlertTime = time.Time{}
	lastTrapAlert = ""
	lastTrapAlertIsError = false
	return "", false
}

// dismissTrapAlert гасит алерт немедленно: inbox + память + таймер одним вызовом.
// Используется при отмене ввода (Escape) и старте нового ввода.
func dismissTrapAlert() {
	TrapAlertIsError = false
	lastTrapAlertIsError = false
	TrapAlertMessage = ""
	lastTrapAlert = ""
	trapAlertTime = time.Time{}
}
