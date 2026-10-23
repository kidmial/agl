// internal/analytics/ingest.go
// =====================================================================
// КОМПОНЕНТ: ПОТОКОВЫЙ ИНЖЕСТ ФОРЕНЗИК-ЛОГОВ
// РОЛЬ: Чтение хвоста tarpit_report.log и передача событий в GlobalStore.
// ПРИНЦИП:
//   - работает в фоновой горутине;
//   - устойчив к ротации через copytruncate (детектит уменьшение размера файла);
//   - не блокирует основной процесс AGL;
//   - пропускает битые строки, но учитывает их в счётчиках качества.
//
// =====================================================================
package analytics

import (
	"bufio"
	"io"
	"os"
	"time"

	"agl/internal/sys"
)

// IngestConfig содержит параметры инжеста.
type IngestConfig struct {
	// LogPath — путь к файлу лога (например, /var/log/amberguard/tarpit_report.log).
	LogPath string

	// PollInterval — как часто проверять появление новых данных.
	// Для логов капкана 1-2 секунды более чем достаточно.
	PollInterval time.Duration

	// Stop — канал для корректной остановки горутины инжеста.
	//
	// Если nil — горутина работает до завершения процесса (поведение по умолчанию,
	// обратно совместимо с продом). Если канал закрыт — горутина завершает цикл,
	// освобождает ресурсы и выходит.
	//
	// Нужен для:
	//   - регрессионных тестов (иначе горутина утекает);
	//   - будущего graceful shutdown при остановке сервиса.
	Stop <-chan struct{}
}

// DefaultIngestConfig возвращает конфигурацию инжеста по умолчанию.
func DefaultIngestConfig(logPath string) IngestConfig {
	return IngestConfig{
		LogPath:      logPath,
		PollInterval: 1 * time.Second,
	}
}

