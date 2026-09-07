// config/config_inventory.go
package config

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

/*
================================================================================
КОМПОНЕНТ: SYSTEM INVENTORY & INDEPENDENT CORRELATION (v6.4.0)
НАЗНАЧЕНИЕ: Полный снимок работы хоста (Processes/Sockets) и создание
              атомарного, защищенного от сбоев JSON-слепка для периметра.
================================================================================
*/

// PortTarget описывает структуру сетевого рубежа (порта) защиты Netfilter/UFW.
type PortTarget struct {
	UUID     string `json:"uuid"`     // Уникальный RFC4122 UUID v4 для однозначной идентификации.
	SysTag   string `json:"sys_tag"`  // Слаггифицированный маркер для системных команд (например, "open_ssh").
	Port     int    `json:"port"`     // Сетевой порт (например, 80).
	Protocol string `json:"protocol"` // Протокол (tcp или udp).
	Service  string `json:"service"`  // Человекочитаемое имя для вывода в TUI.
	Mode     string `json:"mode"`     // Режим защиты: "standard" (пакетный лимит) или "tarpit" (задержка).
	Status   string `json:"status"`   // Текущий статус: "active" (защита включена) или "disarm".
}

// AppInventoryItem описывает контракт для автоматически обнаруженного ПО,
// запущенного на ядре Linux. Это результат работы Контура 1 (Process Discovery).
type AppInventoryItem struct {
	Name            string   `json:"name"`             // Имя процесса (например, "sshd").
	DetectionMethod string   `json:"detection_method"` // Метод обнаружения: "dpkg-query" или "marker_file".
	ConfigPath      string   `json:"config_path"`      // Предполагаемый путь к конфигурации.
	LogPaths        []string `json:"log_paths"`        // Список путей к файлам логов (у обогащения).
	Port            int      `json:"port"`             // Порт, на котором приложение слушает.
	IsActive        bool     `json:"is_active"`        // Флаг активности.
	ListenInterface string   `json:"listen_interface"` // Интерфейс прослушивания (например, "0.0.0.0:443").
	IsArmorEnabled  bool     `json:"is_armor_enabled"` // Статус тумблера Брони в JSON.
	ShowInTui       bool     `json:"show_in_tui"`      // Флаг видимости для экрана ANTIDDOS.
}

// PerimeterInventory представляет монолитный контракт конфигурации Amber Guard Lab v6.0.0.
// Это и есть финальный "слепок" состояния системы.
type PerimeterInventory struct {
	ScanTime       time.Time          `json:"scan_time"`       // Время завершения полного сканирования.
	OS             string             `json:"os"`              // Операционная система (всегда "linux").
	ProtectedPorts []PortTarget       `json:"protected_ports"` // Список настроенных рубежей (UFW/Netfilter).
	DetectedApps   []AppInventoryItem `json:"detected_apps"`   // Динамический список обнаруженных процессов/портов.
}

// --- ВНУТРЕННЫЕ МОДЕЛИ ДАННЫХ ДЛЯ АУДИТА (Черновики) ---

// ProcessInfo - Полный снимок, извлеченный из /proc для Контура 1.
type ProcessInfo struct {
	PID            int      // Идентификатор процесса.
	Name           string   // Имя процесса, как оно отображается в /proc/PID/status.
	ExecutablePath string   // Полный путь к исполняемому бинарнику.
	Status         string   // Состояние процесса (running/zombie).
	LogPaths       []string // Список путей к логам, найденных через файловые дескрипторы.
}

// SocketInfo - Изолированные данные из Контура 2 (ss-audit).
type SocketInfo struct {
	Address     string   // Полный адрес прослушивания (e.g., "0.0.0.0:4444").
	Port        int      // Сетевой порт.
	PID         int      // Идентификатор процесса, владеющего сокетом (ключ к C3).
	ProcessName string   // Имя процесса, извлеченное из 'users:(...)' ss.
	IsExternal  bool     // Флаг, определяющий, что порт доступен извне.
	Protocol    string   // Протокол (tcp).
	LogPaths    []string // Пути к логам (для обогащения в C3).
}

