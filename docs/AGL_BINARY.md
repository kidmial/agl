# Бинарник `agl`: сборка, деплой, эксплуатация

Единый бинарник комплекса Amber Guard Laboratory. Четыре режима запуска, одна точка входа (`main.go`), метаданные версии инжектятся на этапе компиляции.

## 1. Назначение и режимы запуска

| Режим                      | Команда               | Роль                                                                                          | systemd-юнит                                     |
| -------------------------- | --------------------- | --------------------------------------------------------------------------------------------- | ------------------------------------------------ |
| Демон ловушек              | `agl --tarpit-daemon` | Централизованный менеджер ВСЕХ ловушек (порт 22 + мульти), лог-конвейеры, IPC-мастер          | `agld_tarpit.service`                            |
| Наблюдатель                | `agl --watcher`       | 60-сек тикер простоя админа, веб-стук (канал спасения)                                        | `agld_watcher.service`                           |
| Автообновление (однократно)| `agl --autoupdate`    | Oneshot-цикл: `apt-get update` → `dist-upgrade` → проверка reboot-required → отложенный ребут | `agld_autoupdate.service` (по таймеру)           |
| TUI-пульт                  | `agl` (без флагов)    | Интерактивная панель администратора; слушателей НЕ запускает                                   | — (ручной запуск)                                |

Служебные флаги (выход без боевой работы):

- `agl --version` — версия и время сборки (доказательство ldflags-инъекции);
- `agl --info` — геометрия путей из compile-time констант (безопасен без config.json);
- `agl --info --plain` — плоский текстовый вывод `--info` без псевдографики;
- `agl --lang ru|en` — разовый язык запуска, не пишется в config.json (приоритет А.1).

Требование: только root (проверка euid — первая операция в `main()`, до `flag.Parse`).

## 2. Сборка

### 2.1 Локальная (разработка)

```
make build          # или: go build -o agl
```

Без ldflags `buildinfo.Version` = `dev`: это честный маркер нерелизной сборки, а не скрытая подмена версии.

### 2.2 Продакшен (деплой на сервер)

```
make build-linux                     # GOOS=linux GOARCH=amd64 + ldflags + самопроверка file
make build-linux VERSION=v8.5.2      # кастомная версия без правки файлов
```

Эквивалент вручную:

```
GOOS=linux GOARCH=amd64 go build \
  -ldflags "-X agl/internal/buildinfo.Version=v8.5.1 \
            -X agl/internal/buildinfo.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -o amberguard/agl
```

Когда применять: деплой на сервер, релиз-кандидат для аудитора.

### 2.3 Обязательная проверка формата ПЕРЕД копированием

```
file agl        # ELF 64-bit LSB executable, x86-64 ... statically linked
uname -m        # на сервере: x86_64
```

Правило (урок инцидента «Exec format error»): бинарник не покидает хост сборки без вывода `file`.
Mach-O или чужая архитектура = отказ деплоя. Makefile-таргет `build-linux` выполняет эту проверку сам.

### 2.4 Опционально: `-s -w` для продакшена

Добавить в LDFLAGS `-s -w` (убрать symbol table / DWARF): меньше размер, меньше поверхность для анализа извне.
Stack-traces паник остаются читаемыми (pclntab сохраняется).

## 3. Деплой на сервер

1. Собрать `make build-linux`, проверить `file`.
2. Передать на сервер (scp / rsync).
3. Атомарная замена (POSIX-ритуал, не ловит «Text file busy» на живом процессе):

```
cp agl /opt/amberguard/agl.new && mv /opt/amberguard/agl.new /opt/amberguard/agl
```

4. Перезапустить юниты:

```
systemctl restart agld_tarpit.service agld_watcher.service
```

5. Верификация:

```
pgrep -af agl                 # 2 процесса демонов (+ TUI при ручном запуске)
agl --version                 # реальная версия и UTC-время
tail -20 /var/log/amberguard/amberguard.log
```

Регистрация команды `agl` из любого места (однократно):

```
ln -sf /opt/amberguard/agl /usr/local/bin/agl
```

Симлинк указывает на путь, а не на inode: обновления через `mv` подхватываются без переустановки ссылки.
Systemd-юниты ходят по полному пути и не затрагиваются.

