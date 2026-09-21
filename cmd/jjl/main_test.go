package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunNoJev(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.md")
	os.WriteFile(p, []byte("正しい文である。\nですます調の文です。\n"), 0o644)
	var out, errb bytes.Buffer
	code := run([]string{"-no-jev", "-no-cache", p}, nil, &out, &errb)
	if code != 1 {
		t.Errorf("exit = %d, stderr = %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "a.md:2:1: ですます調 [desumasu]") {
		t.Errorf("out = %q", out.String())
	}
	out.Reset()
	if code := run([]string{"-no-jev", "-no-cache", "-fail-on", "none", "-disable", "desumasu", p}, nil, &out, &errb); code != 0 || out.Len() != 0 {
		t.Errorf("exit = %d, out = %q", code, out.String())
	}
}

func TestRunConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "jjl.yml")
	os.WriteFile(cfgPath, []byte(`
checks:
  - key: typo
    threshold: 0.8
regex_checks:
  - key: kana
    label: 全角カナ
    pattern: "[ア-ン]{3,}"
disable: [double_ga]
`), 0o644)
	var out, errb bytes.Buffer
	if code := run([]string{"-config", cfgPath, "-list-checks"}, nil, &out, &errb); code != 0 {
		t.Fatalf("exit = %d: %s", code, errb.String())
	}
	s := out.String()
	if !strings.Contains(s, "typo") || !strings.Contains(s, "threshold=0.80") || !strings.Contains(s, "全角カナ") || strings.Contains(s, "double_ga") {
		t.Errorf("list = %s", s)
	}
}

func TestDryRun(t *testing.T) {
	var out, errb bytes.Buffer
	code := run([]string{"-dry-run", "-"}, strings.NewReader("一つ目の文である。二つ目の文である。"), &out, &errb)
	if code != 0 || strings.Count(out.String(), "\n") != 2 || !strings.Contains(errb.String(), "2 文") {
		t.Errorf("exit = %d, out = %q, err = %q", code, out.String(), errb.String())
	}
}
