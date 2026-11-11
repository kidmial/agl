// config/sysctl_spec_test.go
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ============================================================================
// ИНВАРИАНТЫ СПЕКИ ПАРАМЕТРОВ ЯДРА
// ============================================================================
// PURPOSE: спека — единственный источник чисел. Если она внутренне
// противоречива, баг воспроизводится во всех потребителях сразу.
// CONTRACT: тесты герметичны — не трогают /proc/sys, UFW, systemd и
// реальные пути DirLib/DirRun (они константы, подменить их нельзя).
// ============================================================================

// TestSysctlSpec_FactoryWithinBounds — эталон AGL ОБЯЗАН проходить свои границы.
//
// Исторический баг: RESET копировал Default* (Linux) в Armor*, а
// DefaultNetIpv4TcpSynRetries=6 выходил за Armor-границу Max=5.
// Валидатор молча клампил его в 2 → диск и ОЗУ расходились.
func TestSysctlSpec_FactoryWithinBounds(t *testing.T) {
	for i, s := range sysctlSpec {
		if s.Factory < s.Min || s.Factory > s.Max {
			t.Errorf("[%d] %s: Factory=%d вне границ [%d..%d] — RESET сломается",
				i, s.ParamName, s.Factory, s.Min, s.Max)
		}
	}
}

// TestSysctlSpec_FallbackWithinBounds — значение аварийного отката тоже в границах.
// Иначе кламп породит второе недопустимое значение и зациклит правку.
func TestSysctlSpec_FallbackWithinBounds(t *testing.T) {
	for i, s := range sysctlSpec {
		if s.Fallback < s.Min || s.Fallback > s.Max {
			t.Errorf("[%d] %s: Fallback=%d вне границ [%d..%d]",
				i, s.ParamName, s.Fallback, s.Min, s.Max)
		}
	}
}

// TestSysctlSpec_BoundsSane — сами границы не вывернуты наизнанку.
func TestSysctlSpec_BoundsSane(t *testing.T) {
	for i, s := range sysctlSpec {
		if s.Min > s.Max {
			t.Errorf("[%d] %s: Min=%d > Max=%d", i, s.ParamName, s.Min, s.Max)
		}
	}
}

// TestSysctlSpec_BaselineMayExceedBounds — ФИКСАЦИЯ КОНТРАКТА, не баг.
//
// Baseline — честный стандарт чистого Linux. Границы Armor к нему
// НЕ применяются: syn_retries=6 законен для ядра и незаконен для брони.
// Если этот тест упадёт — кто-то попытался «подогнать» Baseline под Armor.
// Так делать нельзя: ARMOR_OFF перестанет возвращать реальное состояние ОС.
func TestSysctlSpec_BaselineMayExceedBounds(t *testing.T) {
	var s SysctlParamSpec
	for _, spec := range sysctlSpec {
		if spec.ParamName == "net.ipv4.tcp_syn_retries" {
			s = spec
		}
	}
	if s.ParamName == "" {
		t.Fatal("net.ipv4.tcp_syn_retries исчез из спеки")
	}
	if s.Baseline <= s.Max {
		t.Errorf("Baseline=%d больше не выходит за Max=%d — контракт размыт",
			s.Baseline, s.Max)
	}
}

// TestFactorySysctlCore_MapsBothHalves — генератор раскладывает оба набора.
// Factory → Armor*, Baseline → Default*. Перепутанные половины = баг RESET.
func TestFactorySysctlCore_MapsBothHalves(t *testing.T) {
	c := FactorySysctlCore()
	armor, base := c.armorPtrs(), c.baselinePtrs()
	for i, s := range sysctlSpec {
		if *armor[i] != s.Factory {
			t.Errorf("[%d] %s: Armor=%d, ожидали Factory=%d", i, s.ParamName, *armor[i], s.Factory)
		}
		if *base[i] != s.Baseline {
			t.Errorf("[%d] %s: Default=%d, ожидали Baseline=%d", i, s.ParamName, *base[i], s.Baseline)
		}
	}
}

// TestArmorByIndex_RoundTripAndGuards — чтение/запись по индексу + защита границ.
// Индекс вне диапазона обязан вернуть false, а не паниковать: роутер TUI
// может передать номер служебной строки (RESET/BACK).
func TestArmorByIndex_RoundTripAndGuards(t *testing.T) {
	c := FactorySysctlCore()
	for i := range sysctlSpec {
		want := sysctlSpec[i].Min // заведомо валидное отличие от Factory
		if !c.SetArmorByIndex(i, want) {
			t.Fatalf("SetArmorByIndex(%d) вернул false", i)
		}
		got, ok := c.ArmorByIndex(i)
		if !ok || got != want {
			t.Errorf("индекс %d: получили (%d,%v), ожидали (%d,true)", i, got, ok, want)
		}
	}
	for _, bad := range []int{-1, len(sysctlSpec), 999} {
		if _, ok := c.ArmorByIndex(bad); ok {
			t.Errorf("ArmorByIndex(%d): ok=true вне диапазона", bad)
		}
		if c.SetArmorByIndex(bad, 1) {
			t.Errorf("SetArmorByIndex(%d): запись вне диапазона прошла", bad)
		}
	}
}

