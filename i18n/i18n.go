// i18n/i18n.go
package i18n

import "fmt"

// SysctlHelpDoc представляет локализованные данные для одного параметра ядра
type SysctlHelpDoc struct {
	ParamName   string
	DefaultVal  string
	MinVal      int
	MaxVal      int
	Description string
	OnAttack    string
	Risks       string
}

// Translation описывает жесткий каркас всех текстовых строк экосистемы.
// Объединенная структура, включающая Core Settings.
type Translation struct {
	// --- Главное меню и Шапка панели (Core) ---
	MenuTitle   string
	BtnSettings string
	BtnAntiDdos string
	BtnTarpit   string
	BtnExit     string

	// --- Статусы рубежей защиты (Perimeter States) ---
	StatusActive string
	StatusDead   string

	// --- Модуль Резервного Копирования (Backup Module) ---
	BackupStart        string
	BackupSuccess      string
	BackupErrorNoDirs  string
	BackupErrorTar     string
	BackupProgressItem string

	// --- Модуль Форензики и Анализа Периметра (Forensics Module) ---

	// --- Интерактивная карта угроз экосистемы сервера ---
	MapNotFoundLabel string

	// --- Anti-DDOS - SSH-броня ---
	ArmorSshLimitOn   string
	ArmorSshLimitOff  string
	ArmorSshAdminOk   string
	ArmorSshPortFound string

	// --- Рокировка портов ---
	ProtoMovePortStart      string
	ProtoMovePortSuccess    string
	ProtoRestorePortStart   string
	ProtoRestorePortSuccess string

	// --- Оптимизация сетевого стека (PROTO_SYSCTL) ---
	ProtoSysctlOn          string
	ProtoSysctlOff         string
	ProtoSysctlCheckActive string
	ProtoSysctlCheckDead   string

	// --- Дегтярная яма ---
	ProtoTarpitOn        string
	ProtoTarpitOff       string
	ProtoTarpitErrPort22 string
	ProtoTarpitWorkerIn  string

	// --- Строки подсказок ---
	ArmorHintSysctl   string
	ArmorHintLimitSsh string
	ArmorHintLimitXui string
	ArmorHintOn       string
	ArmorHintOff      string
	ArmorHintBack     string

	// --- SETTINGS (глобальные настройки) ---
	SettingsSaved     string
	SettingsErrDigits string
	SettingsErrJitter string

	// --- Строки подсказок Дегтярной Ямы ---
	TarpitHintOn       string
	TarpitHintOff      string
	TarpitHintLive     string
	TarpitHintReport   string
	TarpitHintBack     string
	TarpitHintSettings string
	TarpitHintProtocol string

	// --- Рокировка (MOVE_PORT) ---
	TarpitHintMove string

	// =======================================================
	// --- БЛОК СИСТЕМНЫХ НАСТРОЕК (CORE_SETTINGS) ---
	// =======================================================
	CoreTitleText       string
	CoreHeaderDesc      string
	CoreHeaderWarning   string
	CoreListTitle       string
	CoreResetWarning    string
	CoreBackText        string
	CoreInfoBoxTitle    string
	CoreSysConsoleTitle string
	CoreSysConsoleBody  string
	CoreHelpSelectParam string

	// Слайс документов справки на соответствующем языке
	SysctlDocs []SysctlHelpDoc
}

