// Package format は jjl の診断を既存の lint ツール互換の形式で書き出す。
//
//   - text:     path:line:col: message [rule]  (vim の quickfix、reviewdog -efm 向け。列は byte)
//   - rdjsonl:  reviewdog の Diagnostic を 1 行 1 JSON で
//   - textlint: textlint --format json と同じ形 (列・index は文字数ベース)
//   - json:     jjl 独自の JSON (診断とトークン数などの統計)
package format

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"

	jjl "github.com/pankona/japanese-jev-lint"
)

// Input は書き出しに必要な原稿ごとの情報。textlint 形式は文字数ベースの位置を
// 計算するため原稿本文 (Src) が要る。
type Input struct {
	Result jjl.Result
	Src    []byte
}

// Formatter は複数原稿の結果をまとめて書き出す。
type Formatter func(w io.Writer, inputs []Input) error

// Names は選べる形式名。
var Names = []string{"text", "rdjsonl", "textlint", "json"}

// Get は名前から Formatter を返す。
func Get(name string) (Formatter, error) {
	switch name {
	case "text", "":
		return Text, nil
	case "rdjsonl":
		return RDJSONL, nil
	case "textlint":
		return Textlint, nil
	case "json":
		return JSON, nil
	}
	return nil, fmt.Errorf("unknown format %q (want one of %v)", name, Names)
}

// Text は path:line:col: message [rule] 形式。
func Text(w io.Writer, inputs []Input) error {
	for _, in := range inputs {
		for _, d := range in.Result.Diagnostics() {
			path := d.Path
			if path == "" {
				path = "<stdin>"
			}
			if _, err := fmt.Fprintf(w, "%s:%d:%d: %s [%s]\n", path, d.Start.Line, d.Start.Column, d.Message, d.Rule); err != nil {
				return err
			}
		}
	}
	return nil
}

// RDJSONL は reviewdog の rdjsonl 形式 (1 行 1 Diagnostic)。
// https://github.com/reviewdog/reviewdog/tree/master/proto/rdf
func RDJSONL(w io.Writer, inputs []Input) error {
	enc := json.NewEncoder(w)
	for _, in := range inputs {
		for _, d := range in.Result.Diagnostics() {
			pos := func(p jjl.Position) map[string]int { return map[string]int{"line": p.Line, "column": p.Column} }
			rec := map[string]any{
				"message": d.Message,
				"location": map[string]any{
					"path":  d.Path,
					"range": map[string]any{"start": pos(d.Start), "end": pos(d.End)},
				},
				"severity":        "WARNING",
				"code":            map[string]any{"value": d.Rule},
				"source":          map[string]any{"name": "jjl", "url": "https://github.com/pankona/japanese-jev-lint"},
				"original_output": fmt.Sprintf("%s:%d:%d: %s [%s]", d.Path, d.Start.Line, d.Start.Column, d.Message, d.Rule),
			}
			if err := enc.Encode(rec); err != nil {
				return err
			}
		}
	}
	return nil
}

// textlintMessage は textlint の TextlintMessage に合わせた形。
// line は 1 始まり、column は 1 始まりの文字数、index / range は 0 始まりの
// 文字 (UTF-16 コード単位) オフセット。
type textlintMessage struct {
	Type     string  `json:"type"`
	RuleID   string  `json:"ruleId"`
	Message  string  `json:"message"`
	Index    int     `json:"index"`
	Line     int     `json:"line"`
	Column   int     `json:"column"`
	Range    [2]int  `json:"range"`
	Loc      tlLoc   `json:"loc"`
	Severity int     `json:"severity"`
	Score    float64 `json:"score,omitempty"`
}

type tlPos struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

type tlLoc struct {
	Start tlPos `json:"start"`
	End   tlPos `json:"end"`
}

type textlintFile struct {
	FilePath string            `json:"filePath"`
	Messages []textlintMessage `json:"messages"`
}

// Textlint は textlint --format json 互換の JSON。
func Textlint(w io.Writer, inputs []Input) error {
	var out []textlintFile
	for _, in := range inputs {
		f := textlintFile{FilePath: in.Result.Path, Messages: []textlintMessage{}}
		for _, d := range in.Result.Diagnostics() {
			start := jjl.UTF16Offset(in.Src, d.Start.Offset)
			end := jjl.UTF16Offset(in.Src, d.End.Offset)
			f.Messages = append(f.Messages, textlintMessage{
				Type:     "lint",
				RuleID:   "jjl/" + d.Rule,
				Message:  d.Message,
				Index:    start,
				Line:     d.Start.Line,
				Column:   jjl.RuneColumn(in.Src, d.Start),
				Range:    [2]int{start, end},
				Loc:      tlLoc{tlPos{d.Start.Line, jjl.RuneColumn(in.Src, d.Start)}, tlPos{d.End.Line, jjl.RuneColumn(in.Src, d.End)}},
				Severity: 1, // warning
				Score:    d.Score,
			})
		}
		out = append(out, f)
	}
	if out == nil {
		out = []textlintFile{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// JSON は jjl 独自の形。診断のほかにトークン消費などの統計を含む。
func JSON(w io.Writer, inputs []Input) error {
	type file struct {
		Path        string           `json:"path"`
		JevUsed     bool             `json:"jev_used"`
		Model       string           `json:"model,omitempty"`
		Tokens      int              `json:"tokens"`
		Sentences   int              `json:"sentences"`
		Cached      int              `json:"cached"`
		Errors      int              `json:"errors"`
		Diagnostics []jjl.Diagnostic `json:"diagnostics"`
	}
	files := []file{}
	for _, in := range inputs {
		r := in.Result
		ds := r.Diagnostics()
		if ds == nil {
			ds = []jjl.Diagnostic{}
		}
		sort.SliceStable(ds, func(i, j int) bool { return ds[i].Start.Offset < ds[j].Start.Offset })
		files = append(files, file{r.Path, r.JevUsed, r.Model, r.Tokens, len(r.Items), r.Cached, r.Errors, ds})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(files)
}
