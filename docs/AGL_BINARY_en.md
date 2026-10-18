# The `agl` binary: build, deploy, operation

Single binary of the Amber Guard Laboratory complex. Four launch modes, one entry point (`main.go`), version metadata injected at compile time.

## 1. Purpose and launch modes

| Mode                        | Command               | Role                                                                                         | systemd unit                                       |
| --------------------------- | --------------------- | -------------------------------------------------------------------------------------------- | -------------------------------------------------- |
| Trap daemon                 | `agl --tarpit-daemon` | Centralized manager of ALL traps (port 22 + multi), log pipelines, IPC master                 | `agld_tarpit.service`                              |
| Watcher                     | `agl --watcher`       | 60-sec admin idle ticker, web-knock (rescue channel)                                          | `agld_watcher.service`                             |
| Auto-update (oneshot)       | `agl --autoupdate`    | Oneshot cycle: `apt-get update` → `dist-upgrade` → reboot-required check → deferred reboot    | `agld_autoupdate.service` (scheduled by timer)     |
| TUI console                 | `agl` (no flags)      | Interactive admin panel; does NOT start listeners                                              | — (manual launch)                                  |

Service flags (exit without operational work):

- `agl --version` — version and build time (proof of ldflags injection);
- `agl --info` — path geometry from compile-time constants (safe without config.json);
- `agl --info --plain` — plain text output for `--info` without pseudo-graphics;
- `agl --lang ru|en` — one-time launch language, not written to config.json (priority A.1).

Requirement: root only (euid check is the very first operation in `main()`, before `flag.Parse`).

## 2. Build

### 2.1 Local (development)

```
make build          # or: go build -o agl
```

Without ldflags `buildinfo.Version` = `dev`: this is an honest non-release build marker, not a hidden version substitution.

### 2.2 Production (deploy to server)

```
make build-linux                     # GOOS=linux GOARCH=amd64 + ldflags + self-check via file
make build-linux VERSION=v8.5.2      # custom version without editing files
```

Manual equivalent:

```
GOOS=linux GOARCH=amd64 go build \
  -ldflags "-X agl/internal/buildinfo.Version=v8.5.1 \
            -X agl/internal/buildinfo.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -o amberguard/agl
```

When to apply: server deploy, release candidate for the auditor.

### 2.3 Mandatory format check BEFORE copying

```
file agl        # ELF 64-bit LSB executable, x86-64 ... statically linked
uname -m        # on the server: x86_64
```

Rule (lesson of the "Exec format error" incident): the binary does not leave the build host without `file` output.
Mach-O or a foreign architecture = deploy rejection. The Makefile target `build-linux` performs this check itself.

### 2.4 Optional: `-s -w` for production

Add `-s -w` to LDFLAGS (strip symbol table / DWARF): smaller size, smaller surface for external analysis.
Panic stack traces remain readable (pclntab is preserved).

## 3. Deploy to server

1. Build `make build-linux`, verify with `file`.
2. Transfer to the server (scp / rsync).
3. Atomic replacement (POSIX ritual, avoids "Text file busy" on a live process):

```
cp agl /opt/amberguard/agl.new && mv /opt/amberguard/agl.new /opt/amberguard/agl
```

4. Restart the units:

```
systemctl restart agld_tarpit.service agld_watcher.service
```

5. Verification:

```
pgrep -af agl                 # 2 daemon processes (+ TUI if launched manually)
agl --version                 # actual version and UTC time
tail -20 /var/log/amberguard/amberguard.log
```

Register the `agl` command globally (one-time):

```
ln -sf /opt/amberguard/agl /usr/local/bin/agl
```

The symlink points to a path, not an inode: updates via `mv` are picked up without reinstalling the link.
Systemd units use the full path and are unaffected.

## 4. Version metadata

- `buildinfo.Version`, `buildinfo.BuildTime` are injected via `-ldflags -X`.
- The source of truth for the version is the build command / CI, not the sources (do not hardcode in `buildinfo.go`).
- `--version` and `--info` — human-readable proof of injection.

## 5. Runtime artifacts (what the binary creates)

