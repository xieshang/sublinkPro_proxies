package scheduler

import (
	"context"
	"testing"
	"time"
)

func TestDefaultSpeedTestConfig(t *testing.T) {
	cfg := DefaultSpeedTestConfig()
	if cfg == nil {
		t.Fatal("expected config")
	}
	if cfg.Timeout <= 0 {
		t.Fatalf("timeout should be > 0, got %v", cfg.Timeout)
	}
	if cfg.LatencyTestURL == "" || cfg.SpeedTestURL == "" {
		t.Fatal("urls should not be empty")
	}
}

func TestRunLinkTestsEmpty(t *testing.T) {
	sum := RunLinkTests(context.Background(), nil, DefaultSpeedTestConfig(), LinkTestModeDelay, nil)
	if sum.Total != 0 || sum.Success != 0 || sum.Failed != 0 {
		t.Fatalf("unexpected summary: %+v", sum)
	}
}

func TestNormalizeLinkTestConfigDefaults(t *testing.T) {
	cfg := normalizeLinkTestConfig(&SpeedTestConfig{Timeout: 0})
	if cfg.Timeout != 8*time.Second {
		t.Fatalf("expected default timeout 8s, got %v", cfg.Timeout)
	}
	if cfg.SpeedRecordMode != "average" {
		t.Fatalf("expected average mode, got %s", cfg.SpeedRecordMode)
	}
}

func TestResolveFixedConcurrency(t *testing.T) {
	if got := resolveFixedConcurrency(0, 20, 1000); got != 20 {
		t.Fatalf("fallback expected 20, got %d", got)
	}
	if got := resolveFixedConcurrency(50, 5, 32); got != 32 {
		t.Fatalf("max clamp expected 32, got %d", got)
	}
	if got := resolveFixedConcurrency(8, 5, 32); got != 8 {
		t.Fatalf("configured expected 8, got %d", got)
	}
}
