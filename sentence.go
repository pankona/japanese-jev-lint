// Package jjl (japanese-jev-lint) は jev (TypeSafe の System One モデル) を使って
// 日本語の文章を lint するライブラリ。文単位で「誤字?」「主述のねじれ?」などの
// 確率を jev に聞き、閾値を超えた文を診断 (Diagnostic) として返す。
//
// jev は文章を生成せず確率だけを返すので、このパッケージも指摘文は作らない。
// 原稿本文が api.typesafe.ai へ送られる点に注意。
package jjl

import (
	"regexp"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// Syntax は入力の書式。文の分割で飛ばす行 (front matter、コードブロック等) が変わる。
type Syntax string

const (
	Markdown Syntax = "markdown"
	Text     Syntax = "text"
)

// Position は原稿内の位置。Offset は byte、Line と Column は 1 始まりで
// Column は行頭からの byte 数 (reviewdog の rdjson と同じ流儀)。
// 文字数ベースの列や UTF-16 オフセットが必要なら RuneColumn / UTF16Offset を使う。
type Position struct {
	Offset int `json:"offset"`
	Line   int `json:"line"`
	Column int `json:"column"`
}

// Sentence は分割後の一文。Text は Markdown のリンク・画像・コードスパンを
// 剥がした判定用の本文で、Start..End は原稿上の元の範囲 (剥がす前) を指す。
type Sentence struct {
	Start Position
	End   Position
	Text  string
	Prev  string // 直前の文 (jev に文脈として渡す)
	Next  string
}

var (
	reFence   = regexp.MustCompile("^\\s*(```|~~~)")
	reSkip    = regexp.MustCompile(`^\s*(#|!\[|<!--|\[[^\]]*\]:\s|\||{{<|{{%|---\s*$|https?://\S+\s*$)`)
	rePrefix  = regexp.MustCompile(`^(\s*([-*+]|\d+\.)\s+|\s*>\s*)+`)
	reInline  = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)|\[([^\]]+)\]\([^)]*\)|` + "`[^`]*`")
	reEndMark = regexp.MustCompile(`[。！？!?]`)
)

// minSentenceRunes より短い断片は jev に渡さない (見出し語や記号だけの行など)
const minSentenceRunes = 6

// Split は原稿を文に割る。Markdown では front matter、コードブロック、見出し、
// 画像行、表、Hugo shortcode などを飛ばす。読点やコロンで終わる導入行
// (箇条書きへ続くもの) は文として不完全なのが正常なので除く。
func Split(src []byte, syntax Syntax) []Sentence {
	var out []Sentence
	doc := string(src)
	off := 0 // 現在行の先頭 byte オフセット
	inFence, inFront := false, false
	lines := strings.Split(doc, "\n")
	for i, line := range lines {
		lineStart := off
		lineNo := i + 1
		off += len(line) + 1
		if syntax == Markdown {
			if i == 0 && strings.TrimSpace(line) == "---" {
				inFront = true
				continue
			}
			if inFront {
				if strings.TrimSpace(line) == "---" {
					inFront = false
				}
				continue
			}
			if reFence.MatchString(line) {
				inFence = !inFence
				continue
			}
			if inFence || reSkip.MatchString(line) {
				continue
			}
		}
		body := line
		start := 0
		if syntax == Markdown {
			if m := rePrefix.FindStringIndex(line); m != nil {
				body, start = line[m[1]:], m[1]
			}
		}
		// 文末記号で区切る。記号の直後で切り、引用符閉じ等は次の文の先頭に残る
		// 程度の雑さで良しとする (位置が数文字ずれるだけ)
		segStart := 0
		flush := func(end int) {
			seg := body[segStart:end]
			trimmed := strings.TrimSpace(seg)
			if trimmed != "" {
				lead := strings.Index(seg, trimmed)
				plain := trimmed
				if syntax == Markdown {
					plain = strings.TrimSpace(reInline.ReplaceAllString(trimmed, "$1"))
				}
				if utf8.RuneCountInString(plain) >= minSentenceRunes && !strings.HasSuffix(plain, "、") &&
					!strings.HasSuffix(plain, "：") && !strings.HasSuffix(plain, ":") {
					col := start + segStart + lead // 行頭からの byte 数
					out = append(out, Sentence{
						Start: Position{Offset: lineStart + col, Line: lineNo, Column: col + 1},
						End:   Position{Offset: lineStart + col + len(trimmed), Line: lineNo, Column: col + len(trimmed) + 1},
						Text:  plain,
					})
				}
			}
			segStart = end
		}
		for _, m := range reEndMark.FindAllStringIndex(body, -1) {
			// 「」や () の中の ？！ は文末ではない (「〜は？」って質問して、など)
			if bracketDepth(body[segStart:m[0]]) == 0 {
				flush(m[1])
			}
		}
		flush(len(body))
	}
	for i := range out {
		if i > 0 {
			out[i].Prev = out[i-1].Text
		}
		if i+1 < len(out) {
			out[i].Next = out[i+1].Text
		}
	}
	return out
}

// bracketDepth は s の中で開いたまま閉じていない括弧の数を返す
func bracketDepth(s string) int {
	d := 0
	for _, r := range s {
		switch r {
		case '「', '『', '（', '(', '"', '“':
			d++
		case '」', '』', '）', ')', '”':
			if d > 0 {
				d--
			}
		}
	}
	return d
}

// RuneColumn は pos の列を文字数 (rune) ベースの 1 始まりで返す (textlint の流儀)。
func RuneColumn(src []byte, pos Position) int {
	lineStart := pos.Offset - (pos.Column - 1)
	if lineStart < 0 || pos.Offset > len(src) {
		return pos.Column
	}
	return utf8.RuneCount(src[lineStart:pos.Offset]) + 1
}

// UTF16Offset は byte オフセットを UTF-16 コード単位のオフセットに変換する
// (CodeMirror など JavaScript 側の文字列位置と合わせるため)。
func UTF16Offset(src []byte, byteOff int) int {
	if byteOff > len(src) {
		byteOff = len(src)
	}
	n := 0
	for _, r := range string(src[:byteOff]) {
		n += utf16.RuneLen(r)
	}
	return n
}
