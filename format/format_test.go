package format

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	jjl "github.com/pankona/japanese-jev-lint"
)

func sample() Input {
	src := []byte("見出し\n正しい文である。ですます調の文です。\n")
	sents := jjl.Split(src, jjl.Text)
	res := jjl.Result{Path: "a.md", Config: jjl.DefaultConfig()}
	for _, s := range sents {
		it := jjl.Item{Sentence: s, Flags: []string{}}
		if strings.HasSuffix(s.Text, "です。") {
			it.Flags = []string{"desumasu"}
		}
		res.Items = append(res.Items, it)
	}
	return Input{Result: res, Src: src}
}

func TestText(t *testing.T) {
	var b bytes.Buffer
	if err := Text(&b, []Input{sample()}); err != nil {
		t.Fatal(err)
	}
	want := "a.md:2:" + itoa(len("正しい文である。")+1) + ": ですます調 [desumasu]\n"
	if b.String() != want {
		t.Errorf("got %q, want %q", b.String(), want)
	}
}

func TestRDJSONL(t *testing.T) {
	var b bytes.Buffer
	if err := RDJSONL(&b, []Input{sample()}); err != nil {
		t.Fatal(err)
	}
	var rec map[string]any
	if err := json.Unmarshal(b.Bytes(), &rec); err != nil {
		t.Fatal(err, b.String())
	}
	loc := rec["location"].(map[string]any)
	if loc["path"] != "a.md" || rec["severity"] != "WARNING" {
		t.Errorf("rec = %v", rec)
	}
}

func TestTextlint(t *testing.T) {
	var b bytes.Buffer
	if err := Textlint(&b, []Input{sample()}); err != nil {
		t.Fatal(err)
	}
	var files []struct {
		FilePath string `json:"filePath"`
		Messages []struct {
			RuleID string `json:"ruleId"`
			Line   int    `json:"line"`
			Column int    `json:"column"`
			Index  int    `json:"index"`
			Range  [2]int `json:"range"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(b.Bytes(), &files); err != nil {
		t.Fatal(err)
	}
	m := files[0].Messages[0]
	// 文字数ベース: 2 行目の 9 文字目、先頭からは「見出し\n」(4) + 8 = 12
	if m.RuleID != "jjl/desumasu" || m.Line != 2 || m.Column != 9 || m.Index != 12 || m.Range != [2]int{12, 12 + len([]rune("ですます調の文です。"))} {
		t.Errorf("message = %+v", m)
	}
}

func itoa(n int) string { return strconv.Itoa(n) }
