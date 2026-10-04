## `README_en.md`

# Amber Guard Laboratory

Autonomous defense complex for a Linux host. Detects, deceives, exhausts, protects, rescues.

## Features

- **Tarpit** — SSH trap with variable delays; base trap + declarative multi-trap pool.
- **Perimeter Armor** — auto-discovery of services and UFW rate-limit boundaries with corporate signatures; self-healing after reboot.
- **Port Castling** — moving SSH to a hidden port with auto-backup, rollback, local loopback emergency hatch and an access rule for the moved port (inherits port 22's access type); the rescue channel provides personal admin anti-lockout.
- **Web Rescue** — autonomous HTTPS web-knock server (web gateway) (TLS 1.2+, bcrypt): access even with a sealed perimeter. Active only when three conditions coincide: castling ∧ HTTPS server enabled ∧ rescue toggle enabled; when the channel is disabled, its gates are removed, the server may continue running.
- **Idle Watcher** — session detection and "incineration" of hidden rules on admin idle.
- **Forensics** — SIEM chronicle of touches and exhaustion: countries, behavior, TCP fingerprints of scanners.
- **Server Auto-Update (v8.4.0)** — autonomous daily OS update cycle (apt-get dist-upgrade in noninteractive mode), reboot-need detection and deferred reboot; scheduled by a systemd timer, declaratively enabled from SETTINGS.

## Requirements

- **OS:** Ubuntu 22.04+ / Debian 12+ (systemd, UFW, Netfilter)
- **Go:** 1.21 or newer
- **Privileges:** root (port binding below 1024 and systemd management)
- **Optional:** `geoip-bin` + `geoip-database` (countries in logs; installed automatically on first TUI launch)

## Build

```bash
make build
```

Building via Makefile injects version and timestamp through `-ldflags`:
- `VERSION` (default `v8.4.0`, override: `make build VERSION=v8.4.1`)
- `BUILD_TIME` (UTC timestamp of the build moment, RFC3339)

A bare `go build` without the Makefile leaves version `dev` — a marker of a non-release build.

## Installation

```bash
sudo make install
```

The binary is copied to `/opt/amberguard/agl`. Configuration is created automatically on first launch.

## Launch

### TUI panel (interactive control)

```bash
sudo /opt/amberguard/agl
```

### Trap daemon (systemd unit `agld_tarpit.service`)

Managed automatically from the TUI. Manual launch:

```bash
sudo ./agl --tarpit-daemon
```

### Idle watcher (systemd unit `agld_watcher.service`)

Managed declaratively: alive while at least one SETTINGS toggle is enabled
(`web_server_enabled` / `web_rescue_enabled`); when both are disabled, the process
terminates itself. Port castling does not affect the watcher's lifecycle.
Manual launch:

```bash
sudo ./agl --watcher
```

### Server auto-update (systemd units `agld_autoupdate.service` / `agld_autoupdate.timer`)

Managed declaratively: enabling the `auto_update_enabled` toggle in SETTINGS creates the units and activates the timer; disabling stops and removes them. Manual run of a single cycle:

```bash
sudo ./agl --autoupdate
```

## CLI keys

```bash
sudo ./agl --version      # Version and build stamp
sudo ./agl --info         # Service geometry (paths, units)
sudo ./agl --autoupdate   # Oneshot OS update cycle (invoked by the timer)
sudo ./agl --lang ru|en   # Interface language for this run only (not persisted)
sudo ./agl --plain        # Plain text output for --info (no pseudo-graphics)
```

## Run modes

| Mode                  | Command               | Purpose                                                    |
| --------------------- | --------------------- | ---------------------------------------------------------- |
| Trap daemon           | `agl --tarpit-daemon` | Manager of ALL traps, log pipelines, IPC                   |
| Watcher               | `agl --watcher`       | 60-second admin idle ticker, web-knock                     |
| Auto-update (oneshot) | `agl --autoupdate`    | Update cycle: apt-get update + dist-upgrade + reboot check |
| TUI panel             | `agl` (no flags)      | Interactive panel, does NOT launch listeners               |
| Version               | `agl --version`       | Version and build time                                     |
| Info                  | `agl --info`          | Path geometry from compile-time constants                  |

## Directory structure

| Path                                                 | Purpose                                       |
| ---------------------------------------------------- | --------------------------------------------- |
| `/opt/amberguard/agl`                                | Executable binary                             |
| `/etc/amberguard/config.json`                        | Central config                                |
| `/etc/amberguard/tarpit_inventory.json`              | Declarative trap inventory                    |
| `/etc/amberguard/perimeter_inventory.json`           | Perimeter inventory (auto-scan)               |
| `/var/lib/amberguard/states/`                        | Persistent state markers                      |
| `/var/lib/amberguard/states/agl_autoupdate_last_run` | Unix time of the last successful update cycle |
| `/var/log/amberguard/amberguard.log`                 | Main journal (logrotate)                      |
| `/var/log/amberguard/tarpit_connection.log`          | Socket touches                                |
| `/var/log/amberguard/tarpit_report.log`              | Forensic chronicle                            |
| `/var/run/amberguard/`                               | Runtime (single-instance lock files)          |
| `/dev/shm/amberguard.counter`                        | IPC bot counter in the trap                   |
| `/dev/shm/agl_proto_web_rescue/`                     | Web-knock directory (tmpfs)                   |
| `/etc/systemd/system/agld_autoupdate.timer`          | Auto-update schedule (created declaratively)  |
| `/var/lib/amberguard/states/agl_rescue_active`       | Active rescue channel marker                  |
| `/var/lib/amberguard/states/agl_rescue_ip_pending`   | Admin IP detection pending marker             |
| `/etc/systemd/system/agld_watcher.service`           | Watcher unit (Restart=on-failure)             |
| `/etc/systemd/system/agld_tarpit.service`            | Trap daemon unit                              |

## Server auto-update (v8.4.0)

- Toggles in SETTINGS (SYSTEM group): `auto_update_enabled` and `auto_update_reboot` — binary switches flipped with the ←/→ arrows.
- When enabled, AGL declaratively creates `agld_autoupdate.service` (Type=oneshot) and `agld_autoupdate.timer` (daily at 04:00 + random delay up to 1 hour, Persistent=true); when disabled, it stops and removes them.
- Cycle: `apt-get update` → package count via simulate → `apt-get dist-upgrade -y` (DEBIAN_FRONTEND=noninteractive, force-confdef/confold) → check of `/var/run/reboot-required`.
- A deferred reboot via `shutdown -r +1` happens only when `auto_update_reboot` is enabled; otherwise the event is logged and the admin reboots manually.
- Parallel runs are excluded by a flock lock; all events are written to the main journal under the `AUTOUPDATE` component.

## Principles (product DNA)

- **Declarativeness:** admin writes intent — the daemon brings the system to the fact (reconciliation loop, self-healing).
- **Fail closed:** any failure drops the system into a safe state, never an open one.
- **Three-factor status:** declaration → daemon confirmation → fact on disk.
- **Full autonomy:** no external web servers or clouds; one binary — the whole perimeter.

## Disclaimer

⚠️ For test and isolated servers only.
The complex modifies system configurations (`sshd_config`, UFW, sysctl),
creates systemd units and manages network rules.
Before installing on a production host:
- Make sure you understand the consequences of every operation
- Keep a backup access channel (web-knock rescue channel or physical console access)
- Test in an isolated environment
- Make a full system backup

The authors are not liable for any damage arising from the use of this software.

## License

Apache License 2.0 — see [LICENSE](LICENSE) for details.
Use at your own risk. The authors are not liable for any damage arising from the use of this software.