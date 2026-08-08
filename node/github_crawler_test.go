package node

import (
	"testing"
	"time"
)

func TestScoreFilePathFeaturePriority(t *testing.T) {
	cases := []struct {
		path string
		min  int
		max  int // inclusive upper bound optional; use 0 for no max
	}{
		{"clash.yaml", 80, 0},
		{"config/clash.yml", 80, 0},
		{"subscription.yaml", 80, 0},
		{"proxies.yml", 80, 0},
		{"nodes/sub.txt", 40, 0},
		{"free-nodes.yaml", 50, 0},
		{"mihomo/config.yaml", 70, 0},
		{"provider.yaml", 50, 0},
		{"README.md", -100, -100},
		{"NEWS", -100, -100},
		{"LICENSE", -100, -100},
		{"main.go", -100, -100},
		{"docs/changelog.md", -100, -100},
		{"todo.md", -100, -100},
		{"random.txt", 0, 40}, // extension only, no strong feature
	}
	for _, c := range cases {
		got := scoreFilePath(c.path)
		if got < c.min {
			t.Errorf("scoreFilePath(%q)=%d, want >= %d", c.path, got, c.min)
		}
		if c.max != 0 && got > c.max {
			t.Errorf("scoreFilePath(%q)=%d, want <= %d", c.path, got, c.max)
		}
		if c.max == -100 && got != -100 {
			t.Errorf("scoreFilePath(%q)=%d, want -100", c.path, got)
		}
	}
}

func TestScoreFilePathClashBeatsGeneric(t *testing.T) {
	clash := scoreFilePath("clash.yaml")
	generic := scoreFilePath("notes.txt")
	if clash <= generic {
		t.Fatalf("clash.yaml score %d should beat notes.txt %d", clash, generic)
	}
}

func TestContentLooksLikeSubscription(t *testing.T) {
	if !contentLooksLikeSubscription([]byte("proxies:\n  - name: a\n    type: ss\n    server: 1.2.3.4\n")) {
		t.Fatal("yaml proxies should match")
	}
	if !contentLooksLikeSubscription([]byte("vmess://abcdefg")) {
		t.Fatal("vmess link should match")
	}
	if contentLooksLikeSubscription([]byte("# Changelog\n- fix bug\n- update docs\n")) {
		t.Fatal("changelog should not match")
	}
}

func TestCandidateSortScoreThenRecency(t *testing.T) {
	now := time.Now()
	cands := []githubFileCandidate{
		{Path: "old-clash.yaml", Score: 90, UpdatedAt: now.Add(-48 * time.Hour)},
		{Path: "new-clash.yaml", Score: 90, UpdatedAt: now},
		{Path: "generic.txt", Score: 30, UpdatedAt: now},
	}
	// replicate sort used in crawler
	sortCandidates := func(out []githubFileCandidate) {
		for i := 0; i < len(out); i++ {
			for j := i + 1; j < len(out); j++ {
				swap := false
				if out[i].Score != out[j].Score {
					swap = out[i].Score < out[j].Score
				} else {
					swap = out[i].UpdatedAt.Before(out[j].UpdatedAt)
				}
				if swap {
					out[i], out[j] = out[j], out[i]
				}
			}
		}
	}
	sortCandidates(cands)
	if cands[0].Path != "new-clash.yaml" {
		t.Fatalf("want new-clash.yaml first, got %s", cands[0].Path)
	}
	if cands[1].Path != "old-clash.yaml" {
		t.Fatalf("want old-clash.yaml second, got %s", cands[1].Path)
	}
	if cands[2].Path != "generic.txt" {
		t.Fatalf("want generic.txt last, got %s", cands[2].Path)
	}
}

func TestExcludePathPattern(t *testing.T) {
	for _, p := range []string{"NEWS", "README.md", "LICENSE", "TODO.md", "src/main.go"} {
		if scoreFilePath(p) > -50 {
			t.Errorf("%s should be heavily penalized, got %d", p, scoreFilePath(p))
		}
	}
}
