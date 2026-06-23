package rulekit

import (
	"context"
	"encoding/json"
	"fmt"
)

// FieldType represents the data type of a schema field.
type FieldType string

const (
	FieldTypeNumber  FieldType = "number"
	FieldTypeString  FieldType = "string"
	FieldTypeBoolean FieldType = "boolean"
	FieldTypeEnum    FieldType = "enum"
)

// Direction indicates whether a field is an input or output field.
type Direction string

const (
	DirectionInput  Direction = "input"
	DirectionOutput Direction = "output"
)

// Strategy controls how rules within a node are evaluated.
type Strategy string

const (
	StrategyFirstMatch Strategy = "first_match"
	StrategyAllMatches Strategy = "all_matches"
)

// FieldDef defines the schema for a single field.
type FieldDef struct {
	Type      FieldType `json:"type"`
	Direction Direction `json:"direction"`
	// Options is required for enum fields.
	Options []string `json:"options,omitempty"`
}

// Condition is a single predicate within a rule's When clause.
type Condition struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value any    `json:"value"`
}

// Rule is a single if-then rule within a node.
type Rule struct {
	ID   string         `json:"id"`
	Name string         `json:"name"`
	When []Condition    `json:"when"`
	Then map[string]any `json:"then"`
}

// RuleNode is a decision table node within the graph.
type RuleNode struct {
	ID       string         `json:"id"`
	Name     string         `json:"name,omitempty"`
	Strategy Strategy       `json:"strategy"`
	Rules    []Rule         `json:"rules"`
	Default  map[string]any `json:"default,omitempty"`
}

// Edge connects two nodes in the evaluation graph.
// Map renames output fields from the source node into input field names for the
// destination node. When Map is empty, all output fields are passed as-is.
type Edge struct {
	From string            `json:"from"`
	To   string            `json:"to"`
	Map  map[string]string `json:"map,omitempty"`
}

// DSL is the top-level structure of a rulekit DSL file.
type DSL struct {
	DSLVersion string              `json:"dsl_version"`
	Schema     map[string]FieldDef `json:"schema"`
	Entry      string              `json:"entry"`
	Nodes      []RuleNode          `json:"nodes"`
	Edges      []Edge              `json:"edges,omitempty"`
}

// Ruleset is a parsed and validated DSL, ready for evaluation.
type Ruleset struct {
	dsl      DSL
	nodeByID map[string]*RuleNode
	// outgoing edges indexed by source node ID
	edgesFrom map[string][]Edge
}

// Load parses and validates DSL from JSON bytes.
// Returns a Ruleset ready for evaluation, or a descriptive error.
func Load(data []byte) (*Ruleset, error) {
	var dsl DSL
	if err := json.Unmarshal(data, &dsl); err != nil {
		return nil, fmt.Errorf("rulekit: invalid DSL JSON: %w", err)
	}
	if err := validateDSL(&dsl); err != nil {
		return nil, err
	}

	nodeByID := make(map[string]*RuleNode, len(dsl.Nodes))
	for i := range dsl.Nodes {
		nodeByID[dsl.Nodes[i].ID] = &dsl.Nodes[i]
	}

	edgesFrom := make(map[string][]Edge)
	for _, e := range dsl.Edges {
		edgesFrom[e.From] = append(edgesFrom[e.From], e)
	}

	return &Ruleset{dsl: dsl, nodeByID: nodeByID, edgesFrom: edgesFrom}, nil
}

// TraceEntry records the outcome of a single rule evaluation.
type TraceEntry struct {
	RuleID   string `json:"rule_id"`
	RuleName string `json:"rule_name"`
	NodeID   string `json:"node_id"`
	Matched  bool   `json:"matched"`
}

// EvalResult holds the output and per-rule trace from an evaluation.
type EvalResult struct {
	Output map[string]any
	Trace  []TraceEntry
}

// EvalWithTrace runs the ruleset and returns both output fields and a per-rule trace.
func (r *Ruleset) EvalWithTrace(ctx context.Context, input map[string]any) (*EvalResult, error) {
	facts := make(map[string]any, len(input))
	for k, v := range input {
		facts[k] = v
	}
	output := make(map[string]any)
	var trace []TraceEntry

	nodeID := r.dsl.Entry
	for {
		node, ok := r.nodeByID[nodeID]
		if !ok {
			return nil, fmt.Errorf("rulekit: node %q not found during eval", nodeID)
		}

		nodeOutput, nodeTrace, err := evalNodeWithTrace(node, &r.dsl, facts)
		if err != nil {
			return nil, err
		}
		trace = append(trace, nodeTrace...)
		for k, v := range nodeOutput {
			output[k] = v
			facts[k] = v
		}

		outEdges := r.edgesFrom[nodeID]
		if len(outEdges) == 0 {
			break
		}
		if len(outEdges) == 1 {
			edge := outEdges[0]
			remapped := remapEdge(edge, nodeOutput)
			for k, v := range remapped {
				facts[k] = v
			}
			nodeID = edge.To
		} else {
			for _, edge := range outEdges {
				subFacts := copyMap(facts)
				remapped := remapEdge(edge, nodeOutput)
				for k, v := range remapped {
					subFacts[k] = v
				}
				subResult, err := r.evalFromWithTrace(ctx, edge.To, subFacts)
				if err != nil {
					return nil, err
				}
				trace = append(trace, subResult.Trace...)
				for k, v := range subResult.Output {
					output[k] = v
				}
			}
			break
		}
	}
	return &EvalResult{Output: output, Trace: trace}, nil
}

