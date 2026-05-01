package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// NewClaude returns a Client backed by the real Anthropic API.
// Requires ANTHROPIC_API_KEY in the environment.
func NewClaude() (*Client, error) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY not set")
	}
	httpClient := &http.Client{Timeout: 60 * time.Second}

	return &Client{
		Name: "claude",
		Complete: func(ctx context.Context, req Request) (Response, error) {
			return claudeComplete(ctx, httpClient, key, req)
		},
	}, nil
}

func tierToModel(t Tier) string {
	switch t {
	case TierFast:
		return "claude-haiku-4-5-20251001"
	case TierBalance:
		return "claude-sonnet-4-6"
	case TierStrong:
		return "claude-opus-4-7"
	default:
		return "claude-sonnet-4-6"
	}
}

type claudeReq struct {
	Model       string         `json:"model"`
	System      string         `json:"system,omitempty"`
	Messages    []claudeMsg    `json:"messages"`
	MaxTokens   int            `json:"max_tokens"`
	Temperature float64        `json:"temperature,omitempty"`
}

type claudeMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type claudeResp struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func claudeComplete(ctx context.Context, hc *http.Client, key string, req Request) (Response, error) {
	maxTok := req.MaxTokens
	if maxTok == 0 {
		maxTok = 1024
	}
	body := claudeReq{
		Model:       tierToModel(req.Tier),
		System:      req.System,
		MaxTokens:   maxTok,
		Temperature: req.Temperature,
		Messages:    []claudeMsg{{Role: "user", Content: req.User}},
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return Response{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		"https://api.anthropic.com/v1/messages", bytes.NewReader(buf))
	if err != nil {
		return Response{}, err
	}
	httpReq.Header.Set("x-api-key", key)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	httpReq.Header.Set("content-type", "application/json")

	resp, err := hc.Do(httpReq)
	if err != nil {
		return Response{}, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return Response{}, err
	}
	if resp.StatusCode != 200 {
		return Response{}, fmt.Errorf("claude api %d: %s", resp.StatusCode, string(respBody))
	}

	var parsed claudeResp
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return Response{}, err
	}
	var text string
	for _, c := range parsed.Content {
		if c.Type == "text" {
			text += c.Text
		}
	}
	return Response{
		Text:       text,
		StopReason: parsed.StopReason,
		TokensIn:   parsed.Usage.InputTokens,
		TokensOut:  parsed.Usage.OutputTokens,
	}, nil
}
