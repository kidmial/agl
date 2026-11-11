package tui

import (
	"regexp"
	"strings"
	"unicode/utf8"
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