## 4. Метаданные версии

- `buildinfo.Version`, `buildinfo.BuildTime` инжектятся через `-ldflags -X`.
- Источник правды версии — команда сборки / CI, не исходники (не хардкодить в `buildinfo.go`).
- `--version` и `--info` — человеко-читаемое доказательство инъекции.

## 5. Рантайм-артефакты (что создаёт бинарник)

| Путь                                                              | Назначение                                                              | Права |
| ----------------------------------------------------------------- | ----------------------------------------------------------------------- | ----- |
| `/etc/amberguard/config.json`                                     | Верифицированная конфигурация (генерация при первом старте)             | 0644  |
| `/etc/amberguard/tarpit_inventory.json`                           | Декларативный инвентарь ловушек (клиенты пишут, демон исполняет)        | 0600  |
| `/etc/amberguard/tarpit_inventory.json.lock`                      | Сайдкар-файл транзакционной блокировки инвентаря (flock)                | 0600  |
| `/etc/amberguard/perimeter_inventory.json`                        | Инвентарь периметра: порты, обнаруженные службы (скан `/proc` + `ss`)   | 0644  |
| `/etc/amberguard/backup_targets.json`                             | Цели резервного копирования (создаётся автоматически с дефолтами)       | 0644  |
| `/etc/amberguard/certs/server.crt`, `server.key`                  | Самоподписанный TLS-сертификат веб-стука (ECDSA P-256, 10 лет)          | 0600  |
| `/var/log/amberguard/amberguard.log`                              | Системный журнал (машинные коды + локализованные описания)              | 0644  |
| `/var/log/amberguard/tarpit_connection.log`, `tarpit_report.log`  | SIEM-логи касаний и истощения (пишет ТОЛЬКО демон)                      | 0644  |
| `/var/lib/amberguard/`                                            | Состояния, квитанции брони, инвентари                                   | 0755  |
| `/var/lib/amberguard/states/agl_ssh_moved`                        | Маркер рокировки SSH (порт перенесён)                                   | 0644  |
| `/var/lib/amberguard/states/agl_armor_active_port.N`              | Квитанция брони порта N                                                 | 0644  |
| `/var/lib/amberguard/states/agl_armor_ratelimit_port.N`           | Летучий маркер rate-limit порта N                                       | 0644  |
| `/var/lib/amberguard/states/agl_tarpit_listener_port.N`           | Маркер активного слушателя ловушки порта N (создаёт/удаляет демон)      | 0644  |
| `/var/lib/amberguard/states/agl_rescue_active`                    | Маркер активного канала спасения                                        | 0644  |
| `/var/lib/amberguard/states/agl_rescue_ip_pending`                | Маркер ожидания детекции IP админа (SSH_CONNECTION пуст)                | 0644  |
| `/var/lib/amberguard/states/agl_moved_rule_type`                  | Тип правила перенесённого порта (allow / limit / reject)                | 0644  |
| `/var/lib/amberguard/states/agl_admin_idle_counter`               | Счётчик простоя администратора (наблюдатель)                            | 0644  |
| `/var/lib/amberguard/states/agl_autoupdate_last_run`              | Unix-время последнего успешного цикла автообновления                    | 0644  |
| `/var/lib/amberguard/states/agl_autoupdate_pending_reboot`        | Маркер «ожидается ребут» после обновлений ядра                          | 0644  |
| `/var/run/amberguard/<role>.lock`                                 | R-6: flock-предохранитель единственности (tmpfs)                        | 0600  |
| `/var/run/amberguard/agl_autoupdate.lock`                         | Летучий flock-лок oneshot-запуска автообновления                        | 0600  |
| `/dev/shm/amberguard.counter`                                     | IPC-счётчик ботов (мастер = демон, слейв = TUI)                         | tmpfs |
| `/dev/shm/agl_proto_web_rescue/`                                  | Директория веб-стука (файл сигнала `agl_proto_web_knock.ip`)            | 0700  |
| `/etc/systemd/system/agld_tarpit.service`                         | Юнит демона ловушек (генерируется из TUI)                               | 0644  |
| `/etc/systemd/system/agld_watcher.service`                        | Юнит наблюдателя (Restart=on-failure)                                   | 0644  |
| `/etc/systemd/system/agld_autoupdate.service`, `...timer`         | Юниты автообновления (декларативно создаются из SETTINGS)               | 0644  |
| `/etc/logrotate.d/amberguard`                                     | Конфиг ротации логов (создаётся при первом старте)                      | 0644  |

