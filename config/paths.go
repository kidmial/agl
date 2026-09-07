// config/paths.go
package config

import "fmt"

// ============================================================================
// ГЕНЕРАТОРЫ ПУТЕЙ (ФАБРИКА МАРКЕРОВ)
// ============================================================================
// PURPOSE: Централизованное производство путей к летучим и персистентным
// маркерам состояний подсистем Брони и Дегтярной ямы.
//
// CONTRACT: Принимает идентификатор (порт или имя сервиса), возвращает
// абсолютный детерминированный путь. Никогда не возвращает пустую строку.
// SIDE-EFFECTS: Нет (чистая функция, только конкатенация строк).
// ============================================================================

// ============================================================================
// КОНСТАНТНАЯ ГЕОМЕТРИЯ ФАЙЛОВОЙ СИСТЕМЫ AMBER GUARD LAB v8.2.2
// БЛОК 4.4: Единая точка определения всех путей проекта.
// ============================================================================
const (
	DirOpt = "/opt/amberguard"            // Изолированная зона скомпилированного кода
	DirEtc = "/etc/amberguard"            // Статические конфигурации системы
	DirLib = "/var/lib/amberguard/states" // Геометрия сервера (Энергонезависимые маркеры диска)
	DirRun = "/var/run/amberguard"        // Operational память состояний (Летучий Runtime в ОЗУ)
	DirLog = "/var/log/amberguard"        // Системные журналы и архивы логов

	FileShm          = "/dev/shm/amberguard.counter"        // Сверхскоростной IPC-мост веб-стука в Shared Memory
	FileConfig       = DirEtc + "/config.json"              // Центральный файл конфигурации
	FileMainLog      = DirLog + "/amberguard.log"           // Главный журнал системы
	FilePerimeterInv = DirEtc + "/perimeter_inventory.json" // Инвентарь периметра

	// Маркеры состояний
	MarkerSshMoved       = DirLib + "/agl_ssh_moved"          // Маркер рокировки (кастлинга) SSH
	FileAdminIdleCounter = DirLib + "/agl_admin_idle_counter" // Счётчик простоя администратора (Watcher)

	// Журналы подсистем
	FileTarpitConnLog = DirLog + "/tarpit_connection.log" // Журнал подключений дегтярной ямы
	FileTarpitRptLog  = DirLog + "/tarpit_report.log"     // Отчётный журнал дегтярной ямы

	// Бинарник и инвентари
	FileBinary          = DirOpt + "/agl"                   // Исполняемый файл комплекса
	FileTarpitInventory = DirEtc + "/tarpit_inventory.json" // Декларативный инвентарь ловушек

	// Runtime-директории
	DirWebRescue = "/dev/shm/agl_proto_web_rescue" // Временная папка веб-стука (tmpfs, очищается при ребуте)
)

// ArmorServiceMarkerPath возвращает путь к квитанции брони для именованного сервиса.
func ArmorServiceMarkerPath(service string) string {
	return fmt.Sprintf("%s/agl_armor_active_service.%s", DirLib, service)
}

// ArmorRateLimitMarkerPath возвращает путь к маркеру кастомного rate-limit правила.
func ArmorRateLimitMarkerPath(port int) string {
	return fmt.Sprintf("%s/agl_armor_ratelimit_port.%d", DirLib, port)
}

// TarpitMarkerPath возвращает путь к маркеру активной ловушки для порта.
// Заменяет хардкод и fmt.Sprintf в TUI и main.go.
func TarpitMarkerPath(port int) string {
	return fmt.Sprintf("%s/agl_tarpit_listener_port.%d", DirLib, port)
}

// ArmorMarkerPath возвращает путь к квитанции активной брони для порта.
// Заменяет хардкод и fmt.Sprintf в TUI и main.go.
func ArmorMarkerPath(port int) string {
	return fmt.Sprintf("%s/agl_armor_active_port.%d", DirLib, port)
}