| Path                                                              | Purpose                                                                 | Perms |
| ----------------------------------------------------------------- | ----------------------------------------------------------------------- | ----- |
| `/etc/amberguard/config.json`                                     | Verified configuration (generated on first start)                       | 0644  |
| `/etc/amberguard/tarpit_inventory.json`                           | Declarative trap inventory (clients write, daemon executes)             | 0600  |
| `/etc/amberguard/tarpit_inventory.json.lock`                      | Sidecar transaction lock file for the inventory (flock)                 | 0600  |
| `/etc/amberguard/perimeter_inventory.json`                        | Perimeter inventory: ports, detected services (`/proc` + `ss` scan)     | 0644  |
| `/etc/amberguard/backup_targets.json`                             | Backup targets (auto-created with defaults)                             | 0644  |
| `/etc/amberguard/certs/server.crt`, `server.key`                  | Self-signed TLS certificate for web-knock (ECDSA P-256, 10 years)       | 0600  |
| `/var/log/amberguard/amberguard.log`                              | System journal (machine codes + localized descriptions)                 | 0644  |
| `/var/log/amberguard/tarpit_connection.log`, `tarpit_report.log`  | SIEM logs of touches and exhaustion (written ONLY by the daemon)        | 0644  |
| `/var/lib/amberguard/`                                            | States, armor receipts, inventories                                     | 0755  |
| `/var/lib/amberguard/states/agl_ssh_moved`                        | SSH castling marker (port has been moved)                               | 0644  |
| `/var/lib/amberguard/states/agl_armor_active_port.N`              | Armor receipt for port N                                                | 0644  |
| `/var/lib/amberguard/states/agl_armor_ratelimit_port.N`           | Ephemeral rate-limit marker for port N                                  | 0644  |
| `/var/lib/amberguard/states/agl_tarpit_listener_port.N`           | Active trap listener marker for port N (created/removed by daemon)      | 0644  |
| `/var/lib/amberguard/states/agl_rescue_active`                    | Active rescue channel marker                                            | 0644  |
| `/var/lib/amberguard/states/agl_rescue_ip_pending`                | Admin IP detection pending marker (SSH_CONNECTION was empty)            | 0644  |
| `/var/lib/amberguard/states/agl_moved_rule_type`                  | Moved-port rule type (allow / limit / reject)                           | 0644  |
| `/var/lib/amberguard/states/agl_admin_idle_counter`               | Admin idle counter (watcher)                                            | 0644  |
| `/var/lib/amberguard/states/agl_autoupdate_last_run`              | Unix time of last successful auto-update cycle                          | 0644  |
| `/var/lib/amberguard/states/agl_autoupdate_pending_reboot`        | "Reboot pending" marker after kernel updates                            | 0644  |
| `/var/run/amberguard/<role>.lock`                                 | R-6: flock uniqueness safeguard (tmpfs)                                 | 0600  |
| `/var/run/amberguard/agl_autoupdate.lock`                         | Ephemeral flock for oneshot auto-update runs                            | 0600  |
| `/dev/shm/amberguard.counter`                                     | IPC bot counter (master = daemon, slave = TUI)                          | tmpfs |
| `/dev/shm/agl_proto_web_rescue/`                                  | Web-knock directory (signal file `agl_proto_web_knock.ip`)              | 0700  |
| `/etc/systemd/system/agld_tarpit.service`                         | Trap daemon unit (generated from TUI)                                   | 0644  |
| `/etc/systemd/system/agld_watcher.service`                        | Watcher unit (Restart=on-failure)                                       | 0644  |
| `/etc/systemd/system/agld_autoupdate.service`, `...timer`         | Auto-update units (declaratively created from SETTINGS)                 | 0644  |
| `/etc/logrotate.d/amberguard`                                     | Log rotation config (created on first start)                            | 0644  |

## 6. Journal and localization

- Record format (target, `LogActionEvent`): `<time> [COMPONENT] [ACTION] -> [STATUS] | event=CODE | localized description | details`.
- Record format (wrapper `LogAction`, until migration is complete): `<time> [COMPONENT] [ACTION] -> [STATUS] | details`.
- Description is taken from `i18n.Events` by machine code; code not found → the code itself is printed verbatim (A.2 fallback, no Sprintf artifacts).
- Journal language: `--lang` → config.json → `AGL_LANG` → `en`.
- Events before language initialization (boot-time) — technical English outside dictionaries (doctrine §4): root-check, logrotate-warning, lock-fuse.

## 7. Safeguards

- **Root-only**: first check in `main()`.
- **R-6 singleton**: non-blocking flock on `<role>.lock`; a second instance dies with ALERT `singleton.lock_busy` and exit code 1. The kernel releases flock on any termination, including `kill -9` and panic — dead locks never happen.
- **POSIX atomic write**: all disk commits go through tmp → fsync → rename.
- **No interactivity in daemons**: the geo-dialog `[Y/n]` is disabled in daemon mode (event `geo.install_skipped`), otherwise the process would hang on stdin.
- **Auto-update**: the oneshot cycle is protected by a separate flock lock (`agl_autoupdate.lock`); parallel runs (timer + manual) are excluded; lock busy → WARNING + clean exit (code 0), not an emergency.
- **Trap inventory transactions**: writes to `tarpit_inventory.json` only through `sys.WithInventoryTx()` under an exclusive flock on the sidecar `.lock` file.

## 8. Quick diagnostics

| Symptom                                           | Cause                                                       | Fix                                                                                     |
| ------------------------------------------------- | ----------------------------------------------------------- | --------------------------------------------------------------------------------------- |
| `cannot execute binary file: Exec format error`   | Mach-O or foreign architecture on the server                | `file` + `uname -m`; rebuild with `make build-linux`                                    |
| Machine codes instead of descriptions in journal  | Binary older than dictionaries OR call before `InitLanguage`| Rebuild from HEAD; ordering fixed in 6.7.5                                              |
| `%!(EXTRA …)` in journal                          | Binary older than `881e7da` (unprotected fallback)          | Rebuild and restart units                                                               |
| Journal empty after rotation                      | Rotation renamed the file, daemon writes to old inode       | Daemon handles `SIGHUP` (log file reopening). Current rotation config uses `copytruncate` — the issue does not occur. To switch to `postrotate`: add `postrotate ... kill -HUP ...` and remove `copytruncate` |
| "role already performed by another process"       | Normal R-6 operation                                        | `pgrep -af agl`; do not start a second instance manually                                |
| Duplicate `singleton.lock_held` at startup        | Normal: two units (tarpit + watcher) log their roles        | Not a defect                                                                            |
| Auto-update does not run                          | Toggle `auto_update_enabled` is off OR units not created    | Check `systemctl status agld_autoupdate.timer`; enable in SETTINGS                      |

## 9. Auditor checklist

```
agl --version && agl --info
file /opt/amberguard/agl && uname -m
pgrep -af agl
systemctl status agld_tarpit.service agld_watcher.service --no-pager
systemctl status agld_autoupdate.service agld_autoupdate.timer --no-pager
bash scripts/i18n_gate.sh
tail -50 /var/log/amberguard/amberguard.log
```
