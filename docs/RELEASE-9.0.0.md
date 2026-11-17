# Release v9.0.0

// RU:
## Веб-панель и Аналитика
- Полная локализация веб-панели и аналитического ядра (RU/EN), внедрён глобальный JS-словарь `window.AGL_I18N`.
- Конвейер аналитики: парсинг TSV-логов, нормализация, профилирование акторов, классификация фаз угроз и детекция ботнет-паттернов.
- Дашборды: 'Overview' (плашка «Украденное время»), 'Vitals' (виджет 'GeoPulse', индекс скорости атаки 'IAT'), 'Fingerprints' (TCP Fingerprinting), 'Tarpit' (план/факт, 'CloseReason', 'RTT'), 'SIEM Core' (детекторы 'Proxy/Tunnel', 'OS Spoofing'), 'Reports' (кольца бакетов 24ч/7д/30д).
- Досье актора v2: журнал сессий (FIFO), доказательная суточная таблица, доминирование в странах, ротация IP и поведенческий портрет.
- Режим приватности (Secure by Default): скрытие чувствительных метрик (IP, страны, порты) через `<agl-private>` (CSS blur). Защита данных на графиках (Chart.js watermark и подмена тиков на `••••`).
- Авторизация: 'Session cookies', форма входа и защита от брутфорса ('lockout' на 60 сек после 5 неудач).
- UX и Автообновление: сквозное тихое автообновление (без мерцания, с сохранением состояния UI), адаптивный дизайн, липкая шапка и поддержка тем ('Tactical Dark' / 'Cyber-Minimalism').
- Read-Only доктрина: удалены POST-запросы на rebuild из веб-интерфейса; управление историей переведено в декларативные тумблеры TUI.

## Терминальный интерфейс (TUI)
- Единый дизайн и Бренд: сегментные шапки ('ScreenHeader' / 'ScreenList') с янтарной брендовой грамматикой 'Amber Guard Laboratory'. Унифицированы геометрия и отступы на всех экранах.
- Управление ловушками: пакетные тумблеры ('START_ALL' / 'STOP_ALL') для пула мульти-ловушек с защитой ядра от случайного отключения.
- Алерты и UX: inbox-семантика для TTL-алертов (dismiss по 'Escape'), явный флаг 'isError' заменил хрупкий парсинг префиксов `[ OK ]`.
- Главный экран: арифметическая сетка 3x4 для служб, максимальная плотность фактов (SSH Admin IP, статус паролей канала спасения), хардинг парсера ('/proc', 'systemctl').
- Бэкап: добавлен третий контур архивации ('user_data') для персональных файлов администратора с раскраской тегов операций в логах.
- Настройки: строгие валидаторы иерархии (история требует панель и сервер, веб-стук требует пароль).

## Ядро, Броня и Сеть
- Эталон Linux (Ubuntu 24.04): 'rp_filter' переведен в режим '2' (loose) для поддержки асимметричного роутинга и TUN/VPN. 'somaxconn' baseline поднят до '4096'.
- Единый источник правды 'sysctl': внедрена спека `sysctl_spec.go`. Устранены коллизии 'Factory Reset' (сброс в истинный эталон) и 'Clamping' (границы больше не режут валидные baseline-значения).
- Хирургический переезд порта 22 (SSH): сохранение бут-топологии (перенос владельца с 'ssh.socket' на 'ssh.service'), ликвидация LISTEN-процесса порта 22 после подтверждения слушателя на customPort.
- UFW-предохранитель: блокировка рокировки портов при выключенном UFW (архитектура fail-closed).
- Наблюдатель (Watcher): перехват паник воркеров, бесконечные retry-циклы для писателей логов, криминалистический Hex-слепок payload при сбоях.
- Порт 22 как константа домена: удалена возможность изменения стандартного SSH-порта через настройки, что предотвращает поломку обратного переезда OpenSSH.

## Локализация (i18n)
- Архитектура словарей: выделен блок 'Shared' (единицы измерения, колонки, пустые состояния), что позволило удалить десятки дубликатов.
- Синхронизация состояний: токены поведения ('AGGR'/'PASS'/'MIXED'/'UNK') маппятся на компактные метки ('АГР'/'ПАС'/'СМШ'/'НЗВ') идентично на бэкенде и в JS-интерфейсе.
- Справка ядра: выверена и исправлена. Удалены недостоверные утверждения, описания 'fin_timeout', 'cookies' и 'backlog' приведены к реальной документации ядра Linux.

## Инсталлятор и CLI
- Декларативное меню ('install.sh'): разделение намерения и исполнения (инфо, SHA-256, настройка UFW, рекомендуемые обновления, установка).
- Бут-топология: стадия 0 (подготовка до касания бинарника). При сбое система остается нетронутой.
- CLI: добавлен пункт '--info' для быстрого получения URL веб-панели и статуса. Вывод репозитория GitFlic.