// --- ГЛОБАЛЬНЫЕ ПАРАМЕТРЫ И МОНОЛИТ ---

// inventoryPath - Путь к детерминированному дисковому реестру инвентаря периметра.
// БЛОК 4.4: Используем глобальную константу из paths.go
const inventoryPath = FilePerimeterInv

// inventoryMu - Единый мьютекс управления потоками.
// Используется для обеспечения безопасности данных при одновременном доступе
// к файлу инвентаря (например, TUI читает, а Cron пишет).
var inventoryMu sync.RWMutex

// LoadInventory возвращает изолированный срез портов для TUI и Брони.
func LoadInventory() ([]PortTarget, error) {
	inventoryMu.RLock() // Блокируем для чтения
	defer inventoryMu.RUnlock()

	inv, err := loadInventoryRaw()
	if err != nil {
		return nil, err
	}
	// Возвращаем только защищенные порты для отображения.
	return inv.ProtectedPorts, nil
}

// InitInventory — единая точка инициализации инвентаря на диске.
// v8.2.0 (2.4): ленивая инициализация вынесена из читателя.
// Вызывается ОДИН раз при старте процесса; файл существует —
// не трогаем (совместимость с существующими инсталлами).
func InitInventory() error {
	inventoryMu.Lock()
	defer inventoryMu.Unlock()
	if _, err := os.Stat(inventoryPath); err == nil {
		return nil // инвентарь уже есть — инициализация не требуется
	}
	defaultInv := &PerimeterInventory{
		ScanTime: time.Now(),
		OS:       "linux",
		// Эталонный защищаемый рубеж Open SSH (как до v8.2.0).
		ProtectedPorts: []PortTarget{
			{
				UUID:     "8f3b9c2a-1d4e-4f6b-8a9c-0b2d4e6f8a9c",
				SysTag:   "open_ssh",
				Port:     22,
				Protocol: "tcp",
				Service:  "Open SSH",
				Mode:     "standard",
				Status:   "active",
			},
		},
		DetectedApps: []AppInventoryItem{},
	}
	return saveInventoryRaw(defaultInv)
}

// RunPerimeterInventoryScan - Главный оркестратор.
// Инициирует сканирование, выполняя логику Контуров 1, 2 и 3.
func RunPerimeterInventoryScan() error {
	inventoryMu.Lock() // Блокируем на запись (полное обновление системы)
	defer inventoryMu.Unlock()

	// Загружаем текущее состояние перед сканированием.
	inv, err := loadInventoryRaw()
	if err != nil {
		return fmt.Errorf("ошибка инициализации базы при сканировании: %w", err)
	}

	// Контекст с жестким таймаутом. Все операции будут завершены максимум за 5 секунд.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	inv.ScanTime = time.Now()
	inv.DetectedApps = []AppInventoryItem{}

	// ---------------------------------------------------------
	// КОНТУР 1: Обнаружение процессов (Process Tree Discovery)
	// Собираем полный снимок всех процессов, их путей и логов.
	// ---------------------------------------------------------
	processMap, err := discoverAllProcesses(ctx)
	if err != nil {
		return fmt.Errorf("C1 Error (Process Discovery): %w", err)
	}

	// ---------------------------------------------------------
	// КОНТУР 2: Аудит сокетов (Socket Audit Edge)
	// Используем утилиту ss для быстрого аудита всех слушающих сокетов.
	// ---------------------------------------------------------
	ssCmd := exec.CommandContext(ctx, "ss", "-lntp", "-H") // -l=listening, -n=numeric, -t=tcp, -p=pid/program, -H=no header
	ssOutBytes, err := ssCmd.Output()
	if err != nil {
		return fmt.Errorf("C2 Error (Socket Audit Edge): Failed to execute ss: %w", err)
	}
	rawSocketOutput := string(ssOutBytes)
	// Парсинг выдачи ss в структурированный список.
	rawSockets := parseSocketOutput(rawSocketOutput)

	// ---------------------------------------------------------
	// КОНТУР 3: Корреляция и Обогащение (SIEM Correlation Matrix)
	// Сопоставляем сокеты (C2) с процессами (C1).
	// ---------------------------------------------------------
	finalInventory, err := correlateAndEnrich(rawSockets, processMap)
	if err != nil {
		return fmt.Errorf("C3 Error (Correlation): %w", err)
	}

	// ---------------------------------------------------------
	// Финальный этап: Конвертация SocketInfo в AppInventoryItem и сохранение.
	// ---------------------------------------------------------
	for _, socket := range finalInventory {
		item := AppInventoryItem{
			Name:            strings.ToUpper(socket.ProcessName), // Применяем корпоративный стиль
			DetectionMethod: "linux_socket_audit_ss",
			ConfigPath:      fmt.Sprintf("/etc/%s/", socket.ProcessName),
			LogPaths:        socket.LogPaths,
			Port:            socket.Port,
			IsActive:        true,
			ListenInterface: socket.Address,
			IsArmorEnabled:  false,             // На старте защита обычно отключена
			ShowInTui:       socket.IsExternal, // Отображаем только внешние интерфейсы (правило C2)
		}

		// Восстанавливаем сохраненный статус Брони, проверяя квитанции в директории states.
		// Это позволяет системе помнить о защите даже если процесс завершился.
		// БЛОК 4.4: Используем генератор путей из config/paths.go
		receiptPath := ArmorMarkerPath(socket.Port)
		if _, errStat := os.Stat(receiptPath); errStat == nil {
			item.IsArmorEnabled = true
		}

		inv.DetectedApps = append(inv.DetectedApps, item)
	}

	// Сохраняем полный, обновленный слепок в инвентарь.
	return saveInventoryRaw(inv)
}

