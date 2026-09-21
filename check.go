package jjl

import "regexp"

// Question は jev への質問 (System One の noul 判定)。
type Question struct {
	Instructions string `json:"instructions" yaml:"instructions"`
	True         string `json:"true" yaml:"true"`   // yes と判定する基準
	False        string `json:"false" yaml:"false"` // no と判定する基準
}

// Check は jev に聞く判定ひとつ分。Score (yes の確率) が Threshold 以上なら診断にする。
type Check struct {
	Key       string   `json:"key" yaml:"key"`
	Label     string   `json:"label" yaml:"label"`
	Threshold float64  `json:"threshold" yaml:"threshold"`
	MinRunes  int      `json:"min_runes,omitempty" yaml:"min_runes,omitempty"` // 空白を除いた字数がこれ未満の文には出さない
	Question  Question `json:"question" yaml:"question"`
}

// RegexCheck は正規表現で拾う判定 (jev が苦手、または規則で十分なもの)。
type RegexCheck struct {
	Key     string `json:"key" yaml:"key"`
	Label   string `json:"label" yaml:"label"`
	Pattern string `json:"pattern" yaml:"pattern"`
	Min     int    `json:"min" yaml:"min"` // マッチ数がこれ以上で診断
	re      *regexp.Regexp
}

// Regexp はコンパイル済みのパターンを返す。
func (c *RegexCheck) Regexp() (*regexp.Regexp, error) {
	if c.re == nil {
		re, err := regexp.Compile(c.Pattern)
		if err != nil {
			return nil, err
		}
		c.re = re
	}
	return c.re, nil
}

// Config は判定の一覧。ゼロ値は「判定なし」なので、通常は DefaultConfig から始める。
type Config struct {
	Model       string       `json:"model,omitempty" yaml:"model,omitempty"`
	Checks      []Check      `json:"checks" yaml:"checks"`
	RegexChecks []RegexCheck `json:"regex_checks" yaml:"regex_checks"`
}

// DefaultModel は jev のモデル名。
const DefaultModel = "jev-latest"

// DefaultConfig は 2026-09 に日本語ブログ記事で実験して効いた判定だけを含む。
// 閾値は絶対値が文によってぶれるので、使いながら調整する前提。
// 「係り受けの曖昧さ」「指示語の指し先」は信号が弱かったので入れていない。
// ですます調は jev がである文にも高い値を返すので正規表現で拾う。
func DefaultConfig() Config {
	return Config{
		Model: DefaultModel,
		Checks: []Check{
			{
				Key: "typo", Label: "誤字?", Threshold: 0.5,
				Question: Question{
					Instructions: "`sentence` に誤字・脱字・漢字の変換ミス・文字の重複や抜けがあるか? くだけた言い回しや口語 (「やめらんねぇ」等) は誤字ではない。",
					True:         "明らかな誤字・脱字・変換ミスがある",
					False:        "表記は正しい (口語表現・ひらがな表記は誤りとしない)",
				},
			},
			{
				Key: "twisted", Label: "主述のねじれ?", Threshold: 0.5,
				Question: Question{
					Instructions: "`sentence` は日本語の一文である。主語と述語が対応せず、文としてねじれている、または述語が着地していない (「〜というのは、〜がある」のような重なり、文末で主語がすり替わる、譲歩や否定の二重化) か?",
					True:         "主述がねじれている、述語が未完・重複している、否定や譲歩が二重になっている",
					False:        "口語的・くだけた表現でも、主語と述語は素直に対応しており文として成立している",
				},
			},
			{
				// jev の「長い」は字数でなく節の詰め込みを見るので、括弧の挿入や語の反復がある
				// 短い文にも反応する。字数は規則で決められるので下限を置く (本物は 70 字以上だった)
				Key: "long", Label: "一文が長い", Threshold: 0.7, MinRunes: 70,
				Question: Question{
					Instructions: "`sentence` は、読点や「〜し」「〜が」「〜ので」で節をつなぎ続けて一文に話題や動作が三つ以上詰め込まれており、読者が途中で息切れするか? (読者は `prev`・`next` も見ている)",
					True:         "一文に節が多く、区切って二文以上にした方が読みやすい",
					False:        "節の数は適度で、一文のまま読める",
				},
			},
			{
				Key: "repeat", Label: "同じ語句の繰り返し", Threshold: 0.55,
				Question: Question{
					Instructions: "`sentence` の中で、同じ語句 (名詞・動詞・言い回し) が二度以上出てきて冗長に感じるか? 強調のための意図的な繰り返しや口癖は除く。",
					True:         "同じ語句が一文の中で繰り返されていて、片方を削るか言い換えた方がよい",
					False:        "繰り返しはない、または意図的な強調",
				},
			},
		},
		RegexChecks: []RegexCheck{
			{Key: "desumasu", Label: "ですます調", Pattern: `(です|ます|ました|でした|ましょう|ません|ですね|ますね)[。！？!?」)]*\s*$`, Min: 1},
			{Key: "double_ga", Label: "逆接が二重 (〜が、〜が、)", Pattern: `(が|けど|けれど|けれども|ものの)、`, Min: 2},
		},
	}
}

// Label は判定キーの表示名を返す。未知のキーはそのまま返す。
func (c Config) Label(key string) string {
	for _, ch := range c.Checks {
		if ch.Key == key {
			return ch.Label
		}
	}
	for _, rc := range c.RegexChecks {
		if rc.Key == key {
			return rc.Label
		}
	}
	return key
}

// Labels は判定キーと表示名を設定順に返す。
func (c Config) Labels() []Label {
	var out []Label
	for _, ch := range c.Checks {
		out = append(out, Label{ch.Key, ch.Label})
	}
	for _, rc := range c.RegexChecks {
		out = append(out, Label{rc.Key, rc.Label})
	}
	return out
}

// Label は判定キーと表示名の組。
type Label struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// Filter は only / disable で判定を絞った Config を返す。only が空なら全判定が対象。
func (c Config) Filter(only, disable []string) Config {
	keep := func(key string) bool {
		for _, d := range disable {
			if d == key {
				return false
			}
		}
		if len(only) == 0 {
			return true
		}
		for _, o := range only {
			if o == key {
				return true
			}
		}
		return false
	}
	out := Config{Model: c.Model}
	for _, ch := range c.Checks {
		if keep(ch.Key) {
			out.Checks = append(out.Checks, ch)
		}
	}
	for _, rc := range c.RegexChecks {
		if keep(rc.Key) {
			out.RegexChecks = append(out.RegexChecks, rc)
		}
	}
	return out
}

// questions は jev API に渡す questions オブジェクトを組む。
func (c Config) questions() map[string]any {
	q := map[string]any{}
	for _, ch := range c.Checks {
		q[ch.Key] = map[string]any{
			"type":         "noul",
			"instructions": ch.Question.Instructions,
			"criteria":     map[string]any{"true": ch.Question.True, "false": ch.Question.False},
		}
	}
	return q
}
