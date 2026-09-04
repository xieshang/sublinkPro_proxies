package updater

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	m := GetManager()
	m.SetBaseDir(t.TempDir())
	m.SetAppVersion("v1.0.0")
	t.Cleanup(func() {
		m.SetBaseDir(filepath.Join(".", "db", "updater"))
	})
	return m
}

func TestConfigRoundTripAndValidate(t *testing.T) {
	m := newTestManager(t)

	cfg, err := m.LoadConfig()
	if err != nil {
		t.Fatalf("默认配置加载失败: %v", err)
	}
	if cfg.KeepArtifacts != 10 {
		t.Errorf("默认保留数量 = %d, want 10", cfg.KeepArtifacts)
	}

	// 缺地址校验
	bad := &UpdaterConfig{SourceType: SourceManifest}
	if err := m.SaveConfig(bad); err == nil {
		t.Error("清单模式缺地址应报错")
	}

	cfg.SourceType = SourceManifest
	cfg.ManifestURL = "https://example.com/versions.json"
	if err := m.SaveConfig(cfg); err != nil {
		t.Fatalf("保存配置失败: %v", err)
	}
	reloaded, err := m.LoadConfig()
	if err != nil {
		t.Fatalf("重载配置失败: %v", err)
	}
	if reloaded.ManifestURL != cfg.ManifestURL || reloaded.SourceType != SourceManifest {
		t.Errorf("round trip 不一致: %+v", reloaded)
	}
}

func TestLedgerSaveLoadAndPrune(t *testing.T) {
	m := newTestManager(t)
	if err := m.ensureDirs(); err != nil {
		t.Fatal(err)
	}
	// 保留上限压到 3，便于触发淘汰
	if err := m.SaveConfig(&UpdaterConfig{SourceType: SourceTemplate, TemplateURL: "https://x/{os}{ext}", KeepArtifacts: 3}); err != nil {
		t.Fatal(err)
	}

	ledger, err := m.LoadLedger()
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().Add(-time.Hour)
	for i := 0; i < 5; i++ {
		a := &Artifact{
			ID:        fmt.Sprintf("v1.%d.0-windows-amd64", i),
			Version:   fmt.Sprintf("v1.%d.0", i),
			OS:        "windows",
			Arch:      "amd64",
			FileName:  fmt.Sprintf("v1.%d.0-windows-amd64.exe", i),
			Status:    ArtifactStatusBackup,
			CreatedAt: base.Add(time.Duration(i) * time.Minute),
		}
		path := filepath.Join(m.artifactDir(), a.FileName)
		if err := os.WriteFile(path, []byte("binary"), 0o755); err != nil {
			t.Fatal(err)
		}
		ledger.Artifacts = append(ledger.Artifacts, a)
	}
	if err := m.SaveLedger(ledger); err != nil {
		t.Fatalf("保存账本失败: %v", err)
	}

	reloaded, err := m.LoadLedger()
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Artifacts) != 3 {
		t.Fatalf("应淘汰到 3 条, got %d", len(reloaded.Artifacts))
	}
	for _, a := range reloaded.Artifacts {
		if a.Version == "v1.0.0" || a.Version == "v1.1.0" {
			t.Errorf("最旧的 %s 应被淘汰", a.Version)
		}
		// 对应文件应已删除
		if _, err := os.Stat(filepath.Join(m.artifactDir(), a.FileName)); err != nil {
			t.Errorf("保留记录的文件不应被删除: %v", err)
		}
	}
}