// ============================================================================
// РУССКИЙ ПАКЕТ ЛОКАЛИЗАЦИИ
// ============================================================================
var RuTranslation = Translation{
	// --- Core Strings (from i18n.go) ---
	MenuTitle:               "ГЛАВНОЕ МЕНЮ СЕРВЕРА AMBER GUARD LAB", // РЕБРЕНДИНГ
	BtnSettings:             "SETTINGS - Глобальные настройки модулей системы",
	BtnAntiDdos:             "ANTIDDOS - Менеджер сетевой брони (Защита от штормов)",
	BtnTarpit:               "TARPIT   - Алгоритм «Дегтярная Яма» для ботнетов",
	BtnExit:                 "EXIT     - Выход из панели управления",
	StatusActive:            "Активен",
	StatusDead:              "Отключен",
	BackupStart:             "Инициирован асинхронный снапшот системы...",
	BackupSuccess:           "Резервное копирование завершено. Все нововведения успешно заархивированы.",
	BackupErrorNoDirs:       "критическая ошибка: целевые папки для бэкапа не найдены на диске",
	BackupErrorTar:          "не удалось создать файл снапшота",
	BackupProgressItem:      "Упаковка экосистемы: [%d%%] -> Добавление в архив: %s",
	MapNotFoundLabel:        "Не найден",
	ArmorSshLimitOn:         "[БРОНЯ]: Динамический лимит соединений SSH успешно АКТИВИРОВАН.",
	ArmorSshLimitOff:        "[БРОНЯ]: Ограничения порта SSH сняты. Программа вернулась в штатный режим.",
	ArmorSshAdminOk:         "[ПРОТЕКЦИЯ]: IP администратора %s успешно занесен в белый список (Anti-Lockout).",
	ArmorSshPortFound:       "[КОНФИГ]: В системе обнаружен рабочий порт OpenSSH: %s",
	ProtoMovePortStart:      "[ПРОТОКОЛ]: Переношу реальный OpenSSH на скрытый порт %d...\nИзолирую внешний контур порта 22 заглушкой REJECT.",
	ProtoMovePortSuccess:    "[УСПЕХ]: Рокировка портов завершена.\nЛегитимный OpenSSH успешно перенесен на скрытый порт: %d\nВаш определенный IP доступа: %s\nКонтур порта 22 изолирован заглушкой REJECT.",
	ProtoRestorePortStart:   "[ПРОТОКОЛ]: Возвращаю легитимный SSH на стандартный порт 22...\nСбрасываю правила доступов.",
	ProtoRestorePortSuccess: "[УСПЕХ]: Оригинальный контур восстановлен.\nЛегитимный OpenSSH возвращен на стандартный порт 22.\nВременные доступы скрытого порта полностью аннулированы.",
	ProtoSysctlOn:           "[БРОНЯ ЯДРА]: Оптимизация сетевого стека под высокие нагрузки Anti-DDoS АКТИВИРОВАНА.",
	ProtoSysctlOff:          "[БРОНЯ ЯДРА]: Параметры сетевого стека возвращены к стандартным дефолтам Linux.",
	ProtoSysctlCheckActive:  "[СТАТУС]: Броня сетевого стека sysctl АКТИВНА.",
	ProtoSysctlCheckDead:    "[СТАТУС]: Тюнинг ядра отключен. Система в стандартном режиме.",
	ProtoTarpitOn:           "[КАПКАН]: Автономная Дегтярная Яма на порту 22 успешно ЗАПУЩЕНА. Шлюзы UFW открыты для ботов.",
	ProtoTarpitOff:          "[КАПКАН]: Дегтярная Яма демонтирована. Контур порта 22 возвращен в безопасное состояние.",
	ProtoTarpitErrPort22:    "[БЛОКИРОВКА]: Запуск ямы сорван! Легитимный OpenSSH всё еще висит на порту 22 во избежание локаута.",
	ProtoTarpitWorkerIn:     "[СИГНАЛ]: Сканер из %s (%s) успешно захвачен в капкан. План истощения: %d сек.",
	ArmorHintSysctl:         "[ИНФО]: Проверка и тюнинг параметров ядра (/proc/sys/net). Защита от SYN-флуда и подмены IP-адресов (Spoofing).",
	ArmorHintLimitSsh:       "[ИНФО]: Контроль частоты подключений к OpenSSH (UFW limit). Автономный Anti-Lockout защищает ваш IP.",
	ArmorHintLimitXui:       "[ИНФО]: Включение Rate Limit на порт веб-панели 3X-UI из конфигурации для фильтрации DDoS-штормов.",
	ArmorHintOn:             "[ДЕЙСТВИЕ]: Принудительная одновременная активация защиты на всех доступных сетевых рубежах сервера.",
	ArmorHintOff:            "[ДЕЙСТВИЕ]: Сброс всех правил лимитирования UFW и возврат параметров сетевого стека Linux к заводским дефолтам.",
	ArmorHintBack:           "[НАВИГАЦИЯ]: Закрыть диспетчер сетевой брони и вернуться на главный экран выбора подсистем.",
	SettingsSaved:           "[УСПЕХ]: Конфигурация config.json обновлена! Изменения применятся при следующем запуске модулей.",
	SettingsErrDigits:       "[КРИТИЧЕСКАЯ ОШИБКА]: Ошибка валидации! Разрешено использовать только числовые значения.",
	SettingsErrJitter:       "[ОШИБКА БЕЗОПАСНОСТИ]: Отказ! Вариативность джиттера не может быть больше или равна базовому интервалу.",
	TarpitHintOn:            "[ДЕЙСТВИЕ]: Активация ловушки на порту 22.\nUFW откроет шлюз Temporary Tarpit Pit наружу для ботов.",
	TarpitHintOff:           "[ДЕЙСТВИЕ]: Демонтаж службы из systemd. Включение заглушки Fast Reject, если активен скрытый порт.",
	TarpitHintLive:          "[ФОРЕНЗИКА]: Просмотр файла tarpit_report.log в реальном времени.",
	TarpitHintReport:        "[SIEM ОТЧЕТ]: Глубокий анализ tarpit_report.log. Расчет хронологии истощения и досье сканеров.",
	TarpitHintBack:          "[НАВИГАЦИЯ]: Закрыть диспетчер Дегтярной Ямы и вернуться в Главное меню сервера.",
	TarpitHintSettings:      "[НАСТРОЙКИ]: Перейти в диспетчер редактирования тайм-аутов, джиттера и сетевых конфигов",
	TarpitHintProtocol:      "[СПРАВКА]: Вывод интерактивной ИБ-справки и документации протокола Deception Tarpit Lab",
	// --- Core Settings (from i18n_core_tune.go) ---
	CoreTitleText:       " [ Раздел Конфигурации Системы: CORE_SETTINGS ] ",
	CoreHeaderDesc:      " Модификация интервалов ядра и параметров Анти-DDoS защиты │ [%s](fg:yellow) │ [%s](fg:yellow)",
	CoreHeaderWarning:   " ПРЕДУПРЕЖДЕНИЕ: Прежде чем изменить параметр ядра, убедитесь что вы осведомлены о последствиях!",
	CoreListTitle:       " Выберите параметр для изменения или просмотра справки: ",
	CoreResetWarning:    "\n [ВНИМАНИЕ]: Нажатие ENTER сбросит пользовательские значения 'armor_' в config.json\n к эталонным оборонительным константам комплекса Amber Guard Lab (somaxconn=4096, fin_timeout=15 и т.д.).\n Текущие настройки будут полностью перезаписаны.", // РЕБРЕНДИНГ
	CoreBackText:        "\n Возврат в предыдущий интерфейс управления брандмауэром периметра.",
	CoreInfoBoxTitle:    " Интерактивный Справочник Параметров Ядра Linux ",
	CoreSysConsoleTitle: " СИСТЕМНАЯ КОНСОЛЬ НАВИГАЦИИ ",
	CoreSysConsoleBody:  " [ НАВИГАЦИЯ ]: Стрелки ↑/↓ │ [ ИЗМЕНЕНИЕ ]: Нажмите ENTER, чтобы открыть параметр\n [СБРОС]: Выберите строку RESET для отката к эталонам Amber Guard Lab\n [ ВЫХОД ]: Выберите строку BACK или нажмите ESC для возврата", // РЕБРЕНДИНГ
	CoreHelpSelectParam: "\n Выберите интересующий параметр ядра из верхнего списка для просмотра документации.",
	// --- Structured Data: SysctlDocs ---
	SysctlDocs: []SysctlHelpDoc{
		{
			ParamName:  "net.ipv4.tcp_syncookies",
			DefaultVal: "1 (Включено)",
			MinVal:     0, MaxVal: 1,
			Description: "Защита от SYN-флуда. Активирует криптографические cookie в номерах очередей TCP.",
			OnAttack:    "Держите строго включенным (1). Позволяет серверу не выделять память под фиктивные сессии.",
			Risks:       "Отключение (0) приведет к мгновенному падению сетевого стека при простейшем SYN-штурме.",
		},
		{
			ParamName:  "net.ipv4.tcp_fin_timeout",
			DefaultVal: "15 сек",
			MinVal:     10, MaxVal: 60,
			Description: "Время удержания сокета в состоянии FIN-WAIT-2 после закрытия его сервером.",
			OnAttack:    "Снижайте до 15 секунд для максимально быстрой очистки таблиц соединений ядра.",
			Risks:       "Меньше 10 сек — пойдут массовые обрывы легитимных медленных TLS/SSL рукопожатий.",
		},
		{
			ParamName:  "net.ipv4.tcp_syn_retries",
			DefaultVal: "2",
			MinVal:     1, MaxVal: 5,
			Description: "Количество попыток отправки первичных SYN-пакетов при инициации исходящих сессий.",
			OnAttack:    "Снижайте до 2 или 1, чтобы зависшие процессы не плодили внутренние дедлоки сокетов.",
			Risks:       "Меньше 1 — сервер не сможет связаться наружу через нестабильные каналы связи.",
		},
		{
			ParamName:  "net.ipv4.tcp_synack_retries",
			DefaultVal: "2",
			MinVal:     1, MaxVal: 5,
			Description: "Количество попыток ответа SYN-ACK клиенту, если от него нет финального ACK пакета.",
			OnAttack:    "Установите значение 2. Это защитит буферы сетевой карты от удержания ботнет-сессий.",
			Risks:       "Больше 5 — сервер захлебнется, бесконечно отвечая ботам с поддельными (Spoofing) IP.",
		},
		{
			ParamName:  "net.core.somaxconn",
			DefaultVal: "4096",
			MinVal:     128, MaxVal: 32768,
			Description: "Максимальный размер очереди полностью установленных сокетов, ожидающих вызова accept().",
			OnAttack:    "Увеличивайте до 4096 / 8192, чтобы легитимные клиенты не получали Connection Refused.",
			Risks:       "Меньше 128 — шквал ошибок у пользователей при любом штатном перезапуске Nginx или 3X-UI.",
		},
		{
			ParamName:  "net.ipv4.tcp_max_syn_backlog",
			DefaultVal: "4096",
			MinVal:     128, MaxVal: 32768,
			Description: "Максимальное число полуоткрытых TCP-соединений (клиент прислал SYN, но не ответил ACK).",
			OnAttack:    "Держите значение равным somaxconn (например, 4096), давая запас для SYN-cookies.",
			Risks:       "При значении меньше 128 защита от DDoS падает от пяти одновременно работающих ботов.",
		},
		{
			ParamName:  "net.ipv4.conf.all.rp_filter",
			DefaultVal: "1 (Строгий)",
			MinVal:     0, MaxVal: 2,
			Description: "Обратный фильтр маршрутов (Reverse Path Filtering) для защиты от подделки IP (IP Spoofing).",
			OnAttack:    "Строго режим 1. Ядро сбрасывает пакеты, пришедшие с интерфейсов, где их быть не должно.",
			Risks:       "Режим 1 при асимметричном роутинге (2 провайдера) может заблокировать чистый трафик. Ставьте 2.",
		},
	},
}

