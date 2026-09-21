package main

import (
	"fmt"
	"os"

	jjl "github.com/pankona/japanese-jev-lint"
	"gopkg.in/yaml.v3"
)

// fileConfig は .jjl.yml の形。checks / regex_checks は key ごとに既定へマージする
// (既定にある key はゼロ値でない項目だけ上書き、無い key は追加)。
type fileConfig struct {
	Model       string           `yaml:"model"`
	Checks      []jjl.Check      `yaml:"checks"`
	RegexChecks []jjl.RegexCheck `yaml:"regex_checks"`
	Disable     []string         `yaml:"disable"`
}

// loadConfig は path の設定を既定にマージして返す。path が空なら .jjl.yml を探し、
// 無ければ既定をそのまま返す。
func loadConfig(path string) (jjl.Config, error) {
	cfg := jjl.DefaultConfig()
	if path == "" {
		for _, p := range []string{".jjl.yml", ".jjl.yaml"} {
			if _, err := os.Stat(p); err == nil {
				path = p
				break
			}
		}
		if path == "" {
			return cfg, nil
		}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	var fc fileConfig
	if err := yaml.Unmarshal(b, &fc); err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	if fc.Model != "" {
		cfg.Model = fc.Model
	}
	for _, c := range fc.Checks {
		merged := false
		for i := range cfg.Checks {
			if cfg.Checks[i].Key != c.Key {
				continue
			}
			d := &cfg.Checks[i]
			if c.Label != "" {
				d.Label = c.Label
			}
			if c.Threshold != 0 {
				d.Threshold = c.Threshold
			}
			if c.MinRunes != 0 {
				d.MinRunes = c.MinRunes
			}
			if c.Question.Instructions != "" {
				d.Question.Instructions = c.Question.Instructions
			}
			if c.Question.True != "" {
				d.Question.True = c.Question.True
			}
			if c.Question.False != "" {
				d.Question.False = c.Question.False
			}
			merged = true
		}
		if !merged {
			if c.Key == "" || c.Question.Instructions == "" {
				return cfg, fmt.Errorf("%s: check needs key and question.instructions: %+v", path, c)
			}
			if c.Threshold == 0 {
				c.Threshold = 0.5
			}
			if c.Label == "" {
				c.Label = c.Key
			}
			cfg.Checks = append(cfg.Checks, c)
		}
	}
	for _, c := range fc.RegexChecks {
		merged := false
		for i := range cfg.RegexChecks {
			if cfg.RegexChecks[i].Key != c.Key {
				continue
			}
			d := &cfg.RegexChecks[i]
			if c.Label != "" {
				d.Label = c.Label
			}
			if c.Pattern != "" {
				*d = jjl.RegexCheck{Key: d.Key, Label: d.Label, Pattern: c.Pattern, Min: d.Min}
			}
			if c.Min != 0 {
				d.Min = c.Min
			}
			merged = true
		}
		if !merged {
			if c.Key == "" || c.Pattern == "" {
				return cfg, fmt.Errorf("%s: regex_check needs key and pattern: %+v", path, c)
			}
			if c.Min == 0 {
				c.Min = 1
			}
			if c.Label == "" {
				c.Label = c.Key
			}
			cfg.RegexChecks = append(cfg.RegexChecks, c)
		}
	}
	for i := range cfg.RegexChecks {
		if _, err := cfg.RegexChecks[i].Regexp(); err != nil {
			return cfg, fmt.Errorf("%s: regex_checks[%s]: %w", path, cfg.RegexChecks[i].Key, err)
		}
	}
	return cfg.Filter(nil, fc.Disable), nil
}
