# japanese-jev-lint (jjl)

[jev](https://typesafe.ai) (TypeSafe の System One モデル) で日本語の文章を lint するツール。
文単位で「誤字?」「主述のねじれ?」「一文が長い」「同じ語句の繰り返し」の確率を jev に聞き、
閾値を超えた文を警告として出す。ですます調の混入など規則で決まるものは正規表現で拾う。

jev は文章を生成せず確率だけを返すので、jjl も指摘文や修正案は作らない。
「引っかかりそうな文」に印を付ける前捌きとして、人間や別のレビュー工程に渡す用途を想定している。

**原稿本文が api.typesafe.ai に送られる。** 送ってよい文章にだけ使うこと。
`-dry-run` で送られる文を先に確認できる。

## インストール

```console
go install github.com/pankona/japanese-jev-lint/cmd/jjl@latest
export TYPESAFE_API_KEY=...
```

## 使い方

```console
jjl content/posts/*/index.md
# content/posts/foo/index.md:39:1: 同じ語句の繰り返し (0.62) [repeat]
# content/posts/foo/index.md:61:3: 主述のねじれ? (0.56) [twisted]
# jjl: 2 件の指摘, jev 入力トークン 56263
```

終了コードは 指摘なし=0、指摘あり=1 (`-fail-on none` で 0)、エラー=2。

### 出力形式 (`-format`)

| 形式 | 用途 |
|---|---|
| `text` (既定) | `path:line:col: message [rule]`。vim quickfix、VS Code の problem matcher、`reviewdog -efm` |
| `rdjsonl` | [reviewdog](https://github.com/reviewdog/reviewdog) に流し込む。`jjl -format rdjsonl *.md \| reviewdog -f rdjsonl -reporter github-pr-review` |
| `textlint` | `textlint --format json` と同じ形。textlint の結果を読む既存のパイプラインに混ぜられる |
| `json` | jjl 独自。診断に加えてトークン消費・キャッシュ数などの統計を含む |

`text` と `rdjsonl` の列は byte 数、`textlint` の列・index は文字数 (UTF-16 コード単位) で、
それぞれの世界の慣習に合わせている。

### 主なオプション

```
-syntax markdown|text    入力の書式 (既定 markdown: front matter・コードブロック・見出し等を飛ばす)
-only typo,twisted       この判定だけ
-disable desumasu        この判定を外す
-threshold typo=0.6      閾値の上書き
-dry-run                 送る予定の文を表示するだけ (API に送らない)
-no-jev                  正規表現判定だけ (API キー不要)
-no-cache / -cache-dir   ディスクキャッシュ (既定 $XDG_CACHE_HOME/jjl)。同じ文は二度聞かない
-list-checks             判定の一覧と閾値
```

### 設定ファイル `.jjl.yml`

カレントディレクトリの `.jjl.yml` (または `-config`) で既定の判定を上書き・追加できる。
`key` が既定にあるものは書いた項目だけ上書き、無いものは追加。

```yaml
model: jev-latest
checks:
  - key: typo
    threshold: 0.6
  - key: keigo
    label: 敬語の誤り?
    threshold: 0.6
    question:
      instructions: "`sentence` に敬語の誤用 (二重敬語、尊敬語と謙譲語の混同) があるか?"
      true: 敬語の使い方が誤っている
      false: 敬語は正しい、または敬語を使っていない
regex_checks:
  - key: zenkaku_space
    label: 全角スペース
    pattern: "　"
disable: [double_ga]
```

## ライブラリとして使う

```go
import jjl "github.com/pankona/japanese-jev-lint"

l := jjl.New() // 既定の判定、TYPESAFE_API_KEY、プロセス内キャッシュ
res := l.Lint(ctx, "post.md", src)
for _, d := range res.Diagnostics() {
    fmt.Println(d.Start.Line, d.Rule, d.Message, d.Text)
}
// エディタ (CodeMirror 等) の位置に合わせるなら
from := jjl.UTF16Offset(src, d.Start.Offset)
```

`Result.Items` には診断に至らなかった文も全判定のスコア付きで入っているので、
閾値を自前で決めたり、スコアをそのまま表示したりもできる。

## 判定について

既定の判定と閾値は、2026 年 9 月に日本語ブログ記事で試して実際に拾えたものだけを入れている。
jev の値は文によってぶれるので閾値は絶対ではなく、使いながら `-threshold` や `.jjl.yml` で調整する前提。
ですます調は jev がである文にも高い値を返すので正規表現にしている。
「係り受けの曖昧さ」「指示語の指し先」は試したが信号が弱く、入れていない。

## ライセンス

MIT
