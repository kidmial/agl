// config/config.go
package config

import (
	"agl/i18n"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// SysctlCoreConfig изолирует двухуровневые параметры тюнинга сетевого стека ядра Linux.
// Содержит жесткие эталоны Amber Guard Lab (default_) и изменяемые боевые крутилки (armor_).
type SysctlCoreConfig struct {
	// --- Заводские боевые константы Amber Guard Lab (Зашиты намертво для отката) ---
	DefaultNetIpv4TcpSyncookies       int `json:"default_net_ipv4_tcp_syncookies"`
	DefaultNetIpv4TcpFinTimeout       int `json:"default_net_ipv4_tcp_fin_timeout"`
	DefaultNetIpv4TcpSynRetries       int `json:"default_net_ipv4_tcp_syn_retries"`
	DefaultNetIpv4TcpSynackRetries    int `json:"default_net_ipv4_tcp_synack_retries"`
	DefaultNetCoreSomaxconn           int `json:"default_net_core_somaxconn"`
	DefaultNetIpv4TcpMaxSynBacklog    int `json:"default_net_ipv4_tcp_max_syn_backlog"`
	DefaultNetIpv4ConfAllRpFilter     int `json:"default_net_ipv4_conf_all_rp_filter"`
	DefaultNetIpv4ConfDefaultRpFilter int `json:"default_net_ipv4_conf_default_rp_filter"` // ФИКС: Добавили упущенное

	// --- Боевые изменяемые параметры администратора через TUI ---
	ArmorNetIpv4TcpSyncookies       int `json:"armor_net_ipv4_tcp_syncookies"`
	ArmorNetIpv4TcpFinTimeout       int `json:"armor_net_ipv4_tcp_fin_timeout"`
	ArmorNetIpv4TcpSynRetries       int `json:"armor_net_ipv4_tcp_syn_retries"`
	ArmorNetIpv4TcpSynackRetries    int `json:"armor_net_ipv4_tcp_synack_retries"`
	ArmorNetCoreSomaxconn           int `json:"armor_net_core_somaxconn"`
	ArmorNetIpv4TcpMaxSynBacklog    int `json:"armor_net_ipv4_tcp_max_syn_backlog"`
	ArmorNetIpv4ConfAllRpFilter     int `json:"armor_net_ipv4_conf_all_rp_filter"`
	ArmorNetIpv4ConfDefaultRpFilter int `json:"armor_net_ipv4_conf_default_rp_filter"` // ФИКС: Добавили упущенное
}

// Config описывает центральную структуру конфигурации киберразведки Amber Guard Lab.
type Config struct {
	Lang string `json:"lang"` // Текущий язык интерфейса панели (ru/en)

	// --- Абсолютные пути к лог-файлам и журналам системы ---
	AuthLog          string `json:"auth_log"`
	BtmpLog          string `json:"btmp_log"`
	F2bLog           string `json:"f2b_log"`
	UfwLog           string `json:"ufw_log"`
	NginxRadioLog    string `json:"nginx_radio_log"`
	ApacheLog        string `json:"apache_log"`
	TarpitConnLog    string `json:"tarpit_conn_log"`
	TarpitRptLog     string `json:"tarpit_rpt_log"`
	AglLogarchiveDir string `json:"agl_logarchive_dir"` // РЕБРЕНДИНГ: Изменено под аббревиатуру agl_

	// --- Директории назначения для резервного копирования ---
	BackupRootDir string `json:"backup_root_dir"`

	// --- Инструменты криминалистики и гео-аналитики ---
	GeoTool string `json:"geo_tool"`

	// --- Параметры Модуля «Дегтярная яма» (TARPIT) ---
	StandardSshPort int `json:"standard_ssh_port"` // Всегда 22
	RealSshPort     int `json:"real_ssh_port"`     // Кастомный порт после кастлинга
	DelayStep       int `json:"delay_step"`
	DelayVariance   int `json:"delay_variance"`

	// --- Системные лимиты сокетов Дегтярной Ямы ---
	TarpitBacklog int `json:"tarpit_backlog"`
	TarpitMaxConn int `json:"tarpit_max_conn"`

	// --- Подсистема глубокого тюнинга ядра CORE_SETTINGS ---
	SysctlCore SysctlCoreConfig `json:"sysctl_core_settings"`

	// --- M2 (v8.1.2): Интервал простоя администратора для испепеления ---
	// Таймаут в минутах, после которого анти-локаут админа испепеляется.
	// Дефолт: 10 мин. Настраивается через SETTINGS.
	AdminIdleTimeout int `json:"admin_idle_timeout"`

	// --- M3 (v8.1.2): Канал спасения «веб-стук» ---
	// Тумблер включения/выключения канала спасения через веб-стук.
	// Дефолт: true. Настраивается через SETTINGS.
	WebRescueEnabled bool `json:"web_rescue_enabled"`

	// --- M3b (v8.1.2): Автономный HTTP-сервер веб-стука ---
	// Порт, на котором слушает лёгкий HTTP-сервер веб-стука.
	// Дефолт: 55111. Настраивается через SETTINGS.
	WebRescuePort int `json:"web_rescue_port"`

	// --- Веб-сервер спасения (физический слушатель) ---
	// PURPOSE: Управляет физическим жизненным циклом HTTP-сервера на WebRescuePort.
	// CONTRACT: Независим от рокировки: сервер может жить для будущей TUI-панели.
	//           Если false — сокет закрыт, вотчер не стартует вообще.
	// SIDE-EFFECTS: Мутация этого поля триггерит syncWebRescueServer() в вотчере.
	WebServerEnabled bool `json:"web_server_enabled"`

	// Пароль псевдо-2FA для веб-стука.
	// ВАЖНО: каждый админ должен сменить пароль на уникальный!
	WebRescuePassword string `json:"web_rescue_password,omitempty"`

	// --- M3g (v8.1.3): ХЭШ пароля веб-стука (plaintext изъят из конфига) ---
	// SHA-256 (hex) пароля псевдо-2FA. Без соли — осознанный компромисс
	// (первичная защита — TLS-шлюз; хэш прикрывает утечки backup-копий).
	WebRescuePasswordHash string `json:"web_rescue_password_hash"`

	// --- M4a (v8.3.0): Префикс для канала спасения (веб-стук) ---
	// Случайная hex-строка (12 символов), генерируемая при первом старте.
	// Пример: https://ip:55111/a3f9c2e1b4d0/rescue
	WebKnockHashPrefix string `json:"web_knock_hash_prefix,omitempty"`

	// --- M4b (v8.3.0): Префикс для будущего веб-интерфейса ---
	// Отдельный префикс для панели управления (пока заглушка).
	// Пример: https://ip:55111/x7k2m9p4q1w8/panel
	WebInterfaceHashPrefix string `json:"web_interface_hash_prefix,omitempty"`

	// --- M5 (v8.4.0): Автообновления сервера ---
	// Тумблер включения суточного oneshot-обновления через apt-get dist-upgrade.
	// Дефолт: false. Настраивается через SETTINGS.
	// Юниты agld_autoupdate.service/timer декларативно создаются/удаляются этим тумблером.
	AutoUpdateEnabled bool `json:"auto_update_enabled"`

	// Тумблер автоматической перезагрузки после обновлений ядра.
	// Активен ТОЛЬКО если AutoUpdateEnabled=true И /var/run/reboot-required существует.
	// Дефолт: false (админ ребутает руками). Настраивается через SETTINGS.
	AutoUpdateReboot bool `json:"auto_update_reboot"`

	// --- v8.5.0: Уровни защиты SSH-доступа и веб-панель ---
	// PURPOSE: Три независимых тумблера для гибкого управления доступом.
	// CONTRACT:
	//   - SshAllowInFromIp (опция A): whitelist IP админа для скрытого порта SSH.
	//   - SshIpWebKnock (опция B): веб-стук с испепелением временных дверей. Требует A.
	//   - WebHttpsPanelActive (опция C): доступность веб-панели на порту 55111.
	// SIDE-EFFECTS: Мутация триггерит синхронизацию через service.SyncRescueChannelState().

	// --- v8.5.0: Уровни защиты SSH-доступа (защита в глубину) ---
	// Уровень 2A: постоянный whitelist IP админа на скрытом порту.
	// Сигнатура двери: Master Admin Anti-Lockout; испепелением по таймауту не трогается.
	// Дефолт: false (fail-closed). Настраивается через SETTINGS.
	SshAllowInFromIp bool `json:"ssh_allow_in_from_ip"`

	// Уровень 2B: временный whitelist через веб-стук с испепелением по таймауту.
	// Сигнатура двери: Private SSH Invisible Port НОМЕР (цель испепеления).
	// CONTRACT: требует SshAllowInFromIp=true и WebServerEnabled=true (иерархия D2).
	// Дефолт: false (fail-closed). Настраивается через SETTINGS.
	SshIpWebKnock bool `json:"ssh_ip_web_knock"`

	// --- v8.5.0: Веб-панель (уровень 3) ---
	// Активирует доступность маршрута /panel на порту веб-сервера.
	// CONTRACT: требует WebServerEnabled; на SSH-доступ не влияет.
	// Дефолт: false (fail-closed). Настраивается через SETTINGS.
	WebHttpsPanelActive bool `json:"web_https_panel_active"`

	// --- v8.6.2: Аналитика - автозагрузка истории ---
	// Тумблер автоматической загрузки истории из tarpit_report.log при старте сервиса.
	// Дефолт: false (fail-closed). Настраивается через SETTINGS.
	// При true: после запуска инжеста читается весь лог и обновляются профили акторов.
	// CONTRACT: требует, чтобы GlobalStore был инициализирован (выполняется в analytics.Start).
	AnalyticsAutoRebuild bool `json:"analytics_auto_rebuild"`
}

// C — глобальный синглтон настроек, доступный из любой точки программы.
// v8.2.0 (2.3): C — УКАЗАТЕЛЬ на неизменяемый снепшот. LoadConfig собирает
// новую структуру целиком и публикует её одним присваиванием указателя:
// читатель видит либо старый снепшот целиком, либо новый целиком —
// без «рваных» полей при массовой перезагрузке. Все существующие
// читатели `config.C.X` работают без изменений (авто-разыменование).
var C *Config = &Config{}

// configMu — сериализует транзакции LoadConfig/SaveConfig (v8.2.0, 2.3).
var configMu sync.Mutex

// PasswordJustMigrated — одноразовый флаг миграции plaintext→хэш (логирует вотчер).
var PasswordJustMigrated bool

// CheckFileExists — хелпер для проверки ленивых маркеров активности Брони
func CheckFileExists(filePath string) (bool, error) {
	_, err := os.Stat(filePath)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// CheckArmorRouteStub заглушка-имитатор для сохранения совместимости компиляции tui/view_armor.go
// func (c *Config) CheckArmorRouteStub(uuid string) error {
// 	return nil
// }

// LoadConfig выполняет детерминированное чтение JSON и инициализацию рантайм-контуров.
func LoadConfig(filePath string) error {
	configMu.Lock()
	defer configMu.Unlock()

	// v8.2.0 (2.3): собираем НОВЫЙ снепшот; живой C не трогаем до валидации.
	local := &Config{}

	// ФИКС: Мягкое наполнение базовыми рабочими дефолтами до десериализации JSON.
	// Это защищает структуру от появления "нулей", если старый файл конфигурации обновляется.
	// v8.3.0 (6.3): дефолт интерфейса — английский (глобальный стандарт ИБ-инструментов).
	local.Lang = "en"
	local.StandardSshPort = 22
	local.RealSshPort = 2244
	local.DelayStep = 13
	local.DelayVariance = 8
	local.TarpitMaxConn = 100
	local.TarpitBacklog = 1024
	local.AdminIdleTimeout = 10        // M2: дефолт 10 минут
	local.WebRescueEnabled = true      // M3: канал спасения включён по умолчанию
	local.WebServerEnabled = true      // v8.4.7: физический HTTP-слушатель включён по умолчанию
	local.WebRescuePort = 55111        // M3b: порт автономного HTTP-сервера веб-стука
	local.SshAllowInFromIp = false     // v8.5.0: fail-closed — защита только по явной декларации
	local.SshIpWebKnock = false        // v8.5.0: fail-closed
	local.WebHttpsPanelActive = false  // v8.5.0: fail-closed
	local.AutoUpdateEnabled = false    // M5: дефолт — автообновления ВЫКЛЮЧЕНЫ
	local.AutoUpdateReboot = false     // M5: дефолт — ребут только руками
	local.SshAllowInFromIp = true      // v8.5.0: опция A включена по умолчанию (whitelist IP админа)
	local.SshIpWebKnock = true         // v8.5.0: опция B включена по умолчанию (веб-стук с испепелением)
	local.WebHttpsPanelActive = false  // v8.5.0: опция C выключена по умолчанию (панель недоступна)
	local.AnalyticsAutoRebuild = false // v8.6.2: fail-closed - автозагрузка выключена
	local.GeoTool = "geoiplookup"
	local.AglLogarchiveDir = DirLog // БЛОК 4.4: Используем константу из paths.go
	local.BackupRootDir = "/backup"

	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf(i18n.T.ConfigErrOpenCenter, err)
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	// ← Decode в local, не в &C
	if err := decoder.Decode(local); err != nil {
		return fmt.Errorf(i18n.T.ConfigErrParseStruct, err)
	}

	// БЛОК 3.7: Жёсткие фолбэки для критичных параметров после парсинга.
	// Гарантируем, что даже при повреждённом или неполном конфиге
	// система не упадёт и не получит нулевые/отрицательные значения.
	if local.WebRescuePort <= 0 {
		local.WebRescuePort = 55111
	}
	if local.AdminIdleTimeout <= 0 {
		local.AdminIdleTimeout = 10
	}
	if local.TarpitMaxConn <= 0 {
		local.TarpitMaxConn = 100
	}
	if local.DelayStep <= 0 {
		local.DelayStep = 13
	}
	if local.DelayVariance < 0 {
		local.DelayVariance = 3
	}

	// БЛОК 4.4: централизованные фолбэки путей логов дегтярной ямы (паттерн 3.7).
	// Демон пишет логи по путям из конфига, TUI обязан читать те же файлы.
	// Старые конфиги могут не иметь ключей — откатываемся на заводские
	// константы из paths.go вместо пустой строки.
	if local.TarpitConnLog == "" {
		local.TarpitConnLog = FileTarpitConnLog
	}
	if local.TarpitRptLog == "" {
		local.TarpitRptLog = FileTarpitRptLog
	}

	// --- M3g (v8.1.3): миграция plaintext → хэш с затиранием plaintext ---
	if local.WebRescuePassword != "" && local.WebRescuePasswordHash == "" {
		sum := sha256.Sum256([]byte(local.WebRescuePassword))
		local.WebRescuePasswordHash = hex.EncodeToString(sum[:])
		local.WebRescuePassword = ""
		PasswordJustMigrated = true
		// ВАЖНО: используем saveConfigLocked, так как local не является глобальным state
		if err := saveConfigLocked(filePath, local); err != nil {
			// Добавление обработки ошибки, если сохранение после миграции не удалось
			return fmt.Errorf(i18n.T.ConfigErrSaveAfterMigration, err)
		}
	}

	// v8.4.7 (Fail-Closed): дефолтного пароля НЕТ. Свежий конфиг = канал выключен,
	// пока админ осознанно не задаст собственный пароль через SETTINGS.
	// Без пароля оба тумблера принудительно гасятся: сокет не открывается, вотчер не стартует.
	if local.WebRescuePasswordHash == "" && local.WebRescuePassword == "" {
		local.WebRescueEnabled = false
		local.WebServerEnabled = false
	}
	// v8.5.0 (D2): иерархия защиты в глубину. Стук (B) не работает без постоянной
	// двери (A); B и панель не работают без физического слушателя.
	// Выключение A каскадом гасит B — опция A полноправно управляет опцией B.
	if !local.SshAllowInFromIp {
		local.SshIpWebKnock = false
	}
	if !local.WebServerEnabled {
		local.SshIpWebKnock = false
		local.WebHttpsPanelActive = false
	}
	// v8.2.0 (1.2): есть хэш — plaintext в ОЗУ обязан быть пустым.
	if local.WebRescuePasswordHash != "" {
		local.WebRescuePassword = ""
	}

	// --- M4a & M4b (v8.3.0): Генерация префиксов при первом запуске ---
	prefixGenerated := false

	if local.WebKnockHashPrefix == "" {
		local.WebKnockHashPrefix = generateRandomPrefix(6) // 6 байт = 12 hex-символов
		prefixGenerated = true
	}

	if local.WebInterfaceHashPrefix == "" {
		local.WebInterfaceHashPrefix = generateRandomPrefix(6)
		prefixGenerated = true
	}

	// Сохраняем конфиг на диск, если были сгенерированы новые префиксы
	if prefixGenerated {
		if err := saveConfigLocked(filePath, local); err != nil {
			return fmt.Errorf(i18n.T.ConfigErrSaveAfterPrefixGen, err)
		}
	}

	// --- Автоматическая стабилизация окружения на сервере ---
	if err := os.MkdirAll(DirLib, 0755); err != nil {
		return fmt.Errorf(i18n.T.ConfigErrDiskGeometry, DirLib, err)
	}

	// Папка в ОЗУ /var/run/amberguard должна быть доступна (обычно создается systemd, но страхуем рантайм)
	_ = os.MkdirAll(DirRun, 0750)

	// --- Защитная валидация базовых параметров ---
	if local.Lang != "ru" && local.Lang != "en" {
		local.Lang = "en"
	}

	if local.RealSshPort <= 0 || local.RealSshPort > 65535 {
		return fmt.Errorf(i18n.T.ConfigErrInvalidSshPort, local.RealSshPort)
	}

	// ФИКС: Вместо критического падения — применяем мягкое восстановление лимита
	if local.TarpitMaxConn <= 0 {
		local.TarpitMaxConn = 100
	}

	// =========================================================================
	//   ЖЕСТКИЙ ИБ-ВАЛИДАТОР ГРАНИЦ И ЗАЩИТА ЯДРА ОТ ОКИРПИЧИВАНИЯ (FAIL-SAFE)
	// =========================================================================
	s := &local.SysctlCore

	// 1. Синхронизируем заводские дефолты чистого ядра Linux (Контур отката)
	s.DefaultNetIpv4TcpSyncookies = 1
	s.DefaultNetIpv4TcpFinTimeout = 60
	s.DefaultNetIpv4TcpSynRetries = 6
	s.DefaultNetIpv4TcpSynackRetries = 5
	s.DefaultNetCoreSomaxconn = 128
	s.DefaultNetIpv4TcpMaxSynBacklog = 128
	s.DefaultNetIpv4ConfAllRpFilter = 0
	s.DefaultNetIpv4ConfDefaultRpFilter = 0

	// 2. Бронированная проверка боевых параметров администратора (armor_)
	// При выходе за границы откатываемся к БЕЗОПАСНЫМ значениям Armor (под 1 ГБ RAM),
	// а не к Default, чтобы сохранить уровень защиты даже при ошибке ввода.

	if s.ArmorNetIpv4TcpSyncookies != 0 && s.ArmorNetIpv4TcpSyncookies != 1 {
		s.ArmorNetIpv4TcpSyncookies = 1
	}

	// Границы таймаутов FIN-WAIT-2: строго от 10 до 60 секунд
	if s.ArmorNetIpv4TcpFinTimeout < 10 || s.ArmorNetIpv4TcpFinTimeout > 60 {
		s.ArmorNetIpv4TcpFinTimeout = 15 // Фолбэк на безопасный Armor
	}

	// Повторы SYN пакетов: от 1 до 5 попыток
	if s.ArmorNetIpv4TcpSynRetries < 1 || s.ArmorNetIpv4TcpSynRetries > 5 {
		s.ArmorNetIpv4TcpSynRetries = 2
	}

	// Повторы SYN-ACK ответов ядра: от 1 до 5 попыток
	if s.ArmorNetIpv4TcpSynackRetries < 1 || s.ArmorNetIpv4TcpSynackRetries > 5 {
		s.ArmorNetIpv4TcpSynackRetries = 2
	}

	// Системные буферы очередей: от 128 до 32768 соединений
	if s.ArmorNetCoreSomaxconn < 128 || s.ArmorNetCoreSomaxconn > 32768 {
		s.ArmorNetCoreSomaxconn = 2048 // Фолбэк на безопасный лимит под 1 ГБ
	}
	if s.ArmorNetIpv4TcpMaxSynBacklog < 128 || s.ArmorNetIpv4TcpMaxSynBacklog > 32768 {
		s.ArmorNetIpv4TcpMaxSynBacklog = 4096
	}

	// Обратный фильтр маршрутов rp_filter: строго 0, 1 или 2
	if s.ArmorNetIpv4ConfAllRpFilter < 0 || s.ArmorNetIpv4ConfAllRpFilter > 2 {
		s.ArmorNetIpv4ConfAllRpFilter = 1
	}

	if s.ArmorNetIpv4ConfDefaultRpFilter < 0 || s.ArmorNetIpv4ConfDefaultRpFilter > 2 {
		s.ArmorNetIpv4ConfDefaultRpFilter = 1
	}

	// Публикация снепшота: единое присвоение указателя.
	C = local

	return nil
}

// SaveConfig сохраняет текущее состояние синглтона в файл config.json.
// v8.2.0 (2.3): транзакция под configMu; маршалит ТЕКУЩИЙ снепшот C.
func SaveConfig(filePath string) error {
	configMu.Lock()
	defer configMu.Unlock()
	return saveConfigLocked(filePath, C)
}

// saveConfigLocked — внутренняя атомарная запись БЕЗ взятия замка.
// v8.2.0 (2.3): принимает снепшот явно:
//   - SaveConfig передаёт глобальный C;
//   - LoadConfig (миграция M3g) передаёт local ДО публикации —
//     иначе на диск уехал бы старый снепшот.
//
// Вызов из-под configMu безопасен (mutex не реентерабелен — не берём повторно).
func saveConfigLocked(filePath string, cfg *Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf(i18n.T.ConfigErrSerialization, err)
	}
	tmpPath := filePath + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf(i18n.T.ConfigErrCreateTempFile, err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf(i18n.T.ConfigErrWriteConfig, err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf(i18n.T.ConfigErrSyncConfig, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf(i18n.T.ConfigErrCloseTempFile, err)
	}
	if err := os.Rename(tmpPath, filePath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf(i18n.T.ConfigErrRenameConfig, err)
	}
	return nil
}

// Update — транзакция мутации снепшота конфига (v8.2.2, волна остаточной гонки 2.3).
// Под configMu: клонирует текущий снепшот, применяет fn к клону, публикует клон
// единым присваиванием и атомарно сохраняет на диск.
//
// PURPOSE: устраняет in-place мутации опубликованного снепшота
// («s := &config.C.SysctlCore» + присваивания полей) и потерянные обновления
// при хот-релоаде вотчера, который перетирает C.
//
// CONTRACT:
//   - fn НЕ должна вызывать LoadConfig/SaveConfig/Update — sync.Mutex не
//     реентерабелен, будет deadlock. fn только мутирует поля клона, быстро, без I/O.
//   - После публикации снепшот неизменяем: читатели config.C.X работают без
//     изменений и всегда видят согласованное состояние.
//   - Клон поверхностный (cloned := *C): корректен, пока Config плоский
//     (примитивы, строки, SysctlCoreConfig без слайсов/мап). Появятся в Config
//     слайсы/мапы — заменить на глубокое клонирование.
//
// SIDE-EFFECTS:
//   - Публикует новый снепшот в C (единое присваивание указателя).
//   - Пишет config.json на диск через saveConfigLocked (без повторного лока).
func Update(filePath string, fn func(c *Config)) error {
	configMu.Lock()
	defer configMu.Unlock()

	// Защита от nil: C инициализирован при объявлении, но страхуемся на случай
	// вызова Update до первого LoadConfig.
	if C == nil {
		C = &Config{}
	}

	// Клонируем текущий снепшот: читатели продолжают видеть старый C до публикации.
	cloned := *C

	// Применяем мутации к клону, а не к опубликованному снепшоту.
	fn(&cloned)

	// Публикация единым присваиванием: читатели атомарно переключаются на новый снепшот.
	C = &cloned

	// Персистентность на диск без повторного взятия лока (saveConfigLocked не лочит).
	return saveConfigLocked(filePath, C)
}

// GetSysctlParamNameByIndex возвращает строковое имя параметра ядра по индексу строки TUI-меню

// GetSysctlParamNameByIndex возвращает строковое имя параметра ядра по индексу строки TUI-меню
func GetSysctlParamNameByIndex(index int) string {
	names := []string{
		"net.ipv4.tcp_syncookies",
		"net.ipv4.tcp_fin_timeout",
		"net.ipv4.tcp_syn_retries",
		"net.ipv4.tcp_synack_retries",
		"net.core.somaxconn",
		"net.ipv4.tcp_max_syn_backlog",
		"net.ipv4.conf.all.rp_filter",
		"net.ipv4.conf.default.rp_filter", // ФИКС: Отразили в меню TUI, чтобы индексы не съехали при отрисовке параметров ядра
	}
	if index >= 0 && index < len(names) {
		return names[index]
	}
	return "unknown_param"
}

// generateRandomPrefix создаёт криптографически стойкую случайную hex-строку.
// lengthBytes = 6 даст 12-символьный префикс (например, "a3f9c2e1b4d0").
func generateRandomPrefix(lengthBytes int) string {
	b := make([]byte, lengthBytes)
	if _, err := rand.Read(b); err != nil {
		// Крайне маловероятный фолбэк, но удовлетворяет компилятор
		return "fallback123456"
	}
	return hex.EncodeToString(b)
}
