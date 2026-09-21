package jjl

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Cache は文ごとの jev スコアのキャッシュ。同じ文は聞き直さない
// (書き換えた文だけ課金・待ち時間が発生する)。
type Cache interface {
	Get(key string) (Scores, bool)
	Set(key string, s Scores)
}

// MemoryCache はプロセス内キャッシュ。
type MemoryCache struct {
	mu sync.Mutex
	m  map[string]Scores
}

func NewMemoryCache() *MemoryCache { return &MemoryCache{m: map[string]Scores{}} }

func (c *MemoryCache) Get(key string) (Scores, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, ok := c.m[key]
	return s, ok
}

func (c *MemoryCache) Set(key string, s Scores) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[key] = s
}

// FileCache はディレクトリに 1 キー 1 ファイルで保存する永続キャッシュ
// (CI の再実行や CLI の再起動で課金しないため)。ファイル名はキーの SHA-256。
type FileCache struct {
	Dir string
}

func (c FileCache) path(key string) string {
	sum := sha256.Sum256([]byte(key))
	h := hex.EncodeToString(sum[:])
	return filepath.Join(c.Dir, h[:2], h[2:]+".json")
}

func (c FileCache) Get(key string) (Scores, bool) {
	b, err := os.ReadFile(c.path(key))
	if err != nil {
		return nil, false
	}
	var s Scores
	if json.Unmarshal(b, &s) != nil {
		return nil, false
	}
	return s, true
}

func (c FileCache) Set(key string, s Scores) {
	p := c.path(key)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return
	}
	b, _ := json.Marshal(s)
	tmp := p + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, p)
	}
}

// cacheKey は文とその前後、モデル名、質問の内容から決まる。
// 質問文や閾値を変えたときに古いスコアが残らないよう、questions も混ぜる。
func cacheKey(model string, questions map[string]any, s Sentence) string {
	q, _ := json.Marshal(questions)
	sum := sha256.Sum256(q)
	return model + "\x00" + hex.EncodeToString(sum[:8]) + "\x00" + s.Prev + "\x00" + s.Text + "\x00" + s.Next
}
