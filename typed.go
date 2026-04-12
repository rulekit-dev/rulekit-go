package rulekit

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// defaultClient is the package-level client used by NewRule.
var (
	defaultClient   *Client
	defaultClientMu sync.RWMutex
)

// Init initializes the default client by finding the nearest .rulekit/
// directory walking up from the current working directory.
func Init(opts ...Option) error {
	c, err := New(opts...)
	if err != nil {
		return err
	}
	defaultClientMu.Lock()
	defaultClient = c
	defaultClientMu.Unlock()
	return nil
}

// InitAt initializes the default client rooted at a specific directory.
func InitAt(dir string, opts ...Option) error {
	c, err := NewAt(dir, opts...)
	if err != nil {
		return err
	}
	defaultClientMu.Lock()
	defaultClient = c
	defaultClientMu.Unlock()
	return nil
}

// TypedRule evaluates a named ruleset with typed input and output structs.
type TypedRule[I, O any] struct {
	key    string
	client *Client
}

// NewRule creates a TypedRule bound to the named ruleset using the default client.
// Call Init or InitAt before using NewRule.
func NewRule[I, O any](key string) *TypedRule[I, O] {
	defaultClientMu.RLock()
	c := defaultClient
	defaultClientMu.RUnlock()
	return &TypedRule[I, O]{key: key, client: c}
}

// NewRuleWithClient creates a TypedRule bound to the named ruleset using an explicit client.
func NewRuleWithClient[I, O any](c *Client, key string) *TypedRule[I, O] {
	return &TypedRule[I, O]{key: key, client: c}
}

// Eval evaluates the ruleset against the typed input and returns a typed output.
func (r *TypedRule[I, O]) Eval(ctx context.Context, input I) (O, error) {
	var zero O
	if r.client == nil {
		return zero, fmt.Errorf("rulekit: no client initialized — call rulekit.Init() before NewRule")
	}
	return evalAs[I, O](ctx, r.client, r.key, input)
}

// EvalAs evaluates the named ruleset on an explicit client with typed input/output structs.
func EvalAs[I, O any](ctx context.Context, c *Client, key string, input I) (O, error) {
	return evalAs[I, O](ctx, c, key, input)
}

func evalAs[I, O any](ctx context.Context, c *Client, key string, input I) (O, error) {
	var zero O

	inputJSON, err := json.Marshal(input)
	if err != nil {
		return zero, fmt.Errorf("rulekit: marshal input: %w", err)
	}

	var inputMap map[string]any
	if err := json.Unmarshal(inputJSON, &inputMap); err != nil {
		return zero, fmt.Errorf("rulekit: unmarshal input to map: %w", err)
	}

	resultMap, err := c.Eval(ctx, key, inputMap)
	if err != nil {
		return zero, err
	}

	resultJSON, err := json.Marshal(resultMap)
	if err != nil {
		return zero, fmt.Errorf("rulekit: marshal result: %w", err)
	}

	var output O
	if err := json.Unmarshal(resultJSON, &output); err != nil {
		return zero, fmt.Errorf("rulekit: unmarshal result to output type: %w", err)
	}

	return output, nil
}