// ============================================================================
// АНГЛИЙСКИЙ ПАКЕТ ЛОКАЛИЗАЦИИ
// ============================================================================
var EnTranslation = Translation{
	// --- Core Strings (from i18n.go) ---
	MenuTitle:               "SERVER MAIN MENU AMBER GUARD LAB", // РЕБРЕНДИНГ
	BtnSettings:             "SETTINGS - Global System Module Settings",
	BtnAntiDdos:             "ANTIDDOS - Network Armor Manager (Storm Protection)",
	BtnTarpit:               "TARPIT   - Tarpit Algorithm for Botnet Paralysis",
	BtnExit:                 "EXIT     - Exit Control Panel",
	StatusActive:            "Active",
	StatusDead:              "Disabled",
	BackupStart:             "Asynchronous system snapshot initiated...",
	BackupSuccess:           "Backup completed successfully. All configurations archived.",
	BackupErrorNoDirs:       "critical error: target directories for backup not found on disk",
	BackupErrorTar:          "failed to create snapshot file",
	BackupProgressItem:      "Packing ecosystem: [%d%%] -> Adding to archive: %s",
	MapNotFoundLabel:        "Not Found",
	ArmorSshLimitOn:         "[ARMOR]: Dynamic SSH rate-limiting successfully ACTIVATED.",
	ArmorSshLimitOff:        "[ARMOR]: SSH port limits removed. System returned to normal mode.",
	ArmorSshAdminOk:         "[PROTECTION]: Administrator IP %s added to whitelist (Anti-Lockout).",
	ArmorSshPortFound:       "[CONFIG]: Active OpenSSH port detected in the system: %s",
	ProtoMovePortStart:      "[PROTOCOL]: Moving real OpenSSH to hidden port %d...\nIsolating external port 22 contour with REJECT.",
	ProtoMovePortSuccess:    "[SUCCESS]: Port castling completed.\nLegitimate OpenSSH successfully moved to hidden port: %d\nYour detected access IP: %s\nPort 22 contour isolated with REJECT.",
	ProtoRestorePortStart:   "[PROTOCOL]: Returning legitimate SSH to standard port 22...\nResetting access rules.",
	ProtoRestorePortSuccess: "[SUCCESS]: Original contour restored.\nLegitimate OpenSSH returned to standard port 22.\nTemporary hidden port accesses fully revoked.",
	ProtoSysctlOn:           "[KERNEL ARMOR]: Network stack optimization for high Anti-DDoS loads ACTIVATED.",
	ProtoSysctlOff:          "[KERNEL ARMOR]: Network stack parameters reverted to standard Linux defaults.",
	ProtoSysctlCheckActive:  "[STATUS]: Kernel network stack tuning is ACTIVE.",
	ProtoSysctlCheckDead:    "[STATUS]: Kernel tuning is disabled. System in standard mode.",
	ProtoTarpitOn:           "[TARPIT]: Autonomous SSH Tar-pit on port 22 successfully STARTED. UFW gateways opened.",
	ProtoTarpitOff:          "[TARPIT]: Tar-pit deactivated. Port 22 contour reverted to secure state.",
	ProtoTarpitErrPort22:    "[LOCKOUT BLOCK]: Tarpit startup aborted! Legitimate OpenSSH is still on port 22.",
	ProtoTarpitWorkerIn:     "[SIGNAL]: Scanner from %s (%s) successfully trapped. Exhaustion plan: %d sec.",
	ArmorHintSysctl:         "[INFO]: Kernel parameters tuning (/proc/sys/net). Protects against SYN-flood and IP-spoofing.",
	ArmorHintLimitSsh:       "[INFO]: Rate limiting for OpenSSH port (UFW limit). Autonomous Anti-Lockout whitelists your IP.",
	ArmorHintLimitXui:       "[INFO]: Activates UFW Rate Limit on 3X-UI panel port from configuration to filter out heavy DDoS storms.",
	ArmorHintOn:             "[ACTION]: Enforce full network armor. Activates protection on all perimeter boundaries simultaneously.",
	ArmorHintOff:            "[ACTION]: Purge all UFW rate-limit rules and revert Linux network stack to factory defaults.",
	ArmorHintBack:           "[NAVIGATION]: Close network armor dispatcher and return to the main subsystem registry screen.",
	SettingsSaved:           "[SUCCESS]: config.json registry updated! Changes will apply upon next module startup.",
	SettingsErrDigits:       "[CRITICAL ERROR]: Validation failed! Only numeric values are allowed in port fields.",
	SettingsErrJitter:       "[SECURITY ERROR]: Aborted! Delay variance cannot be greater than or equal to the base step.",
	TarpitHintOn:            "[ACTION]: Activate trap on port 22.\nUFW will open the 'Temporary Tarpit Pit' gateway for botnets.",
	TarpitHintOff:           "[ACTION]: Purge service from systemd. Enables 'Fast Reject' if the hidden SSH port is active.",
	TarpitHintLive:          "[FORENSICS]: Launch LIVE_BOX monitor. Streams tarpit_report.log in real-time.",
	TarpitHintReport:        "[SIEM REPORT]: Deep analysis of tarpit_report.log. Computes exhaustion timelines and bot tags.",
	TarpitHintBack:          "[NAVIGATION]: Close Tarpit Dispatcher and return to the main server subsystem screen.",
	TarpitHintSettings:      "[SETTINGS]: Access the timeout, jitter, and network configuration editor.",
	TarpitHintProtocol:      "[HELP]: Display interactive security help and documentation for the Tarpit Lab Deception protocol.",

	// --- Core Settings (from i18n_core_tune.go) ---
	CoreTitleText:       " [ System Configuration Section: CORE_SETTINGS ] ",
	CoreHeaderDesc:      " Modification of kernel intervals and Anti-DDoS protection parameters │ [%s](fg:yellow) │ [%s](fg:yellow)",
	CoreHeaderWarning:   " WARNING: Before changing kernel parameters, make sure you understand the consequences!",
	CoreListTitle:       " Select a parameter to change or view help: ",
	CoreResetWarning:    "\n [WARNING]: Pressing ENTER will reset user 'armor_' values in config.json\n to the factory defense constants of Amber Guard Lab (somaxconn=4096, fin_timeout=15, etc.).\n Current settings will be completely overwritten.", // РЕБРЕНДИНГ
	CoreBackText:        "\n Return to the previous perimeter firewall interface.",
	CoreInfoBoxTitle:    " Interactive Linux Kernel Parameters Reference ",
	CoreSysConsoleTitle: " SYSTEM NAVIGATION CONSOLE ",
	CoreSysConsoleBody:  " [NAVIGATION]: Arrows ↑/↓ │ [CHANGE]: Press ENTER to open parameter\n [RESET]: Select RESET row to rollback to Amber Guard Lab standards\n [EXIT]: Select BACK row or press ESC to return", // РЕБРЕНДИНГ
	CoreHelpSelectParam: "\n Select a kernel parameter from the list above to view documentation.",
	// --- Structured Data: SysctlDocs ---
	SysctlDocs: []SysctlHelpDoc{
		{
			ParamName:  "net.ipv4.tcp_syncookies",
			DefaultVal: "1 (Enabled)",
			MinVal:     0, MaxVal: 1,
			Description: "Protection against SYN flood. Activates cryptographic cookies in TCP queue numbers.",
			OnAttack:    "Keep strictly enabled (1). Allows the server not to allocate memory for dummy sessions.",
			Risks:       "Disabling (0) will cause immediate network stack collapse under a simple SYN storm.",
		},
		{
			ParamName:  "net.ipv4.tcp_fin_timeout",
			DefaultVal: "15 sec",
			MinVal:     10, MaxVal: 60,
			Description: "The time a socket is held in the FIN-WAIT-2 state after being closed by the server.",
			OnAttack:    "Reduce to 15 seconds for the fastest possible cleanup of kernel connection tables.",
			Risks:       "Less than 10 sec — mass drops of legitimate slow TLS/SSL handshakes will occur.",
		},
		{
			ParamName:  "net.ipv4.tcp_syn_retries",
			DefaultVal: "2",
			MinVal:     1, MaxVal: 5,
			Description: "Number of attempts to send primary SYN packets when initiating outbound sessions.",
			OnAttack:    "Reduce to 2 or 1 so that hung processes do not spawn internal socket deadlocks.",
			Risks:       "Less than 1 — the server will not be able to connect outward via unstable links.",
		},
		{
			ParamName:  "net.ipv4.tcp_synack_retries",
			DefaultVal: "2",
			MinVal:     1, MaxVal: 5,
			Description: "Number of attempts to reply with SYN-ACK to a client if there is no final ACK packet from it.",
			OnAttack:    "Set to 2. This protects the network card buffers from holding botnet sessions.",
			Risks:       "More than 5 — the server will suffocate, endlessly replying to bots with spoofed IPs.",
		},
		{
			ParamName:  "net.core.somaxconn",
			DefaultVal: "4096",
			MinVal:     128, MaxVal: 32768,
			Description: "Maximum queue size of fully established sockets waiting for an accept() call.",
			OnAttack:    "Increase to 4096 / 8192 so legitimate clients do not receive Connection Refused.",
			Risks:       "Less than 128 — a storm of errors for users during any normal restart of Nginx or 3X-UI.",
		},
		{
			ParamName:  "net.ipv4.tcp_max_syn_backlog",
			DefaultVal: "4096",
			MinVal:     128, MaxVal: 32768,
			Description: "Maximum number of half-open TCP connections (client sent SYN, but did not reply with ACK).",
			OnAttack:    "Keep value equal to somaxconn (e.g., 4096), giving headroom for SYN cookies.",
			Risks:       "With a value less than 128, DDoS protection falls from five simultaneously operating bots.",
		},
		{
			ParamName:  "net.ipv4.conf.all.rp_filter",
			DefaultVal: "1 (Strict)",
			MinVal:     0, MaxVal: 2,
			Description: "Reverse Path Filtering to protect against IP Spoofing.",
			OnAttack:    "Strictly mode 1. The kernel drops packets arriving from interfaces where they should not be.",
			Risks:       "Mode 1 with asymmetric routing (2 ISPs) can block clean traffic. Set to 2.",
		},
	},
}