// TestValidateSysctlCore_ClampAndReport — кламп, отчёт, флаг изменения.
func TestValidateSysctlCore_ClampAndReport(t *testing.T) {
	c := FactorySysctlCore()
	c.ArmorNetIpv4TcpSynRetries = 99 // выше Max=5 → Fallback=2
	c.ArmorNetCoreSomaxconn = 1      // ниже Min=128 → Fallback=2048

	reports, changed := ValidateSysctlCore(&c)
	if !changed {
		t.Fatal("changed=false при двух недопустимых значениях")
	}
	if c.ArmorNetIpv4TcpSynRetries != 2 {
		t.Errorf("syn_retries=%d, ожидали 2", c.ArmorNetIpv4TcpSynRetries)
	}
	if c.ArmorNetCoreSomaxconn != 2048 {
		t.Errorf("somaxconn=%d, ожидали 2048", c.ArmorNetCoreSomaxconn)
	}
	if len(reports) != 2 {
		t.Fatalf("отчётов %d, ожидали 2: %v", len(reports), reports)
	}
	// Отчёт — машинные токены без естественного языка (i18n-безопасно).
	joined := strings.Join(reports, " ")
	if !strings.Contains(joined, "net.ipv4.tcp_syn_retries=99->2") {
		t.Errorf("нет токена клампа syn_retries: %v", reports)
	}
}

// TestValidateSysctlCore_Idempotent — повторный прогон ничего не меняет.
// Критично: LoadConfig персистит при changed=true. Неидемпотентность
// заставила бы вотчер писать config.json каждые 60 секунд.
func TestValidateSysctlCore_Idempotent(t *testing.T) {
	c := FactorySysctlCore()
	if _, changed := ValidateSysctlCore(&c); changed {
		t.Fatal("первый прогон эталона сообщил об изменениях")
	}
	reports, changed := ValidateSysctlCore(&c)
	if changed || len(reports) != 0 {
		t.Errorf("повторный прогон: changed=%v reports=%v", changed, reports)
	}
}

// TestValidateSysctlCore_ResyncsBaseline — Default* восстанавливаются из спеки.
// Даже если админ вручную вписал мусор в default_-поля config.json.
func TestValidateSysctlCore_ResyncsBaseline(t *testing.T) {
	c := FactorySysctlCore()
	c.DefaultNetIpv4TcpFinTimeout = 999
	reports, changed := ValidateSysctlCore(&c)
	if !changed {
		t.Error("changed=false при испорченном Baseline")
	}
	if c.DefaultNetIpv4TcpFinTimeout != 60 {
		t.Errorf("Baseline fin_timeout=%d, ожидали 60", c.DefaultNetIpv4TcpFinTimeout)
	}
	// Baseline не клампится по Armor-границам → отчётов быть не должно.
	if len(reports) != 0 {
		t.Errorf("Baseline не должен порождать отчёт клампа: %v", reports)
	}
}

// TestSaveConfigLocked_RoundTrip — атомарная запись читается обратно.
// saveConfigLocked принимает путь явно → тест герметичен через t.TempDir().
func TestSaveConfigLocked_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	c := FactorySysctlCore()
	c.ArmorNetIpv4TcpFinTimeout = 30
	cfg := &Config{Lang: "ru", RealSshPort: 2244, SysctlCore: c}

	if err := saveConfigLocked(path, cfg); err != nil {
		t.Fatalf("запись не удалась: %v", err)
	}
	loaded, err := readConfigForTest(path)
	if err != nil {
		t.Fatalf("чтение не удалось: %v", err)
	}
	if loaded.SysctlCore.ArmorNetIpv4TcpFinTimeout != 30 {
		t.Errorf("fin_timeout=%d после round-trip, ожидали 30",
			loaded.SysctlCore.ArmorNetIpv4TcpFinTimeout)
	}
}

// TestUpdate_DoesNotMutatePublishedSnapshot — контракт клона.
// Старый снепшот обязан остаться нетронутым: читатели держат на него указатель.
func TestUpdate_DoesNotMutatePublishedSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	saved := C
	defer func() { C = saved }() // возвращаем глобальный синглтон как был

	base := FactorySysctlCore()
	C = &Config{SysctlCore: base}
	old := C // указатель на опубликованный снепшот

	if err := Update(path, func(c *Config) {
		c.SysctlCore.SetArmorByIndex(1, 45) // fin_timeout → 45
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if old.SysctlCore.ArmorNetIpv4TcpFinTimeout != base.ArmorNetIpv4TcpFinTimeout {
		t.Error("Update мутировал опубликованный снепшот — контракт клона нарушен")
	}
	if C.SysctlCore.ArmorNetIpv4TcpFinTimeout != 45 {
		t.Errorf("новый снепшот: fin_timeout=%d, ожидали 45",
			C.SysctlCore.ArmorNetIpv4TcpFinTimeout)
	}
	if C == old {
		t.Error("Update не опубликовал новый указатель")
	}
}

// readConfigForTest — минимальный читатель JSON для round-trip проверки.
// PURPOSE: не звать LoadConfig — он создаёт DirLib/DirRun (константы),
// что нарушило бы герметичность теста и потребовало root.
func readConfigForTest(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var c Config
	if err := json.NewDecoder(f).Decode(&c); err != nil {
		return nil, err
	}
	return &c, nil
}
