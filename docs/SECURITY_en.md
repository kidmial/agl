# Security Policy

We take the security of our defensive complex and data privacy extremely seriously. If you have discovered a vulnerability or security bug in the software components of Amber Guard Laboratory (AGL), please review our Coordinated Vulnerability Disclosure rules.

## Our Security Manifesto

Coordinated disclosure, honest triage.

We respond within 7 days, patch within 14 days, and publish no earlier than 90 days. Every report enters the same forensics pipeline that preserves attacker fingerprints — no report is lost.

## Supported Versions

| Version | Supported |
|---------|-----------|
| `v8.5.x`| ✅ Yes     |
| `< v8.0`| ❌ No      |

## Vulnerability Reporting Channels

We welcome findings. To ensure security and confidentiality, please strictly separate reporting channels based on the nature and criticality of the discovered issue.

### Confidential Channels (for critical vulnerabilities)

If your finding relates to security circuits (**Tarpit**, **Armor**, **Castling**, **Web Rescue**, **Watcher**, **Autoupdate**, **Backup**), allows bypassing authorization mechanisms, causes uncontrolled daemon-mode failures, or compromises the `fail-closed` state of the complex, use only the protected channel:

- **Submit a private report via GitHub Security Advisory**: [https://github.com/kidmial/agl/security/advisories/new](https://github.com/kidmial/agl/security/advisories/new)

The report will go directly to the development team in a private dashboard. This channel guarantees protection of the transmitted technical data and logs from interception by third parties until the official release of the fix.

### Public Channels (for ordinary bugs and suggestions)

If the issue does not pose a direct threat to the host's security and cannot be used to bypass the defense perimeter (e.g., `i18n` localization errors, typos in the TUI panel, build logic refactoring suggestions), use the standard issue trackers:

- **Create an Issue on GitHub**: [https://github.com/kidmial/agl/issues](https://github.com/kidmial/agl/issues) — for public notification of issues found on the GitHub platform.
- **Create an Issue on GitFlic**: [https://gitflic.ru/project/kidmial/agl/issue](https://gitflic.ru/project/kidmial/agl/issue) — for public tasks and discussions in the repository on the GitFlic platform.

## Processing and Verification Timelines

We value the work of independent researchers and guarantee adherence to strict timeframes for processing and verifying your findings:

| Processing Stage    | Deadline / Condition | Description                                                                                       |
|---------------------|----------------------|---------------------------------------------------------------------------------------------------|
| **Response (Triage)**| Up to 7 days         | We acknowledge receipt, conduct initial technical analysis and verification of the issue in an isolated environment. |
| **Fix (Patch)**     | Up to 14 days        | We develop, test, and release the fix within the framework of atomic configuration and build updates.               |
| **Publication (Disclose)**| No earlier than 90 days | We publish the vulnerability details, giving host administrators sufficient time to update systems.               |

## Interaction Rules

As part of the coordinated disclosure program, we ask you to:

1. **Not use discovered vulnerabilities** for destructive actions, destruction of forensics logs, disruption of infrastructure operation, or exploitation of systems where the complex is deployed.
2. **Not apply methods of social engineering**, phishing, or physical attacks against developers and users of the project.
3. **Not disclose technical details of the vulnerability**, PoC (Proof of Concept), and bypass methods to third parties until the expiration of the established 90-day publication period of the patch.

Thank you for your help in protecting the project. All confirmed researchers will be mentioned in the official release notes of the defensive complex (upon agreement).

## Bug Bounty

The bug bounty program is currently inactive, but we are grateful for responsible disclosure.