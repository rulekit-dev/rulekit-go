package rulekit

import (
	"fmt"
	"strings"
)

// evalNode evaluates a single RuleNode against the current fact map.
// Returns a map of output field assignments.
func evalNode(node *RuleNode, dsl *DSL, facts map[string]any) (map[string]any, error) {
	output := make(map[string]any)

	switch node.Strategy {
	case StrategyFirstMatch:
		for _, rule := range node.Rules {
			matched, err := evalConditions(rule.When, dsl, facts)
			if err != nil {
				return nil, fmt.Errorf("node %s, rule %s: %w", node.ID, rule.ID, err)
			}
			if matched {
				for k, v := range rule.Then {
					output[k] = v
				}
				return output, nil
			}
		}

	case StrategyAllMatches:
		anyMatched := false
		for _, rule := range node.Rules {
			matched, err := evalConditions(rule.When, dsl, facts)
			if err != nil {
				return nil, fmt.Errorf("node %s, rule %s: %w", node.ID, rule.ID, err)
			}
			if matched {
				anyMatched = true
				for k, v := range rule.Then {
					output[k] = v
				}
			}
		}
		if anyMatched {
			return output, nil
		}

	default:
		return nil, fmt.Errorf("rulekit: node %s: unknown strategy %q", node.ID, node.Strategy)
	}

	// No rule matched — apply default if present.
	if node.Default != nil {
		for k, v := range node.Default {
			output[k] = v
		}
	}
	return output, nil
}

// evalConditions returns true if all conditions pass.
func evalConditions(conditions []Condition, dsl *DSL, facts map[string]any) (bool, error) {
	for i, cond := range conditions {
		ok, err := evalCondition(cond, i, dsl, facts)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

func evalCondition(cond Condition, idx int, dsl *DSL, facts map[string]any) (bool, error) {
	fieldDef, exists := dsl.Schema[cond.Field]
	if !exists {
		return false, fmt.Errorf("condition %d: unknown field %q", idx, cond.Field)
	}

	raw := facts[cond.Field] // nil if missing — treated as zero value

	switch fieldDef.Type {
	case FieldTypeNumber:
		return evalNumberCondition(cond, idx, raw)
	case FieldTypeString:
		return evalStringCondition(cond, idx, raw)
	case FieldTypeBoolean:
		return evalBoolCondition(cond, idx, raw)
	case FieldTypeEnum:
		return evalEnumCondition(cond, idx, raw)
	default:
		return false, fmt.Errorf("condition %d: unknown field type %q for field %q", idx, fieldDef.Type, cond.Field)
	}
}

func toFloat64(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case int32:
		return float64(x), true
	case nil:
		return 0, true // zero value
	}
	return 0, false
}

func evalNumberCondition(cond Condition, idx int, raw any) (bool, error) {
	factVal, ok := toFloat64(raw)
	if !ok {
		return false, fmt.Errorf("condition %d: field %q: expected number, got %T", idx, cond.Field, raw)
	}
	condVal, ok := toFloat64(cond.Value)
	if !ok {
		return false, fmt.Errorf("condition %d: field %q: condition value is not a number", idx, cond.Field)
	}

	switch cond.Op {
	case "eq":
		return factVal == condVal, nil
	case "ne":
		return factVal != condVal, nil
	case "gt":
		return factVal > condVal, nil
	case "gte":
		return factVal >= condVal, nil
	case "lt":
		return factVal < condVal, nil
	case "lte":
		return factVal <= condVal, nil
	default:
		return false, fmt.Errorf("condition %d: field %q: unsupported operator %q for number", idx, cond.Field, cond.Op)
	}
}

func evalStringCondition(cond Condition, idx int, raw any) (bool, error) {
	var factVal string
	switch x := raw.(type) {
	case string:
		factVal = x
	case nil:
		factVal = "" // zero value
	default:
		return false, fmt.Errorf("condition %d: field %q: expected string, got %T", idx, cond.Field, raw)
	}

	condVal, ok := cond.Value.(string)
	if !ok {
		return false, fmt.Errorf("condition %d: field %q: condition value is not a string", idx, cond.Field)
	}

	switch cond.Op {
	case "eq":
		return factVal == condVal, nil
	case "ne":
		return factVal != condVal, nil
	case "contains":
		return strings.Contains(factVal, condVal), nil
	case "starts_with":
		return strings.HasPrefix(factVal, condVal), nil
	case "ends_with":
		return strings.HasSuffix(factVal, condVal), nil
	default:
		return false, fmt.Errorf("condition %d: field %q: unsupported operator %q for string", idx, cond.Field, cond.Op)
	}
}

func evalBoolCondition(cond Condition, idx int, raw any) (bool, error) {
	var factVal bool
	switch x := raw.(type) {
	case bool:
		factVal = x
	case nil:
		factVal = false // zero value
	default:
		return false, fmt.Errorf("condition %d: field %q: expected boolean, got %T", idx, cond.Field, raw)
	}

	condVal, ok := cond.Value.(bool)
	if !ok {
		return false, fmt.Errorf("condition %d: field %q: condition value is not a boolean", idx, cond.Field)
	}

	switch cond.Op {
	case "eq":
		return factVal == condVal, nil
	case "ne":
		return factVal != condVal, nil
	default:
		return false, fmt.Errorf("condition %d: field %q: unsupported operator %q for boolean", idx, cond.Field, cond.Op)
	}
}

func evalEnumCondition(cond Condition, idx int, raw any) (bool, error) {
	var factVal string
	switch x := raw.(type) {
	case string:
		factVal = x
	case nil:
		factVal = "" // zero value
	default:
		return false, fmt.Errorf("condition %d: field %q: expected string (enum), got %T", idx, cond.Field, raw)
	}

	switch cond.Op {
	case "eq":
		condVal, ok := cond.Value.(string)
		if !ok {
			return false, fmt.Errorf("condition %d: field %q: eq value must be a string", idx, cond.Field)
		}
		return factVal == condVal, nil

	case "ne":
		condVal, ok := cond.Value.(string)
		if !ok {
			return false, fmt.Errorf("condition %d: field %q: ne value must be a string", idx, cond.Field)
		}
		return factVal != condVal, nil

	case "in":
		strs, err := toStringSlice(cond.Value)
		if err != nil {
			return false, fmt.Errorf("condition %d: field %q: in value: %w", idx, cond.Field, err)
		}
		for _, s := range strs {
			if factVal == s {
				return true, nil
			}
		}
		return false, nil

	default:
		return false, fmt.Errorf("condition %d: field %q: unsupported operator %q for enum", idx, cond.Field, cond.Op)
	}
}

func toStringSlice(v any) ([]string, error) {
	switch x := v.(type) {
	case []string:
		return x, nil
	case []any:
		out := make([]string, len(x))
		for i, item := range x {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("element %d is not a string", i)
			}
			out[i] = s
		}
		return out, nil
	default:
		return nil, fmt.Errorf("expected []string, got %T", v)
	}
}
