## `README_en.md`

# Amber Guard Laboratory

Amber Guard Laboratory
Project: Autonomous defense complex for Linux hosts on Go 1.21+. Single binary `agl`, three daemon modes + interactive TUI panel.

Key circuits:
- Tar Pit (Tarpit) — base trap on port 22 + multi-trap pool, managed by a single daemon via declarative inventory.
- Perimeter Armor (Armor) — rate-limit defense lines via UFW, auto-service discovery, kernel tuning (sysctl).
- Port Castling (Castling) — SSH relocation to a hidden port with backup and rollback.
- Rescue Channel (Web Rescue) — HTTPS web-knock server (pseudo-2FA), three protection levels (Options A, B, C).
- Idle Watcher (Watcher) — incineration of temporary doors on idle timeout.
- Server Auto-update — oneshot `apt-get dist-upgrade` via systemd timer.
- Forensics and Inventory — scanning `/proc`, `ss`, correlation.

Architectural principles: declarative management, fail-closed, three-factor status, layering (TUI → service → protocol), full autonomy, i18n via dictionaries, external commands exclusively via `sys.RunTimeout`, config mutations exclusively via `config.Update()`, paths via `config/paths.go`.

### Brief characteristics of the complex
Amber Guard Lab. (AGL) — an autonomous defense complex for Linux hosts (Ubuntu/Debian; Go, TUI, single binary). Not a "lock on the door", but an active defense ecosystem: detects, deceives, exhausts, protects, rescues, and collects digital fingerprints (metrics) of the attacker (30 parameters).

### Features
- **Tar Pit (Tarpit)** — SSH trap with variable delays; base trap on port 22 + declarative multi-trap pool managed by a single daemon.
- **Perimeter Armor (Armor)** — auto-service discovery and UFW rate-limit defense lines with corporate signatures; self-healing after reboot via reconciliation circuit (Reconcile Armor).
- **Port Castling (Castling)** — SSH relocation to a hidden port with auto-backup, rollback, local loopback emergency hatch, and accessibility rule for the moved port (inherits port 22 access type). Lock: castling is rejected if armor is live on port 22.
- **Rescue Channel (Web Rescue)** — autonomous HTTPS web-knock server (web gateway) with TLS 1.2+ and bcrypt password: access even with a closed perimeter. Three protection levels:
  - Option A (`ssh_allow_in_from_ip`): permanent admin door — IP whitelist on the hidden port.
  - Option B (`ssh_ip_web_knock`): web-knock with incineration of temporary doors on admin idle timeout. Requires Option A.
  - Option C (`web_https_panel_active`): web panel availability (route `/panel`).
  - Channel is active only when matched: castling ∧ enabled HTTPS server ∧ enabled Option B.
  - Cryptographic route prefixes (12 hex characters): `/hash/rescue`, `/hash/panel`.
- **Idle Watcher (Watcher)** — session detection via `/var/run/utmp` and "incineration" of temporary doors on admin idle. Lifecycle follows the physical server (`web_server_enabled`); when the server is disabled, the process terminates itself. Port castling does not affect the watcher lifecycle.
- **Forensics** — SIEM chronicle of touches and exhaustion: countries (GeoIP), behavior, extended TCP fingerprints of scanners (30 columns: MSS, RTT, retransmits, IAT statistics, read size buckets, close reason).
- **Server Auto-update (v8.4.0)** — autonomous daily OS update cycle (`apt-get dist-upgrade` in noninteractive mode), reboot necessity detection, and delayed reboot; scheduled via systemd timer, declaratively enabled from SETTINGS.
- **Backup (v8.4.0)** — asynchronous generation of three session files (system snapshot, package list, forensics logs) with FIFO rotation under disk shortage. Targets configured via `/etc/amberguard/backup_targets.json`.

### Requirements
- **OS:** Ubuntu 22.04+ / Debian 12+ (systemd, UFW, Netfilter)
- **Go:** 1.21 or newer (Linux only)
- **Privileges:** root (privileges for binding ports < 1024 and managing systemd)
- **Optional:** `geoip-bin` + `geoip-database` (countries in logs; installed automatically on first TUI launch)

### Build
```bash
make build
```
Build via Makefile injects version and timestamp via `-ldflags`:
- `VERSION` (default `v8.5.1`, overridden via: `make build VERSION=v8.5.2`)
- `BUILD_TIME` (UTC timestamp of build moment, RFC3339)

Bare `go build` without Makefile will leave version as `dev` — this is a marker of a non-release build.

### Installation
```bash
sudo make install
```
Binary is copied to `/opt/amberguard/agl`. Configuration is created automatically on first launch. On first start, cryptographic route prefixes for web-knock are also generated, and logrotate config is installed.

### Launch
**TUI panel (interactive management)**
```bash
sudo /opt/amberguard/agl
```

**Trap daemon (systemd unit `agld_tarpit.service`)**
Managed automatically from TUI via `tarpitctl`. Manual launch:
```bash
sudo ./agl --tarpit-daemon
```

**Idle watcher (systemd unit `agld_watcher.service`)**
Managed declaratively: alive as long as `web_server_enabled` toggle is on in SETTINGS; when server is off, process terminates itself. Port castling does not affect watcher lifecycle. Manual launch:
```bash
sudo ./agl --watcher
```

**Server auto-update (systemd units `agld_autoupdate.service` / `agld_autoupdate.timer`)**
Managed declaratively: enabling `auto_update_enabled` toggle in SETTINGS creates units and activates timer; disabling stops and removes them. Manual run of one cycle:
```bash
sudo ./agl --autoupdate
```

