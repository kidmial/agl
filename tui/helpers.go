// /tui/helpers.go
package tui

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/gizak/termui/v3/widgets"
)

// ============================================================================
// ОБЩИЕ ХЕЛПЕРЫ ДЛЯ TUI-МОДУЛЕЙ
// ============================================================================

// uiTagRe матчит markup-теги termui вида "[текст](fg:...,mod:...)".
// Ленивая группа берёт внутренний текст вместе с экранированными скобками
// (например "[ ACTIVE ]"), отбрасывая невидимый модификатор.
var uiTagRe = regexp.MustCompile(`\[(.*?)\]\([^)]*\)`)

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
