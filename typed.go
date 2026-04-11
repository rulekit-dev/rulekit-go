package rulekit

import (
	"context"
	"encoding/json"
	"fmt"
)

// EvalAs evaluates the named ruleset with a typed input struct and decodes the
// result into a typed output struct. Fields are mapped by their json tags.
//
// Missing input fields are treated as zero values; unknown fields are ignored.
func EvalAs[I, O any](ctx context.Context, c *Client, key string, input I) (O, error) {
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
