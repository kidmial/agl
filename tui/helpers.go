// /tui/helpers.go
package tui

import (
	"agl/i18n"
	"fmt"
	"image"
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

// TitleSegment описывает фрагмент заголовка со своим стилем.
type TitleSegment struct {
	Text  string
	Style ui.Style
}

// ScreenHeader — шапка v9.0.0 с поддержкой разноцветных сегментов и 4 углов.
type ScreenHeader struct {
	*widgets.Paragraph
	TopLeft     []TitleSegment
	TopRight    []TitleSegment
	BottomLeft  []TitleSegment
	BottomRight []TitleSegment
}

// Draw отрисовывает стандартный блок и впечатывает цветные заголовки поверх рамки.
func (h *ScreenHeader) Draw(buf *ui.Buffer) {
	h.Paragraph.Draw(buf)
	if !h.Border {
		return
	}
	stampTitleSegments(buf, h.GetRect(), h.TopLeft, h.TopRight, h.BottomLeft, h.BottomRight)
}

// newScreenHeader собирает шапку экрана с разноцветным титулом и поддержкой нижних углов.
// Сохраняет совместимость по сигнатуре вызова и использует твой i18n и trunc.
var (
	hdrYellow = ui.NewStyle(ui.ColorYellow, ui.ColorClear, ui.ModifierBold)
	hdrWhite  = ui.NewStyle(ui.ColorWhite, ui.ColorClear, ui.ModifierBold)
	hdrCyan   = ui.NewStyle(ui.ColorCyan, ui.ColorClear, ui.ModifierBold)
)

// stampTitleSegments впечатывает цветные сегменты поверх рамки блока.
// Единая механика для ScreenHeader (Paragraph) и ScreenList (List).
// Поклеточная запись с guard: за рамку не пишем никогда (fail-safe).
func stampTitleSegments(buf *ui.Buffer, r image.Rectangle, topLeft, topRight, bottomLeft, bottomRight []TitleSegment) {
	drawSegs := func(startX, y int, segs []TitleSegment) {
		x := startX
		for _, s := range segs {
			for _, rn := range s.Text {
				if x >= r.Max.X-1 || x < r.Min.X+1 {
					break
				}
				buf.SetCell(ui.Cell{Rune: rn, Style: s.Style}, image.Pt(x, y))
				x++
			}
		}
	}
	calcWidth := func(segs []TitleSegment) int {
		w := 0
		for _, s := range segs {
			w += utf8.RuneCountInString(s.Text)
		}
		return w
	}
	if len(topLeft) > 0 {
		drawSegs(r.Min.X+2, r.Min.Y, topLeft)
	}
	if len(topRight) > 0 {
		drawSegs(r.Max.X-2-calcWidth(topRight), r.Min.Y, topRight)
	}
	if len(bottomLeft) > 0 {
		drawSegs(r.Min.X+2, r.Max.Y-1, bottomLeft)
	}
	if len(bottomRight) > 0 {
		drawSegs(r.Max.X-2-calcWidth(bottomRight), r.Max.Y-1, bottomRight)
	}
}

// buildTitleSegs собирает сегменты титула с бюджетом и датой/время справа.
// Данные (титул, дата, время) — white; бренд — янтарный + white.
func buildTitleSegs(leftSegs []TitleSegment) ([]TitleSegment, []TitleSegment) {
	now := time.Now()
	// rightStr := fmt.Sprintf(" %s: %s | %s: %s ",
	// i18n.T.ViewMainLabelDate, now.Format("2006-01-02"),
	// i18n.T.ViewMainLabelTime, now.Format("15:04:05"))
	rightStr := fmt.Sprintf(" %s | %s ",
		now.Format("2006-01-02"),
		now.Format("15:04:05"))
	allowed := headerTitleBudget - utf8.RuneCountInString(rightStr) - 1
	var segs []TitleSegment
	used := 0
	for _, s := range leftSegs {
		if allowed-used <= 0 {
			break
		}
		t := s.Text
		if n := utf8.RuneCountInString(t); used+n > allowed {
			t = trunc(t, allowed-used)
		}
		segs = append(segs, TitleSegment{Text: t, Style: s.Style})
		used += utf8.RuneCountInString(t)
	}
	return segs, []TitleSegment{{Text: rightStr, Style: hdrWhite}}
}

// brandSegs — бренд суб-экранов из HeaderBrandShort: первый символ янтарный, хвост white.
func brandSegs(screenTitle string) []TitleSegment {
	b := []rune(HeaderBrandShort)
	return []TitleSegment{
		{Text: " ", Style: hdrWhite},
		{Text: string(b[:1]), Style: hdrYellow},
		{Text: string(b[1:]), Style: hdrWhite},
		{Text: " - " + screenTitle + " ", Style: hdrCyan},
	}
}

func newScreenHeader(screenTitle string, x0, y0, x1, y1 int) *ScreenHeader {
	left, right := buildTitleSegs(brandSegs(screenTitle))
	p := widgets.NewParagraph()
	p.Title = ""
	p.SetRect(x0, y0, x1, y1)
	p.BorderStyle.Fg = ui.ColorCyan
	p.WrapText = false
	return &ScreenHeader{Paragraph: p, TopLeft: left, TopRight: right}
}

// newMainHeader — бренд главного экрана из HeaderBrand: первое слово янтарное, хвост white.
func newMainHeader(version string, x0, y0, x1, y1 int) *ScreenHeader {
	idx := strings.Index(HeaderBrand, " ")
	left, right := buildTitleSegs([]TitleSegment{
		{Text: " ", Style: hdrWhite},
		{Text: HeaderBrand[:idx], Style: hdrYellow},
		{Text: HeaderBrand[idx:], Style: hdrWhite},
		{Text: " - " + version + " ", Style: hdrWhite},
	})
	p := widgets.NewParagraph()
	p.Title = ""
	p.SetRect(x0, y0, x1, y1)
	p.BorderStyle.Fg = ui.ColorCyan
	p.WrapText = false
	return &ScreenHeader{Paragraph: p, TopLeft: left, TopRight: right}
}

// ScreenList — List-блок с сегментным титулом (одиночный блок SETTINGS).
type ScreenList struct {
	*widgets.List
	TopLeft     []TitleSegment
	TopRight    []TitleSegment
	BottomLeft  []TitleSegment
	BottomRight []TitleSegment
}

func (l *ScreenList) Draw(buf *ui.Buffer) {
	l.List.Draw(buf)
	if !l.Border {
		return
	}
	stampTitleSegments(buf, l.GetRect(), l.TopLeft, l.TopRight, l.BottomLeft, l.BottomRight)
}

func newScreenListHeader(screenTitle string, x0, y0, x1, y1 int) *ScreenList {
	left, right := buildTitleSegs(brandSegs(screenTitle))
	l := widgets.NewList()
	l.Title = ""
	l.SetRect(x0, y0, x1, y1)
	l.BorderStyle.Fg = ui.ColorCyan
	l.SelectedRowStyle = ui.NewStyle(ui.ColorBlack, ui.ColorGreen, ui.ModifierBold)
	return &ScreenList{List: l, TopLeft: left, TopRight: right}
}

// Окончание сборщика шапки

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