// ============================================================================
// КАРТА ЯЗЫКОВ И ГЛОБАЛЬНЫЙ УКАЗАТЕЛЬ
// ============================================================================

// Languages объединяет все пакеты в общую карту для быстрого переключения
var Languages = map[string]Translation{
	"ru": RuTranslation,
	"en": EnTranslation,
}

// T — глобальный указатель, через который другие модули берут текст.
// Пример: i18n.T.MenuTitle
var T Translation

// InitLanguage инициализирует активный язык панели при старте системы
func InitLanguage(currentLang string) {
	if val, ok := Languages[currentLang]; ok {
		T = val
	} else {
		T = Languages["en"] // Дефолтный сброс при некорректном конфиге
	}
}

// ============================================================================
// ВСПОМОГАТЕЛЬНЫЕ ФУНКЦИИ ДЛЯ МЕНЮ НАСТРОЕК (SETTINGS)
// ============================================================================

// GetSettingsGroupHeader возвращает локализованное имя тематической группы настроек.
func GetSettingsGroupHeader(group string, isEn bool) string {
	switch group {
	case "LANG":
		if isEn {
			return "─── LOCALIZATION & INTERFACE MANAGEMENT"
		}
		return "─── РЕГЛАМЕНТ ЛОКАЛИЗАЦИИ И ЯЗЫК ИНТЕРФЕЙСА"
	case "TARPIT":
		if isEn {
			return "─── SSH DECEPTION TARPIT INTERVALS (PORT 22)"
		}
		return "─── ПАРАМЕТРЫ ЯДРА ДЕГТЯРНОЙ ЯМЫ SSH (ПОРТ 22) "
	case "LIMITS":
		if isEn {
			return "─── ANTI-DDOS BOUNDARIES & FIREWALL LIMITS"
		}
		return "─── СИСТЕМНЫЕ ЛИМИТЫ И АНТИ-DDOS-ОБОРOНА"
	case "SYSTEM":
		if isEn {
			return "─── CORE MANAGEMENT SERVICES"
		}
		return "─── СЕРВИСНЫЕ И УПРАВЛЯЮЩИЕ КОМАНДЫ"
	default:
		return "──────────────────────────────────────────────"
	}
}

