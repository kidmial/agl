
# Release v8.5.1

// RU:
## Оптимизация работы с приоритетами портов
- Броня ('LIMIT') сосуществует с защитой (дверьми 'A'/'B').
- Админ осознанно выбирает лимит под атакой для сбережения 'RAM'.
- Включение защиты сносит только надгробия ('Disabled'), не трогая 'LIMIT'; порядок взвода больше не влияет на состояние.
- Замок переезда при живой броне на порту '22': экран блокировки с подсказкой о снятии брони с штатного порта.
- Обратная рокировка без автовосстановления брони: админ взводит броню на порту '22' осознанно после переезда.
- Снятие брони на скрытом порту рокировки идёт через санитар без надгробия, потому что штатное открытое состояние отсутствует.
- Двери без дублей: временные двери создаются только веб-шлюзом через веб-стук; 'activateRescueChannel' не создаёт двери.
- Гейт '55111' следует за сервером ('WebServerEnabled'), а не за каналом спасения; маршруты регулирует приложение, снаружи админ видит '404' для диагностики.
- Полный цикл (стук + испепеление) выполняется только при рокировке, активном сервере и активной опции B.
- Маркер типа правила читается при восстановлении перенесённого правила.
- Маршрут '/rescue' без опции 'B' отдаёт '404'.
- Испепеление сжигает обе двери при активном 'B'.
- 'createAdminDoor' идемпотентен: существующая дверь не пересоздаётся.
- Отказ замка при рокировке даёт явную реакцию администратору.
- 'ReconcileRescueChannel' — автоматический путь без создания дверей.
- 'SyncRescueChannelState' — явный путь с созданием дверей.

// EN:
## Optimization of port priority handling
- Armor ('LIMIT') coexists with protection (doors 'A'/'B').
- The admin consciously chooses the limit under attack to conserve 'RAM'.
- Enabling protection removes only tombstones ('Disabled'), without touching 'LIMIT'; arming order no longer affects state.
- Castling lock when armor is live on port '22': lock screen with a hint to remove armor from the standard port.
- Reverse castling without automatic armor restoration: the admin arms armor on port '22' consciously after the move.
- Removing armor on the hidden castling port goes through the sanitizer without a tombstone, because the standard open state is absent.
- Doors without duplicates: temporary doors are created only by the web gateway through web knock; 'activateRescueChannel' does not create doors.
- Gate '55111' follows the server ('WebServerEnabled'), not the rescue channel; routes are regulated by the application, externally the admin sees '404' for diagnostics.
- The full cycle (knock + purge) is executed only during castling, an active server, and active option 'B'.
- The rule type marker is read when restoring a moved rule.
- The '/rescue' route without option 'B' returns '404'.
- Purging removes both doors when 'B' is active.
- 'createAdminDoor' is idempotent: an existing door is not recreated.
- Castling lock rejection gives an explicit response to the administrator.
- 'ReconcileRescueChannel' — automatic path without creating doors.
- 'SyncRescueChannelState' — explicit path with creating doors.