## Критические исправления
- Инжест логов: исправлено «замирание» аналитики после первого 'EOF' (хвост лога не читался до ручного рестарта сервиса).
- Дрейф времени: калибровка Алгоритма 2 (дрейф токена был -3 часа из-за 'time.Parse' без учета 'time.Local').
- UI Артефакты: устранено «самовольное раскрытие всех плашек» SIEM при автообновлении (переход на составные 'data-key').
- Геометрия TUI: исправлена полоса выделения списков 'termui', которая обрывалась на последнем символе (внедрен 'padListRowsToWidth').
- Безопасность статики: директория '/assets/' защищена от 'directory listing'. Запросы к несуществующим файлам возвращают стилизованный '404 Stub' (принцип Fail-closed).
- Дедупликация: устранен двойной прогон 'ufw'-правил при пакетных операциях брони (IPv4+IPv6).

// EN:
## Web Panel & Analytics
- Full localization of the web panel and analytics core (RU/EN), injected global JS dictionary `window.AGL_I18N`.
- Analytics pipeline: TSV log parsing, normalization, actor profiling, threat phase classification, and botnet pattern detection.
- Dashboards: 'Overview' ("Stolen time" widget), 'Vitals' ('GeoPulse' widget, attack velocity index 'IAT'), 'Fingerprints' (TCP Fingerprinting), 'Tarpit' (plan/actual, 'CloseReason', 'RTT'), 'SIEM Core' ('Proxy/Tunnel', 'OS Spoofing' detectors), 'Reports' (bucket rings for 24h/7d/30d).
- Actor dossier v2: session journal (FIFO), daily evidence table, country dominance, IP rotation, and behavioral portrait.
- Privacy mode (Secure by Default): concealment of sensitive metrics (IPs, countries, ports) via `<agl-private>` (CSS blur). Chart data protection (Chart.js watermark and tick replacement with `••••`).
- Authentication: 'Session cookies', login form, and anti-bruteforce protection ('lockout' for 60s after 5 failures).
- UX & Auto-refresh: end-to-end silent auto-refresh (zero flicker, UI state preservation), responsive design, sticky header, and dual themes ('Tactical Dark' / 'Cyber-Minimalism').
- Read-Only doctrine: removed POST rebuild requests from the web UI; history management moved to declarative TUI toggles.

## Terminal UI (TUI)
- Unified design & Brand: segment-based headers ('ScreenHeader' / 'ScreenList') with amber brand grammar for 'Amber Guard Laboratory'. Unified geometry and padding across all screens.
- Trap management: batch toggles ('START_ALL' / 'STOP_ALL') for the multi-trap pool with core trap protection against accidental shutdown.
- Alerts & UX: inbox semantics for TTL alerts (dismiss on 'Escape'), explicit 'isError' flag replaced fragile `[ OK ]` prefix parsing.
- Main screen: 3x4 arithmetic grid for services, maximum fact density (SSH Admin IP, rescue channel password status), parser hardening ('/proc', 'systemctl').
- Backup: added a third archiving contour ('user_data') for administrator personal files with colorized operation tags in logs.
- Settings: strict hierarchy validators (history requires panel and server, web knock requires password).

## Core, Armor & Network
- Linux baseline (Ubuntu 24.04): 'rp_filter' switched to mode '2' (loose) to support asymmetric routing and TUN/VPN. 'somaxconn' baseline raised to '4096'.
- Single source of truth for 'sysctl': introduced `sysctl_spec.go`. Eliminated 'Factory Reset' collisions (reset to true baseline) and 'Clamping' issues (bounds no longer cut valid baseline values).
- Surgical port 22 (SSH) move: preserved boot topology (ownership moved from 'ssh.socket' to 'ssh.service'), surgical termination of port 22 LISTEN process after customPort listener confirmation.
- UFW interlock: blocks port castling when UFW is inactive (fail-closed architecture).
- Watcher: intercepts worker panics, infinite retry loops for log writers, forensic hex dump of payload on failures.
- Port 22 as a domain constant: removed the ability to change the standard SSH port via settings, preventing OpenSSH reverse castling breakage.

## Localization (i18n)
- Dictionary architecture: extracted 'Shared' block (units, columns, empty states), eliminating dozens of duplicates.
- State synchronization: behavior tokens ('AGGR'/'PASS'/'MIXED'/'UNK') mapped to compact labels ('АГР'/'ПАС'/'СМШ'/'НЗВ') consistently across backend and JS interface.
- Kernel help: audited and corrected. Removed unfounded claims, aligned 'fin_timeout', 'cookies', and 'backlog' descriptions with actual Linux kernel documentation.

## Installer & CLI
- Declarative menu ('install.sh'): separation of intent and execution (info, SHA-256, UFW setup, recommended updates, installation).
- Boot topology: stage 0 (preparation before touching the binary). On failure, the system remains untouched.
- CLI: added '--info' flag for quick retrieval of the web panel URL and status. Outputs GitFlic repository.

## Critical Bug Fixes
- Log ingestion: fixed analytics "freezing" after the first 'EOF' (log tail was unread until manual service restart).
- Time drift: calibrated Algorithm 2 (token drift was -3 hours due to 'time.Parse' ignoring 'time.Local').
- UI Artifacts: eliminated "all SIEM plaques popping open" on auto-refresh (switched to composite 'data-key').
- TUI Geometry: fixed 'termui' list selection bar breaking at the last character (introduced 'padListRowsToWidth').
- Static security: '/assets/' directory protected against 'directory listing'. Requests for non-existent files return a styled '404 Stub' (Fail-closed principle).
- Deduplication: eliminated double 'ufw' rule runs during batch armor operations (IPv4+IPv6).