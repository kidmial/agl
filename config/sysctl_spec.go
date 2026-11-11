// config/sysctl_spec.go
package config

import "fmt"

// ============================================================================
// ЕДИНАЯ СПЕКА ПАРАМЕТРОВ ЯДРА (CORE_SETTINGS)
// ============================================================================
// PURPOSE: Один источник правды для трёх наборов значений и границ.
// CONTRACT:
//   - Factory  — эталон AGL. Ведущий: ensureDefaultConfig + RESET_TO_FACTORY.
//   - Baseline — честный стандарт Linux. Фолбэк ARMOR_OFF без слепка.
//     ГРАНИЦЫ К BASELINE НЕ ПРИМЕНЯЮТСЯ: syn_retries=6 законен для чистого ядра.
//   - Min/Max/Fallback — границы Armor. Fallback всегда внутри границ.
//   - Порядок элементов = порядок строк TUI = GetSysctlParamNameByIndex.
// SIDE-EFFECTS: нет (чистые данные).
// ============================================================================

// SysctlParamSpec describes one kernel knob managed by CORE_SETTINGS.
type SysctlParamSpec struct {
	ParamName string // машинное имя параметра ядра
	Factory   int    // эталон AGL (боевой дефолт под 1 ГБ RAM)
	Baseline  int    // стандарт чистого Linux (для отката OFF)
	Min       int    // нижняя ИБ-граница Armor
	Max       int    // верхняя ИБ-граница Armor
	Fallback  int    // безопасное значение при выходе за границы
}

// sysctlSpec — таблица эталонов. Индексы 0..7 совпадают с меню TUI.
var sysctlSpec = [8]SysctlParamSpec{
	{"net.ipv4.tcp_syncookies", 1, 1, 0, 1, 1},
	{"net.ipv4.tcp_fin_timeout", 15, 60, 10, 60, 15},
	{"net.ipv4.tcp_syn_retries", 2, 6, 1, 5, 2},
	{"net.ipv4.tcp_synack_retries", 2, 5, 1, 5, 2},
	{"net.core.somaxconn", 2048, 128, 128, 32768, 2048},
	{"net.ipv4.tcp_max_syn_backlog", 4096, 128, 128, 32768, 4096},
	{"net.ipv4.conf.all.rp_filter", 1, 0, 0, 2, 1},
	{"net.ipv4.conf.default.rp_filter", 1, 0, 0, 2, 1},
}

// SysctlSpec returns a copy of the kernel knob specification table.
func SysctlSpec() []SysctlParamSpec {
	out := make([]SysctlParamSpec, len(sysctlSpec))
	copy(out, sysctlSpec[:])
	return out
}

// SysctlSpecByIndex returns the spec for a TUI row index.
func SysctlSpecByIndex(i int) (SysctlParamSpec, bool) {
	if i < 0 || i >= len(sysctlSpec) {
		return SysctlParamSpec{}, false
	}
	return sysctlSpec[i], true
}

// armorPtrs — указатели на боевые поля в порядке спеки (DRY для switch-ей).
func (s *SysctlCoreConfig) armorPtrs() [8]*int {
	return [8]*int{
		&s.ArmorNetIpv4TcpSyncookies, &s.ArmorNetIpv4TcpFinTimeout,
		&s.ArmorNetIpv4TcpSynRetries, &s.ArmorNetIpv4TcpSynackRetries,
		&s.ArmorNetCoreSomaxconn, &s.ArmorNetIpv4TcpMaxSynBacklog,
		&s.ArmorNetIpv4ConfAllRpFilter, &s.ArmorNetIpv4ConfDefaultRpFilter,
	}
}

// baselinePtrs — указатели на заводские поля Linux в порядке спеки.
func (s *SysctlCoreConfig) baselinePtrs() [8]*int {
	return [8]*int{
		&s.DefaultNetIpv4TcpSyncookies, &s.DefaultNetIpv4TcpFinTimeout,
		&s.DefaultNetIpv4TcpSynRetries, &s.DefaultNetIpv4TcpSynackRetries,
		&s.DefaultNetCoreSomaxconn, &s.DefaultNetIpv4TcpMaxSynBacklog,
		&s.DefaultNetIpv4ConfAllRpFilter, &s.DefaultNetIpv4ConfDefaultRpFilter,
	}
}

// FactorySysctlCore builds the canonical AGL factory snapshot.
// CONTRACT: единственный поставщик дефолтов для ensureDefaultConfig и RESET.
func FactorySysctlCore() SysctlCoreConfig {
	var s SysctlCoreConfig
	armor, base := s.armorPtrs(), s.baselinePtrs()
	for i, spec := range sysctlSpec {
		*armor[i] = spec.Factory // боевой эталон AGL
		*base[i] = spec.Baseline // честный Linux для отката
	}
	return s
}

// ArmorByIndex reads the admin-tunable value for a TUI row index.
func (s *SysctlCoreConfig) ArmorByIndex(i int) (int, bool) {
	if i < 0 || i >= len(sysctlSpec) {
		return 0, false
	}
	return *s.armorPtrs()[i], true
}

// SetArmorByIndex writes the admin-tunable value for a TUI row index.
// CONTRACT: вызывать ТОЛЬКО на клоне внутри config.Update — снепшот неизменяем.
func (s *SysctlCoreConfig) SetArmorByIndex(i int, v int) bool {
	if i < 0 || i >= len(sysctlSpec) {
		return false
	}
	*s.armorPtrs()[i] = v
	return true
}

// ValidateSysctlCore syncs Linux baselines and clamps Armor values to bounds.
// CONTRACT: отчёт — машинные токены "param=old->new", без естественного языка.
// SIDE-EFFECTS: мутирует переданную структуру (вызывать на local/клоне).
func ValidateSysctlCore(s *SysctlCoreConfig) (reports []string, changed bool) {
	armor, base := s.armorPtrs(), s.baselinePtrs()
	for i, spec := range sysctlSpec {
		// Baseline — константа спеки, границы к нему не применяются.
		if *base[i] != spec.Baseline {
			*base[i] = spec.Baseline
			changed = true
		}
		// Armor — декларация админа, обязана пройти ИБ-границы.
		if v := *armor[i]; v < spec.Min || v > spec.Max {
			*armor[i] = spec.Fallback
			reports = append(reports, fmt.Sprintf("%s=%d->%d", spec.ParamName, v, spec.Fallback))
			changed = true
		}
	}
	return reports, changed
}