// GetSettingsStatusText возвращает динамический статус-контракт для нижней консоли ввода
func GetSettingsStatusText(key string, isEn bool) string {
	translations := map[string][]string{
		"title_box":            {" Раздел конфигурации системы ", " System configuration sector "},
		"header_desc":          {"Глобальная Конфигурация Периметра (SETTINGS)", "Global Perimeter Configuration (SETTINGS)"},
		"form_title":           {" Модификация портов, интервалов ядра и локализации системы ", " Modification of ports, kernel intervals & localization "},
		"editing_mode":         {" РЕЖИМ РЕДАКТИРОВАНИЯ ПАРАМЕТРА ", " PARAMETER EDITING MODE "},
		"enter_new":            {"Введите новое значение", "Enter new value"},
		"input_prompt":         {"ЗНАЧЕНИЕ", "VALUE"},
		"hint_nav_1":           {"[ НАВИГАЦИЯ ]: Стрелки ↑/↓ │ [ ИЗМЕНЕНИЕ ]: Нажмите ENTER, чтобы открыть параметр", "[ NAVIGATION ]: Arrows ↑/↓ │ [ MODIFY ]: Press ENTER to open parameter"},
		"hint_nav_2":           {"[ ВЫХОД ]: Выберите строку НАЗАД или нажмите ESC для закрытия конфигуратора", "[ EXIT ]: Choose BACK row or press ESC to close config"},
		"empty_val":            {"Значение не может быть пустым!", "Value cannot be empty!"},
		"out_bounds":           {"Выход за допустимые системные границы (1-65535)!", "Value is out of valid bounds (1-65535)!"},
		"status_console_title": {" СИСТЕМНАЯ КОНСОЛЬ ", " SYSTEM CONSOLE "},
		"lang_support":         {"Допустимые значения локали: RU или EN", "Supported interface locales: RU or EN"},
		"tarpit_lock":          {"⚠️ ОТКАЗ: Дегтярная Яма АКТИВНА! Лимиты UFW заблокированы во избежание сброса логов.", "⚠️ LOCK: Tarpit is ACTIVE! UFW rate limiting disabled to preserve deception analytics."},
	}

	if vec, ok := translations[key]; ok {
		if isEn {
			return vec[1]
		}
		return vec[0]
	}
	return key
}

