# Security Vulnerability Report

> **IMPORTANT**: Fill out this template when creating a private report via the [Confidential Communication Channels](./SECURITY_en.md). This will help us reproduce, verify, and eliminate the discovered issue as quickly as possible.

## 1. Vulnerability Description / Technical Details

Briefly describe the essence of the discovered issue. Which complex circuit does the vulnerability affect, and what attack vector does it relate to?

**Complex Circuit:**
- [ ] **Tarpit** (traps, inventory, daemon)
- [ ] **Armor** (perimeter armor, UFW, rate-limit)
- [ ] **Castling** (SSH port relocation)
- [ ] **Web Rescue** (web-knock, rescue channel)
- [ ] **Watcher** (idle observer)
- [ ] **Autoupdate** (server auto-update)
- [ ] **Backup** (backup system)
- [ ] **Other**: `___`

**Attack Vector** (e.g., perimeter bypass, privilege escalation, authentication bypass, Denial of Service (DoS), data leak, configuration compromise, `fail-closed` state violation):

---

## 2. Localization of the Issue in Source Code

If you were able to determine the vulnerable component, specify the file path and code lines:

- File path: `___`
- Line range: `___`
- Related components (if applicable):
  - [ ] `internal/tarpit/`
  - [ ] `internal/armor/`
  - [ ] `internal/maintenance/`
  - [ ] `internal/backup/`
  - [ ] `internal/service/`
  - [ ] `config/`
  - [ ] `config.Update()` mutation logic
  - [ ] `sys.RunTimeout` handling
  - [ ] Other: `___`

---

## 3. Reproduction Steps (PoC / Proof of Concept)

Describe the sequence of actions to trigger the anomalous behavior.

### Initial State

Describe the configuration of `/etc/amberguard/config.json` or inventory required for reproduction:

```
[Describe config state, toggles, markers, etc.]
```

### Action Sequence

List the steps (network requests, calls, state mutations):

1. `___`
2. `___`
3. `___`

### Command or Script for Reproduction

Provide an example command or script demonstrating the issue:

```
[Command or script]
```

---

## 4. Expected Behavior (Fail-Closed Principle)

How should the complex behave in this scenario according to architectural principles:

- [ ] Fall into a safe isolated state (`fail-closed`)
- [ ] Block the interface or operation
- [ ] Correctly record `machine event` code via `sys.LogAction`
- [ ] Refuse to perform the operation without compromising the system
- [ ] Other: `___`

**Detailed description of expected behavior:**

```
[Describe what should have happened]
```

---

## 5. Actual Behavior and Logs

Describe what actually happens (uncontrolled daemon failure, descriptor leak, UFW `rate-limit` rules bypass, etc.):

```
[Describe actual behavior]
```

### Snapshots and Command Output

Attach (if possible):

**1. Log `/var/log/amberguard/amberguard.log`:**
```
[Log text or its fragment]
```

**2. Info panel output `agl --info --plain`:**
```
[Command output]
```

**3. Marker state from `/var/lib/amberguard/states/`:**
```
[Output or screenshot]
```

---

## 6. Tester Environment Information

| Parameter                          | Value                        |
|------------------------------------|------------------------------|
| AGL Version (`agl --version`)     | `___`                        |
| Operating System (`uname -a`)      | `___`                        |
| Linux Kernel (`uname -r`)          | `___`                        |