func TestPruneKeepsActive(t *testing.T) {
	m := newTestManager(t)
	if err := m.ensureDirs(); err != nil {
		t.Fatal(err)
	}
	if err := m.SaveConfig(&UpdaterConfig{SourceType: SourceTemplate, TemplateURL: "https://x/{os}{ext}", KeepArtifacts: 2}); err != nil {
		t.Fatal(err)
	}
	ledger, _ := m.LoadLedger()
	base := time.Now().Add(-time.Hour)

	active := &Artifact{ID: "active", Version: "9.9.9", OS: "any", Arch: "any", FileName: "active.bin", Status: ArtifactStatusActive, CreatedAt: base}
	old1 := &Artifact{ID: "old1", Version: "0.0.1", OS: "any", Arch: "any", FileName: "old1.bin", Status: ArtifactStatusBackup, CreatedAt: base.Add(time.Minute)}
	old2 := &Artifact{ID: "old2", Version: "0.0.2", OS: "any", Arch: "any", FileName: "old2.bin", Status: ArtifactStatusBackup, CreatedAt: base.Add(2 * time.Minute)}
	for _, a := range []*Artifact{active, old1, old2} {
		_ = os.WriteFile(filepath.Join(m.artifactDir(), a.FileName), []byte("x"), 0o755)
		ledger.Artifacts = append(ledger.Artifacts, a)
	}
	if err := m.SaveLedger(ledger); err != nil {
		t.Fatal(err)
	}
	reloaded, _ := m.LoadLedger()
	if len(reloaded.Artifacts) != 2 {
		t.Fatalf("应保留 2 条, got %d", len(reloaded.Artifacts))
	}
	foundActive := false
	for _, a := range reloaded.Artifacts {
		if a.ID == "active" {
			foundActive = true
		}
		if a.ID == "old1" {
			t.Error("最旧的 backup 应先被淘汰而不是 active")
		}
	}
	if !foundActive {
		t.Error("active 成品不允许被淘汰")
	}
}

func TestDeleteArtifactRules(t *testing.T) {
	m := newTestManager(t)
	if err := m.ensureDirs(); err != nil {
		t.Fatal(err)
	}
	ledger, _ := m.LoadLedger()
	now := time.Now()
	active := &Artifact{ID: "a1", Version: "1", OS: "any", Arch: "any", FileName: "a1.bin", Status: ArtifactStatusActive, CreatedAt: now}
	backup := &Artifact{ID: "b1", Version: "0", OS: "any", Arch: "any", FileName: "b1.bin", Status: ArtifactStatusBackup, CreatedAt: now}
	for _, a := range []*Artifact{active, backup} {
		_ = os.WriteFile(filepath.Join(m.artifactDir(), a.FileName), []byte("x"), 0o755)
		ledger.Artifacts = append(ledger.Artifacts, a)
	}
	_ = m.SaveLedger(ledger)

	if err := m.DeleteArtifact("a1"); err == nil {
		t.Error("删除 active 成品应被拒绝")
	}
	if err := m.DeleteArtifact("nope"); err == nil {
		t.Error("删除不存在的成品应报错")
	}
	if err := m.DeleteArtifact("b1"); err != nil {
		t.Fatalf("删除 backup 失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(m.artifactDir(), "b1.bin")); !os.IsNotExist(err) {
		t.Error("成品文件应被删除")
	}
	reloaded, _ := m.LoadLedger()
	if len(reloaded.Artifacts) != 1 {
		t.Errorf("账本应剩 1 条, got %d", len(reloaded.Artifacts))
	}
}

func TestSnapshotCurrent(t *testing.T) {
	m := newTestManager(t)
	if err := m.ensureDirs(); err != nil {
		t.Fatal(err)
	}
	ledger, _ := m.LoadLedger()
	// 当前运行的测试二进制即"正在运行的程序"，快照入库
	exe, err := os.Executable()
	if err != nil {
		t.Skipf("无法定位测试二进制: %v", err)
	}
	if fileSize(exe) == 0 {
		t.Skip("测试二进制大小异常，跳过")
	}
	if err := m.snapshotCurrent(ledger); err != nil {
		t.Fatalf("snapshotCurrent: %v", err)
	}
	if ledger.Current != "v1.0.0" {
		t.Errorf("Current = %q, want v1.0.0", ledger.Current)
	}
	id := ArtifactID("v1.0.0", DetectPlatform().OS, DetectPlatform().Arch)
	a := m.FindArtifact(ledger, id)
	if a == nil {
		t.Fatalf("快照成品 %s 未入库", id)
	}
	if a.Status != ArtifactStatusActive && a.Status != ArtifactStatusBackup {
		t.Errorf("快照状态异常: %q", a.Status)
	}
	full := filepath.Join(m.artifactDir(), a.FileName)
	if fileSize(full) != fileSize(exe) {
		t.Error("快照文件应与当前二进制等大")
	}
	// 二次快照不重复入库
	before := len(ledger.Artifacts)
	if err := m.snapshotCurrent(ledger); err != nil {
		t.Fatal(err)
	}
	if len(ledger.Artifacts) != before {
		t.Errorf("重复快照不应新增记录: %d → %d", before, len(ledger.Artifacts))
	}
}