## 6. Журнал и локализация

- Формат записи (целевой, `LogActionEvent`): `<время> [КОМПОНЕНТ] [ДЕЙСТВИЕ] -> [СТАТУС] | event=КОД | локализованное описание | детали`.
- Формат записи (обёртка `LogAction`, до завершения миграции): `<время> [КОМПОНЕНТ] [ДЕЙСТВИЕ] -> [СТАТУС] | детали`.
- Описание берётся из `i18n.Events` по машинному коду; код не найден → выводится сам код дословно (фолбэк А.2, без артефактов Sprintf).
- Язык журнала: `--lang` → config.json → `AGL_LANG` → `en`.
- События до инициализации языка (boot-time) — технический английский вне словарей (доктрина §4): root-check, logrotate-warning, lock-fuse.

## 7. Предохранители

- **Root-only**: первая проверка в `main()`.
- **R-6 singleton**: неблокирующий flock на `<role>.lock`; второй экземпляр умирает с ALERT `singleton.lock_busy` и кодом 1. Ядро освобождает flock при любом завершении, включая `kill -9` и panic — мёртвых локов не бывает.
- **POSIX atomic write**: все фиксации на диск через tmp → fsync → rename.
- **Нет интерактива в демонах**: гео-диалог `[Y/n]` в демон-режиме отключён (событие `geo.install_skipped`), иначе процесс завис бы на stdin.
- **Автообновление**: oneshot-цикл защищён отдельным flock-локом (`agl_autoupdate.lock`); параллельные запуски (таймер + ручной) исключены; занято → WARNING + чистый выход (код 0), не авария.
- **Транзакции инвентаря ловушек**: запись в `tarpit_inventory.json` только через `sys.WithInventoryTx()` под эксклюзивным flock на сайдкар-файл `.lock`.

## 8. Быстрая диагностика

| Симптом                                           | Причина                                                     | Лечение                                                                  |
| ------------------------------------------------- | ----------------------------------------------------------- | ------------------------------------------------------------------------ |
| `cannot execute binary file: Exec format error`   | Mach-O или чужая архитектура на сервере                     | `file` + `uname -m`; пересобрать `make build-linux`                      |
| Машинные коды вместо описаний в журнале           | Бинарник старше словарей ИЛИ вызов до `InitLanguage`        | Пересобрать из HEAD; порядок исправлен в 6.7.5                           |
| `%!(EXTRA …)` в журнале                           | Бинарник старше `881e7da` (фолбэк без защиты)               | Пересобрать и перезапустить юниты                                        |
| Журнал пуст после ротации                         | Ротация переименовала файл, демон пишет в старый inode      | Демон обрабатывает `SIGHUP` (переоткрытие лог-файлов). Текущий конфиг ротации использует `copytruncate` — проблема не возникает. Для перехода на `postrotate`: добавить `postrotate ... kill -HUP ...` и убрать `copytruncate` |
| «роль уже выполняется другим процессом»           | Штатная работа R-6                                          | `pgrep -af agl`; не запускать второй экземпляр вручную                   |
| Дубль `singleton.lock_held` при старте            | Норма: два юнита (tarpit + watcher) логируют свои роли      | Не дефект                                                                |
| Автообновление не запускается                     | Тумблер `auto_update_enabled` выключен ИЛИ юниты не созданы | Проверить `systemctl status agld_autoupdate.timer`; включить в SETTINGS  |

## 9. Чек-лист аудитора

```
agl --version && agl --info
file /opt/amberguard/agl && uname -m
pgrep -af agl
systemctl status agld_tarpit.service agld_watcher.service --no-pager
systemctl status agld_autoupdate.service agld_autoupdate.timer --no-pager
bash scripts/i18n_gate.sh
tail -50 /var/log/amberguard/amberguard.log
```