// --- СЕРДЦЕ АЛГОРИТМА: КОНТУРЫ ---

// discoverAllProcesses выполняет полный снимок системы, итеративно читая /proc.
// Возвращает карту процессов: PID -> ProcessInfo.
func discoverAllProcesses(ctx context.Context) (map[int]*ProcessInfo, error) {
	procMap := make(map[int]*ProcessInfo)

	// Итерация по директории /proc/, где каждый подкаталог - это PID.
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, fmt.Errorf("failed to read /proc: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue // Пропускаем, если это не каталог
		}

		pidStr := entry.Name()
		pid, err := strconv.Atoi(pidStr)
		if err != nil {
			continue // Не PID каталог (например, "self")
		}

		// Проверка, что процесс все еще существует на момент сканирования.
		if _, err := os.Stat(filepath.Join("/proc", pidStr)); os.IsNotExist(err) {
			continue
		}

		pInfo := ProcessInfo{PID: pid}

		// Сбор базовых метаданных о процессе.
		pInfo.Name, pInfo.ExecutablePath, pInfo.Status, err = readProcessInfo(pid)
		if err != nil {
			// Если чтение недоступно (permissions), игнорируем процесс.
			continue
		}

		// Поиск и сбор лог-путей через файловые дескрипторы (FDs).
		pInfo.LogPaths = findLogsViaFDs(pid)

		procMap[pid] = &pInfo
	}

	return procMap, nil
}

