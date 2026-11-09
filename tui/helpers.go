package tui

import (
	"regexp"
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