// FormatTuiHelpText форматирует локализованный документ справки ядра для TUI.
func FormatTuiHelpText(doc SysctlHelpDoc, curVal int, lang string) string {
	if doc.ParamName == "" {
		return Languages[lang].CoreHelpSelectParam
	}

	// Локализация меток блоков в зависимости от языка
	descLabel := " [ОПИСАНИЕ]:"
	attackLabel := " [ПРИ DDoS АТАКЕ]:"
	risksLabel := " [КРИТИЧЕСКИЕ РИСКИ]:"
	curValLabel := "Текущее боевое значение в config.json:"
	limitLabel := "Безопасные границы регулирования: от"
	factoryLabel := "Заводской эталон:"

	if lang == "en" {
		descLabel = " [DESCRIPTION]:"
		attackLabel = " [UPON DDoS ATTACK]:"
		risksLabel = " [CRITICAL RISKS]:"
		curValLabel = "Current operational value in config.json:"
		limitLabel = "Safe regulation boundaries: from"
		factoryLabel = "Factory template:"
	}

	return fmt.Sprintf(
		" Параметр: %s \n"+
			" %s %d (%s %s)\n"+
			" %s [ %d ] до [ %d ]\n\n"+
			"%s\n %s\n\n"+
			"%s\n %s\n\n"+
			"%s\n %s ",
		doc.ParamName, curValLabel, curVal, factoryLabel, doc.DefaultVal, limitLabel, doc.MinVal, doc.MaxVal, descLabel, doc.Description, attackLabel, doc.OnAttack, risksLabel, doc.Risks,
	)
}