// v6.4.0: Исправлен парсинг колонок и добавлена строгая изоляция локального мусора.
// parseSocketOutput анализирует сырой вывод утилиты 'ss' и извлекает ИСКЛЮЧИТЕЛЬНО внешние,
// боевые сокеты. Это реализация фильтров Контура 2.
func parseSocketOutput(ssOutput string) []SocketInfo {
	var sockets []SocketInfo

	// Регулярные выражения для извлечения PID и имени процесса из блока 'users:(...)'.
	pidRegex := regexp.MustCompile(`pid=(\d+)`)
	procNameRegex := regexp.MustCompile(`"([^"]+)"`)

	lines := strings.Split(ssOutput, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "LISTEN") {
			continue // Нас интересуют только прослушивающие сокеты
		}

		// Сокеты 'ss' структурированы, но зависят от версии ОС.
		// Мы полагаемся на, что в режиме -H адрес и порт находятся в определенных полях.
		fields := strings.Fields(line)
		// В режиме без шапки ss -lntp -H строки имеют 4 или 5 основных полей до блока users
		if len(fields) < 4 {
			continue // Недостаточно полей для анализа
		}

		// Локальный адрес прослушивания (C2) - это 4-й элемент (индекс 3)
		addrPort := fields[3]

		// Находим последнее двоеточие с конца для корректной поддержки IPv6 ([::]:80) и суффиксов %lo
		lastColon := strings.LastIndex(addrPort, ":")
		if lastColon == -1 {
			continue
		}

		address := addrPort[:lastColon]
		portStr := addrPort[lastColon+1:]
		port, err := strconv.Atoi(portStr)
		if err != nil || port == 0 {
			continue
		}

		// ИБ-ПРЕДОХРАНИТЕЛЬ 1: Фильтрация VNC-консолей гипервизоров (защита от шума)
		if port >= 5900 && port <= 5999 {
			continue
		}

		// ИБ-ПРЕДОХРАНИТЕЛЬ 2: Тотальная зачистка локальных петель (ключевое требование алгоритма)
		if strings.HasPrefix(address, "127.") ||
			strings.Contains(strings.ToLower(address), "localhost") ||
			strings.Contains(address, "::1") ||
			strings.Contains(address, "%lo") {
			// Сокет прослушивает только локальную сеть, игнорируем его для инвентаря периметра.
			continue
		}

		// Парсинг PID и Имени процесса (подготовка к Контуру 3)
		processName := "UNKNOWN_PROCESS"
		pid := 0

		if strings.Contains(line, "users:(") {
			if pidMatch := pidRegex.FindStringSubmatch(line); len(pidMatch) > 1 {
				pid, _ = strconv.Atoi(pidMatch[1])
			}
			if nameMatch := procNameRegex.FindStringSubmatch(line); len(nameMatch) > 1 {
				processName = nameMatch[1]
			}
		}

		// Если сокет прошел все фильтры, он считается потенциально внешним рубежом.
		sockets = append(sockets, SocketInfo{
			Address:     addrPort,
			Port:        port,
			PID:         pid,
			ProcessName: strings.ToUpper(processName),
			IsExternal:  true,
			Protocol:    "tcp",
			LogPaths:    []string{}, // Будут заполнены в Контуре 3
		})
	}
	return sockets
}

// correlateAndEnrich объединяет данные C2 (Sockets) и C1 (Processes).
func correlateAndEnrich(sockets []SocketInfo, processes map[int]*ProcessInfo) ([]SocketInfo, error) {
	enrichedSockets := make([]SocketInfo, 0, len(sockets))

	for _, socket := range sockets {
		process, found := processes[socket.PID]

		if found {
			// Сценарий 1: PID найден. Сопоставляем и обогащаем данные (C1 -> C2).
			socket.ProcessName = process.Name
			socket.LogPaths = process.LogPaths
			enrichedSockets = append(enrichedSockets, socket)
		} else {
			// Сценарий 2 (Amber Guard Lab): PID не найден, но порт активен.
			// Принудительно помечаем как подозрительный и сохраняем сокет,
			// не удаляя его из инвентаря.
			socket.ProcessName = "UNKNOWN_PROCESS (Suspicious Activity Detect)"
			// Сокет всегда сохраняется, так как он представляет собой открытую дверь.
			enrichedSockets = append(enrichedSockets, socket)
		}
	}

	return enrichedSockets, nil
}

// --- Вспомогательные функции для Контура 1 (/proc) ---

// readProcessInfo читает имя, путь и статус процесса по PID из файловой системы.
func readProcessInfo(pid int) (name string, path string, status string, err error) {
	// Чтение статуса и имени из /proc/PID/status
	statusPath := filepath.Join("/proc", strconv.Itoa(pid), "status")
	statusContent, err := os.ReadFile(statusPath)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to read status: %w", err)
	}

	lines := strings.Split(string(statusContent), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "Name:") {
			// Извлечение имени процесса.
			name = strings.TrimSpace(strings.TrimPrefix(line, "Name:"))
		} else if strings.HasPrefix(line, "State:") {
			// Извлечение состояния процесса.
			status = strings.TrimSpace(strings.Split(line, ":")[1])
		}
	}

	// Чтение exe (пути к исполняемому файлу).
	// v8.1.1 (M0d / K-24): filepath.EvalSymlinks полностью разрешает цепочку
	// симлинков /proc/PID/exe до реального бинарника.
	exePath := filepath.Join("/proc", strconv.Itoa(pid), "exe")
	info, err := filepath.EvalSymlinks(exePath)
	if err == nil {
		path = info
	} else {
		path = "N/A"
	}
	return name, path, status, nil
}

