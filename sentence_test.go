package jjl

import (
	"strings"
	"testing"
)

func TestSplit(t *testing.T) {
	doc := strings.Join([]string{
		"---",
		"title: テスト",
		"---",
		"",
		"# 見出し",
		"",
		"最初の文である。二つ目の文だ！三つ目はどうか？",
		"- 箇条書きの文である。",
		"```",
		"code。code。",
		"```",
		"![alt](img.png)",
		"[リンク](https://example.com)を含む文である。",
		"短い。",
		"これはですます調の文です。",
		"「ジャンルは？」って聞くと答えが返る。",
		"思ったことは、",
		"- 箇条書きへ続く導入は判定しない。",
	}, "\n")
	src := []byte(doc)
	got := Split(src, Markdown)
	want := []string{
		"最初の文である。", "二つ目の文だ！", "三つ目はどうか？",
		"箇条書きの文である。",
		"リンクを含む文である。",
		"これはですます調の文です。",
		"「ジャンルは？」って聞くと答えが返る。",
		"箇条書きへ続く導入は判定しない。",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d sentences, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Text != w {
			t.Errorf("[%d] text = %q, want %q", i, got[i].Text, w)
		}
	}
	if s := string(src[got[1].Start.Offset:got[1].End.Offset]); s != "二つ目の文だ！" {
		t.Errorf("offset of [1] points to %q", s)
	}
	if s := string(src[got[3].Start.Offset:got[3].End.Offset]); s != "箇条書きの文である。" {
		t.Errorf("offset of [3] points to %q", s)
	}
	if got[1].Start.Line != 7 || got[3].Start.Line != 8 {
		t.Errorf("lines = %d, %d", got[1].Start.Line, got[3].Start.Line)
	}
	// 行頭からの byte 列 / rune 列
	if got[1].Start.Column != len("最初の文である。")+1 {
		t.Errorf("byte column of [1] = %d", got[1].Start.Column)
	}
	if c := RuneColumn(src, got[1].Start); c != 9 {
		t.Errorf("rune column of [1] = %d, want 9", c)
	}
	if c := RuneColumn(src, got[3].Start); c != 3 {
		t.Errorf("rune column of [3] = %d, want 3", c)
	}
	if got[1].Prev != "最初の文である。" || got[1].Next != "三つ目はどうか？" {
		t.Errorf("context of [1] = %q / %q", got[1].Prev, got[1].Next)
	}
	// UTF-16 オフセット (BMP のみなので rune 数と一致)
	runes := []rune(doc)
	from, to := UTF16Offset(src, got[6].Start.Offset), UTF16Offset(src, got[6].End.Offset)
	if s := string(runes[from:to]); s != "「ジャンルは？」って聞くと答えが返る。" {
		t.Errorf("utf16 offset points to %q", s)
	}
}

func TestSplitText(t *testing.T) {
	got := Split([]byte("---\n# これは見出しではない。\n- 箇条書きでもない。"), Text)
	want := []string{"# これは見出しではない。", "- 箇条書きでもない。"}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i, w := range want {
		if got[i].Text != w {
			t.Errorf("[%d] = %q, want %q", i, got[i].Text, w)
		}
	}
}

func TestRegexChecks(t *testing.T) {
	cfg := DefaultConfig()
	find := func(key string) *RegexCheck {
		for i := range cfg.RegexChecks {
			if cfg.RegexChecks[i].Key == key {
				return &cfg.RegexChecks[i]
			}
		}
		t.Fatalf("no regex check %q", key)
		return nil
	}
	re, _ := find("desumasu").Regexp()
	if !re.MatchString("これはですます調の文です。") || re.MatchString("最初の文である。") {
		t.Errorf("desumasu detection wrong")
	}
	ga := find("double_ga")
	gre, _ := ga.Regexp()
	if n := len(gre.FindAllStringIndex("詳しくは分からないが、どうやら LLM ではあるが、テキストは出せない。", -1)); n < ga.Min {
		t.Errorf("double_ga: got %d matches", n)
	}
	if n := len(gre.FindAllStringIndex("分からないが、試してみた。", -1)); n >= ga.Min {
		t.Errorf("double_ga false positive: %d", n)
	}
}
