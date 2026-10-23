// internal/analytics/rebuild.go
// =====================================================================
// КОМПОНЕНТ: ЗАГРУЗКА ИСТОРИИ ИЗ ЛОГА
// РОЛЬ: Полный rebuild истории с обновлением профилей.
// ПРИНЦИП:
//   - guard от повторной загрузки (защита от удвоения счётчиков);
//   - полный цикл: парсинг → нормализация → store → manager;
//   - используется и авто-rebuild при старте, и ручная кнопка в UI.
//
// =====================================================================
package analytics

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"

	"agl/internal/sys"
)

// rebuildMu и rebuildDone защищают от повторной загрузки истории.
// Повторный вызов RebuildHistory удваивает счётчики - это недопустимо.
// Guard сбрасывается автоматически при рестарте сервиса.
var (
	rebuildMu   sync.Mutex
	rebuildDone bool
)

// MarkHistoryLoaded помечает историю как загруженную.
// Вызывается после успешного rebuild (авто или ручного).
func MarkHistoryLoaded() {
	rebuildMu.Lock()
	rebuildDone = true
	rebuildMu.Unlock()
}

// IsHistoryLoaded возвращает true, если история уже загружена в этом процессе.
// Используется UI для отображения состояния кнопки.
func IsHistoryLoaded() bool {
	rebuildMu.Lock()
	defer rebuildMu.Unlock()
	return rebuildDone
}

// RebuildResult содержит статистику загрузки истории.
type RebuildResult struct {
	LinesRead int
	Parsed    int
	Valid     int
	Broken    int
}

// RebuildHistory загружает всю историю из файла лога.
//
// Аргументы:
//   - logPath: путь к файлу лога.
//
// Возврат:
//   - RebuildResult со статистикой загрузки (или nil если уже загружено).
//   - error при проблеме открытия файла.
//
// Поведение:
//   - если IsHistoryLoaded() == true, возвращает nil без действий;
//   - читает файл построчно;
//   - для каждой строки: ParseLine -> AddRaw -> ObserveEvent;
//   - считает parsed/valid/broken;
//   - после успеха помечает историю как загруженную.
//
// Важно:
//   - это полная загрузка с обновлением профилей;
//   - store.IngestFile() НЕ подходит, так как не обновляет ProfileManager.
func RebuildHistory(logPath string) (*RebuildResult, error) {
	// Проверяем guard ДО захвата файла.
	// Если уже загружено, сразу возвращаем nil.
	rebuildMu.Lock()
	if rebuildDone {
		rebuildMu.Unlock()
		return nil, nil // уже загружено
	}
	rebuildDone = true
	rebuildMu.Unlock()

	// Открываем файл.
	file, err := os.Open(logPath)
	if err != nil {
		// Откатываем флаг, чтобы можно было повторить попытку.
		rebuildMu.Lock()
		rebuildDone = false
		rebuildMu.Unlock()
		return nil, fmt.Errorf("cannot open log: %w", err)
	}
	defer file.Close()

	// Создаём сканер для построчного чтения.
	// Увеличенный буфер на случай длинных строк с токенами.
	scanner := bufio.NewScanner(file)
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	result := &RebuildResult{}

	// Читаем файл построчно.
	for scanner.Scan() {
		result.LinesRead++
		line := scanner.Text()

		// Пропускаем пустые строки.
		if strings.TrimSpace(line) == "" {
			continue
		}

		// Парсим строку в сырое событие.
		rawEvent, err := ParseLine(line, result.LinesRead, logPath)
		if err != nil {
			result.Broken++
			continue
		}

		// Нормализуем и добавляем в хранилище.
		result.Parsed++
		normalized := GlobalStore.AddRaw(rawEvent)
		if normalized.Valid {
			result.Valid++
		}

		// Передаём в менеджер профилей для построения досье актора.
		if GlobalManager != nil {
			GlobalManager.ObserveEvent(normalized)
		}
	}

	// Логируем результат загрузки.
	sys.LogAction("ANALYTICS", "REBUILD_COMPLETE", "INFO",
		fmt.Sprintf("History loaded: %d lines, %d parsed, %d valid, %d broken",
			result.LinesRead, result.Parsed, result.Valid, result.Broken))

	return result, nil
}