// findLogsViaFDs ищет лог-файлы, которые процесс открыл через файловые дескрипторы.
func findLogsViaFDs(pid int) []string {
	var logs []string
	fdPath := filepath.Join("/proc", strconv.Itoa(pid), "fd")

	entries, err := os.ReadDir(fdPath)
	if err != nil {
		return nil // Игнорируем, если нет доступа
	}

	for _, file := range entries {
		realPath, err := os.Readlink(filepath.Join(fdPath, file.Name()))
		if err != nil {
			continue // Игнорируем недоступные дескрипторы
		}

		// Фильтрация: ищем пути, содержащие /var/log/, что обычно указывает на системные логи.
		if strings.HasPrefix(realPath, "/var/log/") {
			// Фильтруем временные и сокеты, которые могут быть символически связаны.
			if strings.Contains(realPath, "null") {
				continue
			}
			logs = append(logs, realPath)
		}
	}
	return logs
}

// --- АТОМНЫЕ ДИССКОВО-ЗАПИСНЫЕ ФУНКЦИИ (С ИСПРАВЛЕНИЕМ РИСКА СБОЯ) ---

// Slugify преобразует читаемое имя в системный sys_tag.
func Slugify(name string) string {
	tag := strings.ToLower(name)
	// Удаляем все символы, кроме букв и цифр.
	tag = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		// Оставляем разделители, но они будут удалены в следующем шаге.
		if r == ' ' || r == '-' || r == '_' || r == '.' {
			return r
		}
		return -1
	}, tag)

	// Заменяем множественные разделители на один.
	for strings.Contains(tag, "__") {
		tag = strings.ReplaceAll(tag, "__", "_")
	}
	return strings.Trim(tag, "_")
}

// loadInventoryRaw — читает содержимое JSON-инвентаря.
func loadInventoryRaw() (*PerimeterInventory, error) {
	// v8.2.0 (2.4): читатель НЕ создаёт файл. Инвентарь отсутствует —
	// возвращаем пустой слепок; создание — обязанность InitInventory
	// (старт процесса) и функций-писателей (скан, AddPort и т.д.).
	if _, err := os.Stat(inventoryPath); os.IsNotExist(err) {
		return &PerimeterInventory{
			OS:             "linux",
			ProtectedPorts: []PortTarget{},
			DetectedApps:   []AppInventoryItem{},
		}, nil
	}

	data, err := os.ReadFile(inventoryPath)
	if err != nil {
		return nil, err
	}

	var inv PerimeterInventory
	if err := json.Unmarshal(data, &inv); err != nil {
		return nil, err
	}
	return &inv, nil
}

