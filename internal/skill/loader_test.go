package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSkill(t *testing.T, root, dir, name, desc, body string) {
	t.Helper()
	d := filepath.Join(root, dir)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := "---\nname: " + name + "\ndescription: " + desc + "\n---\n" + body
	if err := os.WriteFile(filepath.Join(d, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestLoaderDiscoversAndLoads(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "waf", "waf-investigation", "WAF 拦截排查", "第一步查 query_waf_logs。")

	l := New(root)
	skills := l.List()
	if len(skills) != 1 {
		t.Fatalf("List = %d skills, want 1", len(skills))
	}
	if skills[0].Name != "waf-investigation" || skills[0].Description != "WAF 拦截排查" {
		t.Errorf("unexpected skill: %+v", skills[0])
	}
	got, err := l.Load("waf-investigation")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(got.Body, "query_waf_logs") {
		t.Errorf("body = %q, want query_waf_logs", got.Body)
	}
	if _, err := l.Load("missing"); err == nil {
		t.Error("Load(missing) expected error")
	}
}

func TestLoaderMissingDirIsEmpty(t *testing.T) {
	l := New(filepath.Join(t.TempDir(), "nope"))
	if len(l.List()) != 0 {
		t.Errorf("List = %v, want empty", l.List())
	}
	if s := l.Summary(); s != "" {
		t.Errorf("Summary = %q, want empty", s)
	}
}

func TestLoaderSummarySorted(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "b", "b-skill", "第二个", "body")
	writeSkill(t, root, "a", "a-skill", "第一个", "body")

	l := New(root)
	sum := l.Summary()
	if !strings.Contains(sum, "- a-skill:") || !strings.Contains(sum, "- b-skill:") {
		t.Errorf("Summary missing entries: %q", sum)
	}
	if strings.Index(sum, "a-skill") > strings.Index(sum, "b-skill") {
		t.Errorf("Summary not sorted: %q", sum)
	}
}
