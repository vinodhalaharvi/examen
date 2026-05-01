// Package llm provides a typed seam to language models without using interfaces.
// The Client is a struct of function fields, so we get pluggable implementations
// (mock, claude, etc.) without runtime type erasure.
package llm

import (
	"context"
)

// Tier represents model quality/cost tradeoff.
type Tier int

const (
	TierFast    Tier = iota // cheap, fast — like Haiku
	TierBalance             // mid-tier — like Sonnet
	TierStrong              // top quality — like Opus
)

// Request is a single LLM call's input.
type Request struct {
	System      string
	User        string
	Tier        Tier
	MaxTokens   int
	Temperature float64
	JSONSchema  string // if non-empty, response must be JSON matching this schema
}

// Response is what an LLM call returns.
type Response struct {
	Text       string
	StopReason string
	TokensIn   int
	TokensOut  int
}

// Client is a function-typed seam — no interface, just a callable struct.
// Different implementations populate Complete with mock or real behavior.
type Client struct {
	Complete func(ctx context.Context, req Request) (Response, error)
	Name     string
}