### CLI keys
```bash
sudo ./agl --version      # Version and build timestamp
sudo ./agl --info         # Interactive info panel (paths, statuses, rescue channel)
sudo ./agl --info --plain # Plain text output for --info (no pseudo-graphics)
sudo ./agl --autoupdate   # Oneshot OS update cycle (called by timer)
sudo ./agl --lang ru|en   # Interface language for this launch (not saved)
```

### Launch modes
| Mode                  | Command               | Purpose                                                     |
| --------------------- | --------------------- | ----------------------------------------------------------- |
| Trap daemon           | `agl --tarpit-daemon` | Manager of ALL traps, log pipelines, IPC                    |
| Watcher               | `agl --watcher`       | 30-sec admin idle ticker, web-knock, rescue server          |
| Auto-update (oneshot) | `agl --autoupdate`    | Update cycle: apt-get update + dist-upgrade + reboot check  |
| TUI console           | `agl` (no flags)      | Interactive panel, does NOT start listeners                 |
| Version               | `agl --version`       | Version and build time                                      |
| Info                  | `agl --info`          | Interactive panel: rescue channel, services, paths, version |

### Directory structure
| Path                                                 | Purpose                                           |
| ---------------------------------------------------- | ------------------------------------------------- |
| `/opt/amberguard/agl`                                | Executable binary                                 |
| `/etc/amberguard/config.json`                        | Central config                                    |
| `/etc/amberguard/tarpit_inventory.json`              | Declarative trap inventory                        |
| `/etc/amberguard/perimeter_inventory.json`           | Perimeter inventory (auto-scan)                   |
| `/etc/amberguard/backup_targets.json`                | Backup targets                                    |
| `/etc/amberguard/certs/server.crt`                   | Web-knock TLS certificate (self-signed, 10 years) |
| `/etc/amberguard/certs/server.key`                   | TLS private key (0600)                            |
| `/var/lib/amberguard/states/`                        | Persistent state markers                          |
| `/var/lib/amberguard/states/agl_autoupdate_last_run` | Unix time of last successful update cycle         |
| `/var/lib/amberguard/states/agl_rescue_active`       | Active rescue channel marker                      |
| `/var/lib/amberguard/states/agl_rescue_ip_pending`   | Admin IP detection pending marker                 |
| `/var/lib/amberguard/states/agl_moved_rule_type`     | Moved port rule type (allow/limit/reject)         |
| `/var/log/amberguard/amberguard.log`                 | Main log (logrotate)                              |
| `/var/log/amberguard/tarpit_connection.log`          | Socket touches                                    |
| `/var/log/amberguard/tarpit_report.log`              | Forensic chronicle (30 columns)                   |
| `/var/run/amberguard/`                               | Runtime (flock uniqueness safeguards)             |
| `/dev/shm/amberguard.counter`                        | IPC bot counter in trap                           |
| `/dev/shm/agl_proto_web_rescue/`                     | Web-knock directory (tmpfs, 0700)                 |
| `/etc/systemd/system/agld_tarpit.service`            | Trap daemon unit                                  |
| `/etc/systemd/system/agld_watcher.service`           | Watcher unit (Restart=on-failure)                 |
| `/etc/systemd/system/agld_autoupdate.timer`          | Auto-update schedule (created declaratively)      |
| `/etc/logrotate.d/amberguard`                        | Log rotation config (created on first start)      |

### Server Auto-update
Toggles in SETTINGS (SYSTEM group): `auto_update_enabled` and `auto_update_reboot` — binary switches, inverted with ←/→ arrows.
When enabled, AGL declaratively creates `agld_autoupdate.service` (Type=oneshot) and `agld_autoupdate.timer` (daily at 04:00 + random jitter up to 1 hour, Persistent=true); when disabled — stops and removes them.
Cycle: `apt-get update` → package count via simulate → `apt-get dist-upgrade -y` (DEBIAN_FRONTEND=noninteractive, force-confdef/confold) → check `/var/run/reboot-required`.
Delayed reboot via `shutdown -r +1` is executed only if `auto_update_reboot` is enabled; otherwise, the event is logged, administrator reboots manually.
Parallel launches are excluded by flock-lock; all events are written to the main log under component `AUTOUPDATE`.

### Backup
Launched from TUI (BACKUP menu). Asynchronous generation of three files into session directory `/backup/YYYY.MM.DD_HH.MM/`.
Files: `*_server_snapshot.tar.gz` (system configs), `*_package_list.txt` (dpkg), `*_forensics_logs.tar.gz` (SIEM logs).
Targets configured via `/etc/amberguard/backup_targets.json` (created automatically with defaults).
FIFO rotation: under disk shortage (<15%), old sessions are deleted automatically.

### Principles (Product DNA)
- **Declarative management:** admin writes intent — daemon brings system to fact (reconciliation loop, self-healing).
- **Fail closed:** any failure drops system to safe state, never to open.
- **Three-factor status:** declaration → daemon confirmation → fact on disk.
- **Full autonomy:** no external web servers or clouds; one binary — entire perimeter.
- **Uniqueness safeguards:** flock-locks for daemon modes; second instance dies with ALERT.
- **POSIX Atomic Write:** all disk commits via tmp → fsync → rename.

### Disclaimer
For test and isolated servers only.
The complex modifies system configurations (`sshd_config`, UFW, sysctl),
creates systemd units and manages network rules.
Before installing on a production host:
- Ensure you understand the consequences of each operation
- Have a backup access channel (web-knock rescue channel or physical console access)
- Test in an isolated environment
- Make a full system backup

The authors are not responsible for any damage arising from the use of this software.

### License
Apache License 2.0 — see [LICENSE](LICENSE) file for details.
Use at your own risk. The authors are not responsible for any damage arising from the use of this software.