// evalFromWithTrace evaluates the sub-graph starting at nodeID, collecting trace.
func (r *Ruleset) evalFromWithTrace(ctx context.Context, nodeID string, facts map[string]any) (*EvalResult, error) {
	output := make(map[string]any)
	var trace []TraceEntry
	for {
		node, ok := r.nodeByID[nodeID]
		if !ok {
			return nil, fmt.Errorf("rulekit: node %q not found during eval", nodeID)
		}
		nodeOutput, nodeTrace, err := evalNodeWithTrace(node, &r.dsl, facts)
		if err != nil {
			return nil, err
		}
		trace = append(trace, nodeTrace...)
		for k, v := range nodeOutput {
			output[k] = v
			facts[k] = v
		}
		outEdges := r.edgesFrom[nodeID]
		if len(outEdges) == 0 {
			break
		}
		if len(outEdges) == 1 {
			edge := outEdges[0]
			remapped := remapEdge(edge, nodeOutput)
			for k, v := range remapped {
				facts[k] = v
			}
			nodeID = edge.To
		} else {
			for _, edge := range outEdges {
				subFacts := copyMap(facts)
				remapped := remapEdge(edge, nodeOutput)
				for k, v := range remapped {
					subFacts[k] = v
				}
				subResult, err := r.evalFromWithTrace(ctx, edge.To, subFacts)
				if err != nil {
					return nil, err
				}
				trace = append(trace, subResult.Trace...)
				for k, v := range subResult.Output {
					output[k] = v
				}
			}
			break
		}
	}
	return &EvalResult{Output: output, Trace: trace}, nil
}

// Eval runs the ruleset against the provided input facts and returns output fields.
//
// Missing input fields are treated as the zero value for their type:
// number → 0, string → "", boolean → false, enum → "".
// Unknown input fields are silently ignored.
func (r *Ruleset) Eval(ctx context.Context, input map[string]any) (map[string]any, error) {
	// Working fact map: starts as a copy of input, accumulates outputs.
	facts := make(map[string]any, len(input))
	for k, v := range input {
		facts[k] = v
	}

	output := make(map[string]any)

	nodeID := r.dsl.Entry
	for {
		node, ok := r.nodeByID[nodeID]
		if !ok {
			return nil, fmt.Errorf("rulekit: node %q not found during eval", nodeID)
		}

		nodeOutput, err := evalNode(node, &r.dsl, facts)
		if err != nil {
			return nil, err
		}

		// Accumulate outputs.
		for k, v := range nodeOutput {
			output[k] = v
			facts[k] = v
		}

		outEdges := r.edgesFrom[nodeID]
		if len(outEdges) == 0 {
			break
		}

		for _, edge := range outEdges {
			remapped := remapEdge(edge, nodeOutput)
			for k, v := range remapped {
				facts[k] = v
			}
			// Evaluate each successor; for multi-edge DAGs each branch is
			// independent and we continue traversal from their successors.
			// We recurse inline by queueing; for simplicity we evaluate
			// sequentially via the outer loop for linear chains.
		}

		// Follow the first edge for traversal (linear pipeline).
		// For branching DAGs: evaluate all successor sub-graphs.
		if len(outEdges) == 1 {
			edge := outEdges[0]
			remapped := remapEdge(edge, nodeOutput)
			for k, v := range remapped {
				facts[k] = v
			}
			nodeID = edge.To
		} else {
			// Multiple outgoing edges: evaluate each sub-graph recursively.
			for _, edge := range outEdges {
				subFacts := copyMap(facts)
				remapped := remapEdge(edge, nodeOutput)
				for k, v := range remapped {
					subFacts[k] = v
				}
				subOut, err := r.evalFrom(ctx, edge.To, subFacts)
				if err != nil {
					return nil, err
				}
				for k, v := range subOut {
					output[k] = v
				}
			}
			break
		}
	}

	return output, nil
}

// evalFrom evaluates the sub-graph starting at nodeID with the given facts.
func (r *Ruleset) evalFrom(ctx context.Context, nodeID string, facts map[string]any) (map[string]any, error) {
	output := make(map[string]any)
	for {
		node, ok := r.nodeByID[nodeID]
		if !ok {
			return nil, fmt.Errorf("rulekit: node %q not found during eval", nodeID)
		}

		nodeOutput, err := evalNode(node, &r.dsl, facts)
		if err != nil {
			return nil, err
		}
		for k, v := range nodeOutput {
			output[k] = v
			facts[k] = v
		}

		outEdges := r.edgesFrom[nodeID]
		if len(outEdges) == 0 {
			break
		}
		if len(outEdges) == 1 {
			edge := outEdges[0]
			remapped := remapEdge(edge, nodeOutput)
			for k, v := range remapped {
				facts[k] = v
			}
			nodeID = edge.To
		} else {
			for _, edge := range outEdges {
				subFacts := copyMap(facts)
				remapped := remapEdge(edge, nodeOutput)
				for k, v := range remapped {
					subFacts[k] = v
				}
				subOut, err := r.evalFrom(ctx, edge.To, subFacts)
				if err != nil {
					return nil, err
				}
				for k, v := range subOut {
					output[k] = v
				}
			}
			break
		}
	}
	return output, nil
}

func remapEdge(edge Edge, nodeOutput map[string]any) map[string]any {
	result := make(map[string]any)
	if len(edge.Map) == 0 {
		for k, v := range nodeOutput {
			result[k] = v
		}
		return result
	}
	for outField, inField := range edge.Map {
		if v, ok := nodeOutput[outField]; ok {
			result[inField] = v
		}
	}
	return result
}

func copyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
