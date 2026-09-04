package models

import (
	"testing"

	"sublink/database"
	"sublink/internal/testutil"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// 测试「应用设置 → 节点自动处理」：连续失败计数维护与阈值删除
func TestUpdateConsecutiveFailures(t *testing.T) {
	setupNodeInfoBatchTestDB(t)

	seed := func(t *testing.T, link, name, group string) Node {
		t.Helper()
		n := Node{Link: link, Name: name, Group: group}
		n.syncLinkHash()
		if err := database.DB.Create(&n).Error; err != nil {
			t.Fatalf("seed node %q: %v", name, err)
		}
		nodeCache.Set(n.ID, n)
		return n
	}

	a := seed(t, "ss://auto-del-a", "a", "g1")
	b := seed(t, "ss://auto-del-b", "b", "g1")
	c := seed(t, "ss://auto-del-c", "c", "g2")

	// 两轮失败 + 一轮成功
	if err := UpdateConsecutiveFailures([]int{a.ID, c.ID}, nil); err != nil {
		t.Fatalf("round1: %v", err)
	}
	if err := UpdateConsecutiveFailures([]int{a.ID}, []int{c.ID}); err != nil {
		t.Fatalf("round2: %v", err)
	}

	var a2, b2, c2 Node
	database.DB.First(&a2, a.ID)
	database.DB.First(&b2, b.ID)
	database.DB.First(&c2, c.ID)
	if a2.ConsecutiveFailures != 2 {
		t.Fatalf("node a failures = %d, want 2", a2.ConsecutiveFailures)
	}
	if b2.ConsecutiveFailures != 0 {
		t.Fatalf("node b failures = %d, want 0 (未参与)", b2.ConsecutiveFailures)
	}
	if c2.ConsecutiveFailures != 0 {
		t.Fatalf("node c failures = %d, want 0 (成功清零)", c2.ConsecutiveFailures)
	}
}

func TestDeleteNodesOverFailureThreshold(t *testing.T) {
	setupNodeInfoBatchTestDB(t)
	// BatchDel 会清理 subcription_nodes 关联表，测试库需一并建表
	if err := database.DB.AutoMigrate(&SubcriptionNode{}); err != nil {
		t.Fatalf("auto migrate subcription_nodes: %v", err)
	}

	seed := func(t *testing.T, link, name, group string, failures int) Node {
		t.Helper()
		n := Node{Link: link, Name: name, Group: group}
		n.syncLinkHash()
		if err := database.DB.Create(&n).Error; err != nil {
			t.Fatalf("seed node %q: %v", name, err)
		}
		nodeCache.Set(n.ID, n)
		if err := database.DB.Model(&Node{}).Where("id = ?", n.ID).
			Update("consecutive_failures", failures).Error; err != nil {
			t.Fatalf("set failures %q: %v", name, err)
		}
		return n
	}

	dead1 := seed(t, "ss://dead-1", "dead1", "keep-group", 3)   // 达阈值但分组不在范围
	dead2 := seed(t, "ss://dead-2", "dead2", "purge-group", 3)  // 达阈值且分组命中
	alive := seed(t, "ss://alive-1", "alive", "purge-group", 1) // 未达阈值

	// 仅作用于 purge-group 分组
	names, err := DeleteNodesOverFailureThreshold(3, []string{"purge-group"})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(names) != 1 || names[0] != "dead2" {
		t.Fatalf("deleted names = %v, want [dead2]", names)
	}

	var count int64
	database.DB.Model(&Node{}).Where("id IN ?", []int{dead1.ID, dead2.ID}).Count(&count)
	if count != 1 {
		t.Fatalf("remaining count = %d, want 1（keep-group 的 dead1 不应被删）", count)
	}

	// 不限分组时 dead1 也应被删
	names, err = DeleteNodesOverFailureThreshold(3, nil)
	if err != nil {
		t.Fatalf("delete all groups: %v", err)
	}
	if len(names) != 1 || names[0] != "dead1" {
		t.Fatalf("deleted names = %v, want [dead1]", names)
	}
	_ = alive
}

// setupAutoDeleteSettingTestDB 准备仅含 SystemSetting 表的内存库（Get/SetSetting 依赖）
func setupAutoDeleteSettingTestDB(t *testing.T) {
	t.Helper()
	oldDB := database.DB
	oldDialect := database.Dialect
	oldInitialized := database.IsInitialized

	db, err := gorm.Open(sqlite.Open(testutil.UniqueMemoryDSN(t, "node_auto_delete_setting_test")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := db.AutoMigrate(&SystemSetting{}); err != nil {
		t.Fatalf("auto migrate system_settings: %v", err)
	}
	database.DB = db
	database.Dialect = database.DialectSQLite
	database.IsInitialized = false

	t.Cleanup(func() {
		database.DB = oldDB
		database.Dialect = oldDialect
		database.IsInitialized = oldInitialized
		testutil.CloseDB(t, db)
	})
}

func TestGetSaveNodeAutoDeleteConfig(t *testing.T) {
	setupAutoDeleteSettingTestDB(t)

	enabled, threshold, groups := GetNodeAutoDeleteConfig()
	if enabled || threshold != NodeAutoDeleteDefaultThreshold || len(groups) != 0 {
		t.Fatalf("default config = (%v,%d,%v), want (false,3,[])", enabled, threshold, groups)
	}

	if err := SaveNodeAutoDeleteConfig(true, 5, []string{"g1", " g2 "}); err != nil {
		t.Fatalf("save: %v", err)
	}
	enabled, threshold, groups = GetNodeAutoDeleteConfig()
	if !enabled || threshold != 5 || len(groups) != 2 || groups[1] != "g2" {
		t.Fatalf("saved config = (%v,%d,%v), want (true,5,[g1 g2])", enabled, threshold, groups)
	}
}
