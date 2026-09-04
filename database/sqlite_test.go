package database

import (
	"net/url"
	"strings"
	"testing"
)

// extractPragmas 从 DSN 中解析出全部 _pragma 值
func extractPragmas(dsn string) map[string]string {
	result := make(map[string]string)
	idx := strings.Index(dsn, "?")
	if idx < 0 {
		return result
	}
	values, err := url.ParseQuery(dsn[idx+1:])
	if err != nil {
		return result
	}
	for _, p := range values["_pragma"] {
		name := p
		value := ""
		if i := strings.Index(p, "("); i > 0 {
			name = strings.TrimSpace(p[:i])
			value = strings.TrimSuffix(p[i+1:], ")")
		}
		result[strings.ToLower(name)] = value
	}
	return result
}

func TestApplySQLitePragmas_AddsDefaults(t *testing.T) {
	dsn := applySQLitePragmas("/app/db/sublink.db")
	pragmas := extractPragmas(dsn)

	expected := map[string]string{
		"busy_timeout": "5000",
		"journal_mode": "WAL",
		"synchronous":  "NORMAL",
		"cache_size":   "-64000",
		"foreign_keys": "ON",
	}
	for name, want := range expected {
		got, ok := pragmas[name]
		if !ok {
			t.Errorf("缺少默认 pragma %s, dsn=%s", name, dsn)
			continue
		}
		if got != want {
			t.Errorf("pragma %s = %q, 期望 %q", name, got, want)
		}
	}
	if !strings.HasPrefix(dsn, "/app/db/sublink.db?") {
		t.Errorf("DSN 路径部分被破坏: %s", dsn)
	}
}

func TestApplySQLitePragmas_RespectsUserOverride(t *testing.T) {
	dsn := applySQLitePragmas("/app/db/sublink.db?_pragma=busy_timeout(10000)&_pragma=journal_mode(DELETE)")
	pragmas := extractPragmas(dsn)

	if got := pragmas["busy_timeout"]; got != "10000" {
		t.Errorf("用户自定义 busy_timeout 被覆盖: %q", got)
	}
	if got := pragmas["journal_mode"]; got != "DELETE" {
		t.Errorf("用户自定义 journal_mode 被覆盖: %q", got)
	}
	// 未指定的默认项仍应补齐
	if got := pragmas["synchronous"]; got != "NORMAL" {
		t.Errorf("未指定时应补齐 synchronous: %q", got)
	}
}

func TestApplySQLitePragmas_PreservesOtherParams(t *testing.T) {
	dsn := applySQLitePragmas("/app/db/sublink.db?_fk=1&mode=ro")
	if !strings.Contains(dsn, "_fk=1") || !strings.Contains(dsn, "mode=ro") {
		t.Errorf("原有参数丢失: %s", dsn)
	}
}
