package jjl

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"
)

// Severity は診断の重さ。jev の判定は確率なので Error は出さない。
type Severity string

const (
	Warning Severity = "warning"
	Info    Severity = "info"
)

// Diagnostic は「この文がこの判定に引っかかった」一件。
type Diagnostic struct {
	Path     string   `json:"path,omitempty"`
	Start    Position `json:"start"`
	End      Position `json:"end"`
	Text     string   `json:"text"`
	Rule     string   `json:"rule"`
	Label    string   `json:"label"`
	Message  string   `json:"message"`
	Severity Severity `json:"severity"`
	Score    float64  `json:"score,omitempty"` // 正規表現判定なら 0
}

// Item は文ごとの結果。診断に至らなかった文もスコア付きで含む。
type Item struct {
	Sentence Sentence
	Scores   Scores   // jev の全判定のスコア (jev 無効なら nil)
	Flags    []string // 閾値を超えた判定キー。空なら問題なし
	Err      error    // jev への問い合わせに失敗した場合
}

// Result は一つの原稿の lint 結果。
type Result struct {
	Path    string
	JevUsed bool   // jev に聞いたか (API キーが無ければ false で正規表現判定のみ)
	Model   string // jev が返したモデル名
	Tokens  int    // 今回消費した入力トークン (キャッシュ分は含まない)
	Cached  int    // キャッシュが効いた文の数
	Errors  int    // 問い合わせに失敗した文の数
	Items   []Item
	Config  Config
}

// Diagnostics は Items から診断だけを取り出す。
func (r Result) Diagnostics() []Diagnostic {
	var out []Diagnostic
	for _, it := range r.Items {
		for _, f := range it.Flags {
			d := Diagnostic{
				Path: r.Path, Start: it.Sentence.Start, End: it.Sentence.End,
				Text: it.Sentence.Text, Rule: f, Label: r.Config.Label(f), Severity: Warning,
			}
			if v, ok := it.Scores[f]; ok && isJevCheck(r.Config, f) {
				d.Score = v
				d.Message = fmt.Sprintf("%s (%.2f)", d.Label, v)
			} else {
				d.Message = d.Label
			}
			out = append(out, d)
		}
	}
	return out
}

func isJevCheck(c Config, key string) bool {
	for _, ch := range c.Checks {
		if ch.Key == key {
			return true
		}
	}
	return false
}

// Describe は診断の理由を「誤字? 0.65 / ですます調」の形で返す (プロンプトや表示用)。
func (c Config) Describe(flags []string, scores Scores) string {
	var parts []string
	for _, f := range flags {
		if v, ok := scores[f]; ok && isJevCheck(c, f) {
			parts = append(parts, fmt.Sprintf("%s %.2f", c.Label(f), v))
		} else {
			parts = append(parts, c.Label(f))
		}
	}
	return strings.Join(parts, " / ")
}

// Linter は設定・jev クライアント・キャッシュを束ねる。
type Linter struct {
	Config      Config
	Client      *Client // nil なら jev に聞かず正規表現判定のみ
	Cache       Cache   // nil ならキャッシュしない
	Concurrency int     // jev への同時問い合わせ数。0 なら 16
	Syntax      Syntax  // 空なら Markdown
}

// New は既定の設定と環境変数の API キー、プロセス内キャッシュで Linter を作る。
func New() *Linter {
	return &Linter{Config: DefaultConfig(), Client: NewClientFromEnv(), Cache: NewMemoryCache()}
}

// Lint は原稿を文に割って jev に並列で問い合わせ、文ごとの結果を返す。
// 個々の文の問い合わせ失敗は Result.Errors / Item.Err に入れ、エラーは返さない。
func (l *Linter) Lint(ctx context.Context, path string, src []byte) Result {
	syntax := l.Syntax
	if syntax == "" {
		syntax = Markdown
	}
	res := Result{Path: path, Config: l.Config, JevUsed: l.Client != nil && len(l.Config.Checks) > 0}
	sents := Split(src, syntax)
	res.Items = make([]Item, len(sents))
	for i, s := range sents {
		res.Items[i].Sentence = s
	}
	if res.JevUsed {
		l.askAll(ctx, &res)
	}
	for i := range res.Items {
		it := &res.Items[i]
		if it.Err != nil {
			res.Errors++
			continue
		}
		it.Flags = []string{}
		runes := utf8.RuneCountInString(strings.Join(strings.Fields(it.Sentence.Text), ""))
		for _, c := range l.Config.Checks {
			if v, ok := it.Scores[c.Key]; ok && v >= c.Threshold && runes >= c.MinRunes {
				it.Flags = append(it.Flags, c.Key)
			}
		}
		for i := range l.Config.RegexChecks {
			c := &l.Config.RegexChecks[i]
			re, err := c.Regexp()
			if err != nil {
				continue
			}
			if len(re.FindAllStringIndex(it.Sentence.Text, -1)) >= c.Min {
				it.Flags = append(it.Flags, c.Key)
			}
		}
	}
	return res
}

func (l *Linter) askAll(ctx context.Context, res *Result) {
	model := l.Config.Model
	if model == "" {
		model = DefaultModel
	}
	questions := l.Config.questions()
	conc := l.Concurrency
	if conc <= 0 {
		conc = 16
	}
	var (
		wg  sync.WaitGroup
		mu  sync.Mutex
		sem = make(chan struct{}, conc)
	)
	for i := range res.Items {
		it := &res.Items[i]
		key := cacheKey(model, questions, it.Sentence)
		if l.Cache != nil {
			if sc, ok := l.Cache.Get(key); ok {
				it.Scores = sc
				res.Cached++
				continue
			}
		}
		wg.Add(1)
		go func(it *Item, key string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			a, err := l.Client.Ask(ctx, model, map[string]string{
				"prev": it.Sentence.Prev, "sentence": it.Sentence.Text, "next": it.Sentence.Next,
			}, questions)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				it.Err = err
				return
			}
			it.Scores = a.Scores
			res.Model, res.Tokens = a.Model, res.Tokens+a.InputTokens
			if l.Cache != nil {
				l.Cache.Set(key, a.Scores)
			}
		}(it, key)
	}
	wg.Wait()
}
