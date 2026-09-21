package jjl

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// DefaultEndpoint は TypeSafe System One API のエンドポイント。
const DefaultEndpoint = "https://api.typesafe.ai/v1/systemone"

// ErrNoAPIKey は API キーが無い (jev に聞けない) ことを表す。
var ErrNoAPIKey = errors.New("jjl: TYPESAFE_API_KEY is not set")

// Client は jev への問い合わせを行う。
type Client struct {
	APIKey     string
	Endpoint   string       // 空なら DefaultEndpoint
	HTTPClient *http.Client // 空なら 30 秒タイムアウトのクライアント
}

// NewClientFromEnv は環境変数 TYPESAFE_API_KEY からクライアントを作る。
// キーが無ければ nil を返す (Linter は nil Client を「正規表現判定のみ」として扱う)。
func NewClientFromEnv() *Client {
	key := os.Getenv("TYPESAFE_API_KEY")
	if key == "" {
		return nil
	}
	return &Client{APIKey: key}
}

// Answer は jev の一回の応答。
type Answer struct {
	Model       string
	Scores      Scores // 質問キー → yes (noul) の確率
	InputTokens int
}

// Scores は判定キー → yes の確率。
type Scores map[string]float64

// Ask は state と questions を jev に投げる。
func (c *Client) Ask(ctx context.Context, model string, state map[string]string, questions map[string]any) (Answer, error) {
	if c == nil || c.APIKey == "" {
		return Answer{}, ErrNoAPIKey
	}
	endpoint := c.Endpoint
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	hc := c.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	if model == "" {
		model = DefaultModel
	}
	body, _ := json.Marshal(map[string]any{
		"model":     model,
		"state":     state,
		"questions": questions,
	})
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return Answer{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	res, err := hc.Do(req)
	if err != nil {
		return Answer{}, err
	}
	defer res.Body.Close()
	rb, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		return Answer{}, fmt.Errorf("jev: %d: %s", res.StatusCode, rb)
	}
	var r struct {
		Model   string `json:"model"`
		Answers map[string]struct {
			Noul float64 `json:"noul"`
		} `json:"answers"`
		Usage struct {
			InputTokens int `json:"input_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(rb, &r); err != nil {
		return Answer{}, err
	}
	a := Answer{Model: r.Model, Scores: Scores{}, InputTokens: r.Usage.InputTokens}
	for k, v := range r.Answers {
		a.Scores[k] = v.Noul
	}
	return a, nil
}