// saveInventoryRaw — реализует атомарную запись с принудительным сбросом (Sync).
func saveInventoryRaw(inv *PerimeterInventory) error {
	// Сериализуем обновленную структуру.
	newData, err := json.MarshalIndent(inv, "", "  ")
	if err != nil {
		return err
	}

	tmpFile := inventoryPath + ".tmp"

	// 1. Открываем временный файл для записи.
	f, err := os.OpenFile(tmpFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	// 2. Записываем данные в буфер файла.
	_, err = f.Write(newData)
	if err != nil {
		return err
	}

	// 3. !!! ВАЖНО: Вызываем f.Sync(). Это принудительно сбрасывает
	// данные из оперативной памяти ядра (buffer) на физический накопитель.
	// Это устраняет риск потери данных при внезапном отключении питания.
	if err = f.Sync(); err != nil {
		return fmt.Errorf("failed to sync file to disk: %w", err)
	}

	// 4. Выполняем атомарную смену имени.
	if err := os.Rename(tmpFile, inventoryPath); err != nil {
		// Попытка удалить временный файл, если переименование не удалось.
		_ = os.Remove(tmpFile)
		return err
	}
	return nil
}

// --- API ГЕНЕРАЦИЯ И ОБНОВЛЕНИЕ (Управление рубежами) ---

// AddPort атомарно добавляет новый защищаемый рубеж по правилам СTI.
func AddPort(uuid, name string, port int, protocol string) error {
	inventoryMu.Lock()
	defer inventoryMu.Unlock()
	inv, err := loadInventoryRaw()
	if err != nil {
		return err
	}
	// Проверка на дублирование.
	for _, p := range inv.ProtectedPorts {
		if p.Port == port && p.Protocol == protocol {
			return fmt.Errorf("рубеж для порта %d/%s уже существует", port, protocol)
		}
	}
	newTarget := PortTarget{
		UUID:     uuid,
		SysTag:   Slugify(name),
		Port:     port,
		Protocol: protocol,
		Service:  name,
		Mode:     "standard",
		Status:   "disarm", // По умолчанию новый рубеж выключен.
	}
	inv.ProtectedPorts = append(inv.ProtectedPorts, newTarget)
	return saveInventoryRaw(inv)
}

// UpdatePortStatus изменяет рантайм-режимы и стейты рубежа.
func UpdatePortStatus(uuid, mode, status string) error {
	inventoryMu.Lock()
	defer inventoryMu.Unlock()
	inv, err := loadInventoryRaw()
	if err != nil {
		return err
	}
	for i, p := range inv.ProtectedPorts {
		if p.UUID == uuid {
			inv.ProtectedPorts[i].Mode = mode
			inv.ProtectedPorts[i].Status = status
			return saveInventoryRaw(inv)
		}
	}
	return fmt.Errorf("рубеж с UUID %s не найден", uuid)
}

// DeletePort атомарно удаляет порт брандмауэра по UUID.
func DeletePort(uuid string) error {
	inventoryMu.Lock()
	defer inventoryMu.Unlock()
	inv, err := loadInventoryRaw()
	if err != nil {
		return err
	}
	var updatedPorts []PortTarget
	found := false
	for _, p := range inv.ProtectedPorts {
		if p.UUID == uuid {
			found = true
			// Этот порт не включается в новый список.
			continue
		}
		updatedPorts = append(updatedPorts, p)
	}
	if !found {
		return fmt.Errorf("рубеж с UUID %s не найден", uuid)
	}
	inv.ProtectedPorts = updatedPorts
	return saveInventoryRaw(inv)
}

// LoadDetectedApps возвращает динамический срез обнаруженных процессов и портов для TUI.
// v6.4.0: Безопасное чтение под RLock мьютексом.
func LoadDetectedApps() ([]AppInventoryItem, error) {
	inventoryMu.RLock() // Блокируем для чтения
	defer inventoryMu.RUnlock()

	inv, err := loadInventoryRaw()
	if err != nil {
		return nil, err
	}
	return inv.DetectedApps, nil
}

// UpdateDetectedAppArmorStatus атомарно обновляет статус брони обнаруженной
// службы в инвентаре периметра. v8.2.0 (1.9): единая транзакционная точка —
// инвентарь больше не перезаписывается «обрезанной» локальной структурой,
// поля ScanTime, OS и ProtectedPorts сохраняются нетронутыми.
func UpdateDetectedAppArmorStatus(appName string, port int, armorEnabled bool) error {
	inventoryMu.Lock()
	defer inventoryMu.Unlock()
	inv, err := loadInventoryRaw()
	if err != nil {
		return err
	}
	for i, app := range inv.DetectedApps {
		if (port > 0 && app.Port == port) || strings.EqualFold(app.Name, appName) {
			inv.DetectedApps[i].IsArmorEnabled = armorEnabled
			if armorEnabled {
				inv.DetectedApps[i].IsActive = true
			}
			break
		}
	}
	return saveInventoryRaw(inv)
}
