// jjl (japanese-jev-lint) は jev で日本語の文章を lint する CLI。
//
//	jjl [flags] file.md ...   (file に - を渡すと標準入力)
//
// 終了コード: 0 = 指摘なし、1 = 指摘あり (-fail-on none で 0)、2 = エラー。
// 原稿本文が api.typesafe.ai へ送られる。-dry-run で送る前に確認できる。
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"

	jjl "github.com/pankona/japanese-jev-lint"
	"github.com/pankona/japanese-jev-lint/format"
)

type listFlag []string

func (l *listFlag) String() string     { return strings.Join(*l, ",") }
func (l *listFlag) Set(v string) error { *l = append(*l, strings.Split(v, ",")...); return nil }

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("jjl", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		formatName                = fs.String("format", "text", "出力形式: "+strings.Join(format.Names, ", "))
		configPath                = fs.String("config", "", "設定ファイル (.jjl.yml)。省略時はカレントの .jjl.yml を使う")
		syntax                    = fs.String("syntax", "markdown", "入力の書式: markdown, text")
		failOn                    = fs.String("fail-on", "warning", "終了コード 1 にする条件: warning (指摘があれば), none")
		dryRun                    = fs.Bool("dry-run", false, "jev に送らず、送る予定の文と数を表示する")
		noJev                     = fs.Bool("no-jev", false, "jev に聞かず正規表現判定だけ行う")
		noCache                   = fs.Bool("no-cache", false, "ディスクキャッシュを使わない")
		cacheDir                  = fs.String("cache-dir", "", "キャッシュディレクトリ (既定: $XDG_CACHE_HOME/jjl)")
		model                     = fs.String("model", "", "jev のモデル名 (既定: "+jjl.DefaultModel+")")
		concurrency               = fs.Int("concurrency", 16, "jev への同時問い合わせ数")
		listChecks                = fs.Bool("list-checks", false, "判定の一覧と閾値を表示して終了")
		quiet                     = fs.Bool("quiet", false, "統計 (トークン数など) を stderr に出さない")
		only, disable, thresholds listFlag
	)
	fs.Var(&only, "only", "この判定だけ行う (カンマ区切り、複数指定可)")
	fs.Var(&disable, "disable", "この判定を行わない (カンマ区切り、複数指定可)")
	fs.Var(&thresholds, "threshold", "閾値の上書き key=value (例: typo=0.6)")
	fs.Usage = func() {
		fmt.Fprintf(stderr, "usage: jjl [flags] file.md ...   (- で標準入力)\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		fmt.Fprintln(stderr, "jjl:", err)
		return 2
	}
	if *model != "" {
		cfg.Model = *model
	}
	for _, t := range thresholds {
		k, v, ok := strings.Cut(t, "=")
		f, perr := strconv.ParseFloat(v, 64)
		if !ok || perr != nil {
			fmt.Fprintf(stderr, "jjl: -threshold は key=value の形 (got %q)\n", t)
			return 2
		}
		found := false
		for i := range cfg.Checks {
			if cfg.Checks[i].Key == k {
				cfg.Checks[i].Threshold = f
				found = true
			}
		}
		if !found {
			fmt.Fprintf(stderr, "jjl: -threshold: unknown check %q\n", k)
			return 2
		}
	}
	cfg = cfg.Filter(only, disable)

	if *listChecks {
		for _, c := range cfg.Checks {
			fmt.Fprintf(stdout, "%-12s %-24s jev  threshold=%.2f", c.Key, c.Label, c.Threshold)
			if c.MinRunes > 0 {
				fmt.Fprintf(stdout, " min_runes=%d", c.MinRunes)
			}
			fmt.Fprintln(stdout)
		}
		for _, c := range cfg.RegexChecks {
			fmt.Fprintf(stdout, "%-12s %-24s regex min=%d /%s/\n", c.Key, c.Label, c.Min, c.Pattern)
		}
		return 0
	}

	fmtr, err := format.Get(*formatName)
	if err != nil {
		fmt.Fprintln(stderr, "jjl:", err)
		return 2
	}
	var syn jjl.Syntax
	switch *syntax {
	case "markdown", "md":
		syn = jjl.Markdown
	case "text", "txt":
		syn = jjl.Text
	default:
		fmt.Fprintf(stderr, "jjl: unknown -syntax %q\n", *syntax)
		return 2
	}
	paths := fs.Args()
	if len(paths) == 0 {
		fs.Usage()
		return 2
	}

	// 原稿を読む
	type doc struct {
		path string
		src  []byte
	}
	var docs []doc
	for _, p := range paths {
		var b []byte
		if p == "-" {
			b, err = io.ReadAll(stdin)
			p = ""
		} else {
			b, err = os.ReadFile(p)
		}
		if err != nil {
			fmt.Fprintln(stderr, "jjl:", err)
			return 2
		}
		docs = append(docs, doc{p, b})
	}

	if *dryRun {
		total := 0
		for _, d := range docs {
			sents := jjl.Split(d.src, syn)
			total += len(sents)
			for _, s := range sents {
				fmt.Fprintf(stdout, "%s:%d:%d: %s\n", d.path, s.Start.Line, s.Start.Column, s.Text)
			}
		}
		fmt.Fprintf(stderr, "jjl: %d 文を jev (%s) に送る予定 (dry-run のため送っていない)\n", total, cfg.Model)
		return 0
	}

	l := &jjl.Linter{Config: cfg, Concurrency: *concurrency, Syntax: syn}
	if !*noJev {
		l.Client = jjl.NewClientFromEnv()
		if l.Client == nil {
			fmt.Fprintln(stderr, "jjl: TYPESAFE_API_KEY が未設定。jev には聞かず正規表現判定だけ行う (-no-jev で明示できる)")
		}
	}
	if !*noCache {
		dir := *cacheDir
		if dir == "" {
			if base, err := os.UserCacheDir(); err == nil {
				dir = filepath.Join(base, "jjl")
			}
		}
		if dir != "" {
			l.Cache = jjl.FileCache{Dir: dir}
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	var inputs []format.Input
	var nDiag, nErr, tokens int
	for _, d := range docs {
		res := l.Lint(ctx, d.path, d.src)
		nDiag += len(res.Diagnostics())
		nErr += res.Errors
		tokens += res.Tokens
		if res.Errors > 0 {
			for _, it := range res.Items {
				if it.Err != nil {
					fmt.Fprintf(stderr, "jjl: %s:%d: %v\n", d.path, it.Sentence.Start.Line, it.Err)
					break // 同種のエラーが並ぶので最初の一件だけ
				}
			}
		}
		inputs = append(inputs, format.Input{Result: res, Src: d.src})
	}
	if err := fmtr(stdout, inputs); err != nil {
		fmt.Fprintln(stderr, "jjl:", err)
		return 2
	}
	if !*quiet && l.Client != nil {
		fmt.Fprintf(stderr, "jjl: %d 件の指摘, jev 入力トークン %d\n", nDiag, tokens)
	}
	if nErr > 0 {
		return 2
	}
	if nDiag > 0 && *failOn != "none" {
		return 1
	}
	return 0
}
