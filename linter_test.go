package jjl

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// fakeJev は sentence に「誤」を含む文だけ typo=0.9 を返す
func fakeJev(t *testing.T, calls *int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(calls, 1)
		if r.Header.Get("Authorization") != "Bearer test-key" {
			http.Error(w, "unauthorized", 401)
			return
		}
		var req struct {
			Model     string            `json:"model"`
			State     map[string]string `json:"state"`
			Questions map[string]any    `json:"questions"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		typo := 0.1
		for _, r := range req.State["sentence"] {
			if r == '誤' {
				typo = 0.9
			}
		}
		ans := map[string]any{}
		for k := range req.Questions {
			ans[k] = map[string]float64{"noul": 0.1}
		}
		ans["typo"] = map[string]float64{"noul": typo}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": req.Model, "answers": ans, "usage": map[string]int{"input_tokens": 100},
		})
	}))
}

func TestLint(t *testing.T) {
	var calls int32
	srv := fakeJev(t, &calls)
	defer srv.Close()
	l := &Linter{
		Config: DefaultConfig(),
		Client: &Client{APIKey: "test-key", Endpoint: srv.URL},
		Cache:  NewMemoryCache(),
	}
	src := []byte("正しい文である。誤字のある文である。ですます調の文です。")
	res := l.Lint(context.Background(), "a.md", src)
	if !res.JevUsed || res.Errors != 0 || res.Tokens != 300 || res.Model != "jev-latest" {
		t.Fatalf("res = %+v", res)
	}
	ds := res.Diagnostics()
	if len(ds) != 2 {
		t.Fatalf("diagnostics = %+v", ds)
	}
	if ds[0].Rule != "typo" || ds[0].Text != "誤字のある文である。" || ds[0].Score != 0.9 || ds[0].Message != "誤字? (0.90)" {
		t.Errorf("ds[0] = %+v", ds[0])
	}
	if ds[1].Rule != "desumasu" || ds[1].Score != 0 || ds[1].Message != "ですます調" {
		t.Errorf("ds[1] = %+v", ds[1])
	}
	if ds[1].Start.Line != 1 || ds[1].Start.Column != len("正しい文である。誤字のある文である。")+1 {
		t.Errorf("ds[1] pos = %+v", ds[1].Start)
	}
	// 二回目はキャッシュで jev に聞かない
	res = l.Lint(context.Background(), "a.md", src)
	if res.Cached != 3 || atomic.LoadInt32(&calls) != 3 {
		t.Errorf("cached = %d, calls = %d", res.Cached, calls)
	}
	if got := res.Config.Describe([]string{"typo", "desumasu"}, Scores{"typo": 0.9}); got != "誤字? 0.90 / ですます調" {
		t.Errorf("Describe = %q", got)
	}
}

func TestLintWithoutClient(t *testing.T) {
	l := &Linter{Config: DefaultConfig()}
	res := l.Lint(context.Background(), "", []byte("ですます調の文です。"))
	if res.JevUsed || len(res.Diagnostics()) != 1 || res.Diagnostics()[0].Rule != "desumasu" {
		t.Errorf("res = %+v", res.Diagnostics())
	}
}

func TestFileCache(t *testing.T) {
	c := FileCache{Dir: t.TempDir()}
	if _, ok := c.Get("k"); ok {
		t.Fatal("unexpected hit")
	}
	c.Set("k", Scores{"typo": 0.5})
	s, ok := c.Get("k")
	if !ok || s["typo"] != 0.5 {
		t.Errorf("got %v %v", s, ok)
	}
}
