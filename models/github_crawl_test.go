package models

import (
	"testing"

	"sublink/database"
	"sublink/internal/testutil"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func setupGitHubCrawlDeleteTestDB(t *testing.T) {
	t.Helper()

	oldDB := database.DB
	oldDialect := database.Dialect
	oldInitialized := database.IsInitialized

	db, err := gorm.Open(sqlite.Open(testutil.UniqueMemoryDSN(t, "github_crawl_delete_test")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := db.AutoMigrate(&Node{}, &GitHubCrawlNode{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	// BatchDel cleans subscription associations; ensure the join table exists.
	if err := db.Exec(`CREATE TABLE IF NOT EXISTS subcription_nodes (
		subcription_id INTEGER,
		node_id INTEGER,
		sort INTEGER DEFAULT 0
	)`).Error; err != nil {
		t.Fatalf("create subcription_nodes: %v", err)
	}

	database.DB = db
	database.Dialect = database.DialectSQLite
	database.IsInitialized = false
	resetNodeCacheForTest()

	t.Cleanup(func() {
		database.DB = oldDB
		database.Dialect = oldDialect
		database.IsInitialized = oldInitialized
		resetNodeCacheForTest()
		testutil.CloseDB(t, db)
	})
}

func TestDeleteInvalidGitHubCrawlNodesAlsoRemovesFromTotalList(t *testing.T) {
	setupGitHubCrawlDeleteTestDB(t)

	configID := 11

	// Total-list node created from github-crawl promote
	totalPromoted := createNodeForBatchUpdateTest(t, Node{
		Name:     "gh-promoted",
		LinkName: "gh-promoted",
		Link:     "ss://invalid-promoted",
		Source:   "github-crawl",
		SourceID: configID,
		Group:    "github",
	})
	// Total-list node matched by link only (promoted_node_id missing)
	totalByLink := createNodeForBatchUpdateTest(t, Node{
		Name:     "gh-by-link",
		LinkName: "gh-by-link",
		Link:     "ss://invalid-by-link",
		Source:   "github-crawl",
		SourceID: configID,
		Group:    "github",
	})
	// Unrelated total node must remain
	keepNode := createNodeForBatchUpdateTest(t, Node{
		Name:     "keep-me",
		LinkName: "keep-me",
		Link:     "ss://keep-valid",
		Source:   "manual",
		SourceID: 0,
		Group:    "default",
	})

	invalidPromoted := GitHubCrawlNode{
		ConfigID:       configID,
		Link:           "ss://invalid-promoted",
		Name:           "gh-promoted",
		IsValid:        false,
		Promoted:       true,
		PromotedNodeID: totalPromoted.ID,
	}
	invalidByLink := GitHubCrawlNode{
		ConfigID: configID,
		Link:     "ss://invalid-by-link",
		Name:     "gh-by-link",
		IsValid:  false,
		Promoted: true,
	}
	validKeep := GitHubCrawlNode{
		ConfigID: configID,
		Link:     "ss://still-valid",
		Name:     "still-valid",
		IsValid:  true,
		Promoted: false,
	}
	if err := database.DB.Create(&invalidPromoted).Error; err != nil {
		t.Fatalf("create invalid promoted crawl node: %v", err)
	}
	if err := database.DB.Create(&invalidByLink).Error; err != nil {
		t.Fatalf("create invalid by-link crawl node: %v", err)
	}
	if err := database.DB.Create(&validKeep).Error; err != nil {
		t.Fatalf("create valid crawl node: %v", err)
	}

	crawlDeleted, totalDeleted, err := DeleteInvalidGitHubCrawlNodes(configID)
	if err != nil {
		t.Fatalf("DeleteInvalidGitHubCrawlNodes: %v", err)
	}
	if crawlDeleted != 2 {
		t.Fatalf("crawlDeleted = %d, want 2", crawlDeleted)
	}
	if totalDeleted != 2 {
		t.Fatalf("totalDeleted = %d, want 2", totalDeleted)
	}

	var remainCrawl int64
	if err := database.DB.Model(&GitHubCrawlNode{}).Where("config_id = ?", configID).Count(&remainCrawl).Error; err != nil {
		t.Fatalf("count crawl nodes: %v", err)
	}
	if remainCrawl != 1 {
		t.Fatalf("remaining crawl nodes = %d, want 1 (valid only)", remainCrawl)
	}

	var remainTotal int64
	if err := database.DB.Model(&Node{}).Count(&remainTotal).Error; err != nil {
		t.Fatalf("count total nodes: %v", err)
	}
	if remainTotal != 1 {
		t.Fatalf("remaining total nodes = %d, want 1", remainTotal)
	}
	var stillThere Node
	if err := database.DB.First(&stillThere, keepNode.ID).Error; err != nil {
		t.Fatalf("keep node should still exist: %v", err)
	}
	if stillThere.Name != "keep-me" {
		t.Fatalf("unexpected remaining node name %q", stillThere.Name)
	}

	// Ensure deleted total nodes are gone
	var gone Node
	if err := database.DB.First(&gone, totalPromoted.ID).Error; err == nil {
		t.Fatalf("promoted total node %d should be deleted", totalPromoted.ID)
	}
	if err := database.DB.First(&gone, totalByLink.ID).Error; err == nil {
		t.Fatalf("by-link total node %d should be deleted", totalByLink.ID)
	}
}

func setupGitHubCrawlLogTestDB(t *testing.T) {
	t.Helper()

	oldDB := database.DB
	oldDialect := database.Dialect
	oldInitialized := database.IsInitialized

	db, err := gorm.Open(sqlite.Open(testutil.UniqueMemoryDSN(t, "github_crawl_log_test")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := db.AutoMigrate(&GitHubCrawlLog{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
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

func TestAppendGitHubCrawlLogKeepsMax500(t *testing.T) {
	setupGitHubCrawlLogTestDB(t)

	const configID = 42
	// 写入超过上限
	for i := 0; i < githubCrawlLogMaxKeep+120; i++ {
		if err := AppendGitHubCrawlLog(1, configID, "info", "log-line"); err != nil {
			t.Fatalf("append log %d: %v", i, err)
		}
	}

	var count int64
	if err := database.DB.Model(&GitHubCrawlLog{}).Where("config_id = ?", configID).Count(&count).Error; err != nil {
		t.Fatalf("count logs: %v", err)
	}
	if count != int64(githubCrawlLogMaxKeep) {
		t.Fatalf("expected %d logs kept, got %d", githubCrawlLogMaxKeep, count)
	}

	list, err := ListGitHubCrawlLogs(0, configID, 0, githubCrawlLogMaxKeep)
	if err != nil {
		t.Fatalf("list logs: %v", err)
	}
	if len(list) != githubCrawlLogMaxKeep {
		t.Fatalf("list expected %d, got %d", githubCrawlLogMaxKeep, len(list))
	}
	// 升序且为最新一段
	if list[0].ID >= list[len(list)-1].ID {
		t.Fatalf("list should be ascending by id")
	}
}