// StartIngest запускает фоновый процесс чтения лога.
//
// Аргументы:
//   - cfg: параметры инжеста.
//
// Поведение:
//   - открывает файл;
//   - переходит в конец файла (чтобы не парсить всю историю при старте);
//   - запускает бесконечный цикл чтения новых строк;
//   - при обнаружении уменьшения размера файла (copytruncate) сбрасывает оффсет в 0.
//
// Важно:
//   - функция не блокирует вызывающий код;
//   - останавливается только при завершении процесса или панике;
//   - все ошибки чтения логируются через sys.LogAction.
func StartIngest(cfg IngestConfig) {
	// Запускаем инжест в отдельной горутине.
	// Это гарантирует, что основной процесс AGL не будет заблокирован.
	go func() {
		sys.LogAction("ANALYTICS", "INGEST_START", "INFO", "Starting log ingest for "+cfg.LogPath)

		// Открываем файл для чтения.
		file, err := os.Open(cfg.LogPath)
		if err != nil {
			// Если файл ещё не создан (первый запуск), ждём и пробуем снова.
			// Не пишем ERROR, так как это нормальная ситуация при первом старте.
			sys.LogAction("ANALYTICS", "INGEST_OPEN_FAIL", "WARNING", "Log file not found, waiting: "+err.Error())
			if cfg.Stop != nil {
				select {
				case <-cfg.Stop:
					sys.LogAction("ANALYTICS", "INGEST_STOP", "INFO", "Ingest stopped during wait")
					return
				case <-time.After(5 * time.Second):
				}
			} else {
				time.Sleep(5 * time.Second)
			}
			go StartIngest(cfg) // Рекурсивный перезапуск через 5 секунд
			return
		}
		defer file.Close()

		// Переходим в конец файла.
		// Мы не хотим парсить всю историю при каждом старте демона.
		// Исторические данные можно загрузить отдельно через RebuildFromStore при необходимости.
		_, err = file.Seek(0, io.SeekEnd)
		if err != nil {
			sys.LogAction("ANALYTICS", "INGEST_SEEK_FAIL", "ERROR", "Failed to seek to end of log: "+err.Error())
			return
		}

		// Фиксируем путь для диагностики
		if GlobalStore != nil {
			GlobalStore.SetIngestPath(cfg.LogPath)
		}

		// Создаём сканер для построчного чтения.
		scanner := bufio.NewScanner(file)
		// Увеличиваем буфер сканера на случай очень длинных строк (например, с огромными токенами).
		buf := make([]byte, 64*1024)
		scanner.Buffer(buf, 1024*1024)

		// Переменная для отслеживания текущего размера файла.
		// Нужна для детекции copytruncate.
		var lastSize int64

		// Бесконечный цикл чтения.
		// Если Stop задан — слушаем канал выхода через select.
		// Если Stop == nil — select не используется, поведение прода не меняется.
		for {
			// Проверяем текущий размер файла.
			stat, err := file.Stat()
			if err == nil {
				currentSize := stat.Size()

				// Если текущий размер меньше последнего известного размера,
				// значит произошла ротация через copytruncate.
				// Файл был обрезан, и наш текущий оффсет теперь за пределами файла.
				if currentSize < lastSize {
					sys.LogAction("ANALYTICS", "INGEST_ROTATION", "INFO", "Log rotation detected (copytruncate), resetting offset")

					// Сбрасываем оффсет в начало файла.
					_, err = file.Seek(0, io.SeekStart)
					if err != nil {
						sys.LogAction("ANALYTICS", "INGEST_RESET_FAIL", "ERROR", "Failed to reset offset after rotation: "+err.Error())
						time.Sleep(cfg.PollInterval)
						continue
					}

					// Сбрасываем сканер, так как базовый io.Reader изменил позицию.
					scanner = bufio.NewScanner(file)
					scanner.Buffer(buf, 1024*1024)
				}

				lastSize = currentSize
			}

			// Пытаемся прочитать следующую строку.
			if scanner.Scan() {
				line := scanner.Text()

				// Парсим строку в сырое событие.
				// LineNum здесь не имеет смысла для tail-чтения, ставим 0.
				rawEvent, err := ParseLine(line, 0, cfg.LogPath)
				if err != nil {
					// Пустые строки игнорируем молча.
					if err != ErrEmptyLine {
						// Битые строки логируем как проблемы качества данных, но не останавливаем инжест.
						sys.LogAction("ANALYTICS", "INGEST_PARSE_ERROR", "WARNING", "Failed to parse line: "+err.Error())
						if GlobalStore != nil {
							GlobalStore.RecordIngestError(line, err)
						}
					}
					continue
				}

				// Если GlobalStore инициализирован, добавляем событие.
				if GlobalStore != nil {
					// Нормализуем и сохраняем в кольцевой буфер
					normalized := GlobalStore.AddRaw(rawEvent)

					// Передаем в менеджер профилей для построения досье актора
					if GlobalManager != nil {
						GlobalManager.ObserveEvent(normalized)
					}
				}
			} else {
				// Если scanner.Scan() вернул false, проверяем, была ли это ошибка.
				if err := scanner.Err(); err != nil {
					sys.LogAction("ANALYTICS", "INGEST_SCAN_ERROR", "ERROR", "Scanner error: "+err.Error())
					// При серьёзной ошибке сканера (например, файл удалён),
					// закрываем текущий файл и пробуем перезапустить инжест.
					file.Close()
					if cfg.Stop != nil {
						select {
						case <-cfg.Stop:
							sys.LogAction("ANALYTICS", "INGEST_STOP", "INFO", "Ingest stopped during error recovery")
							return
						case <-time.After(5 * time.Second):
						}
					} else {
						time.Sleep(5 * time.Second)
					}
					go StartIngest(cfg)
					return
				}

				// Достигнут EOF (нет новых данных).
				//bufio.Scanner в Go после EOF ставит внутренний флаг s.done = true,
				// и все последующие Scan() сразу возвращают false, даже если
				// писатель дописал новые байты в файл. Это documented behavior.
				// Решение: пересоздать scanner, чтобы сбросить флаг s.done.
				// Следующий Scan() снова попытается читать из os.File,
				// и если писатель дописал данные — прочитает их.
				scanner = bufio.NewScanner(file)
				scanner.Buffer(buf, 1024*1024)

				// Новых данных нет. Ждём следующую итерацию или сигнал остановки.
				//
				// Если Stop не задан — select работает как обычный sleep (nil-канал
				// в select блокирует forever, срабатывает только таймер).
				// Если Stop закрыт — выходим из цикла и освобождаем defer file.Close().
				if cfg.Stop != nil {
					select {
					case <-cfg.Stop:
						sys.LogAction("ANALYTICS", "INGEST_STOP", "INFO", "Ingest stopped by signal")
						return
					case <-time.After(cfg.PollInterval):
						// Таймер сработал, продолжаем цикл.
					}
				} else {
					time.Sleep(cfg.PollInterval)
				}
			}
		}
	}()
}
