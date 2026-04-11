package rulekit_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/rulekit/rulekit-go"
)

// ---- helpers ----------------------------------------------------------------

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func marshalDSL(t *testing.T, dsl rulekit.DSL) []byte {
	t.Helper()
	b, err := json.Marshal(dsl)
	if err != nil {
		t.Fatalf("marshal DSL: %v", err)
	}
	return b
}

func ctx() context.Context { return context.Background() }

// ---- DSL fixtures -----------------------------------------------------------

var singleNodeFirstMatch = rulekit.DSL{
	DSLVersion: "v1",
	Schema: map[string]rulekit.FieldDef{
		"amount": {Type: rulekit.FieldTypeNumber, Direction: rulekit.DirectionInput},
		"tier":   {Type: rulekit.FieldTypeString, Direction: rulekit.DirectionOutput},
	},
	Entry: "node-1",
	Nodes: []rulekit.RuleNode{
		{
			ID:       "node-1",
			Strategy: rulekit.StrategyFirstMatch,
			Rules: []rulekit.Rule{
				{
					ID:   "node-1_r0",
					Name: "high",
					When: []rulekit.Condition{{Field: "amount", Op: "gte", Value: 1000.0}},
					Then: map[string]any{"tier": "premium"},
				},
				{
					ID:   "node-1_r1",
					Name: "low",
					When: []rulekit.Condition{{Field: "amount", Op: "lt", Value: 1000.0}},
					Then: map[string]any{"tier": "standard"},
				},
			},
		},
	},
}

var singleNodeAllMatches = rulekit.DSL{
	DSLVersion: "v1",
	Schema: map[string]rulekit.FieldDef{
		"score":   {Type: rulekit.FieldTypeNumber, Direction: rulekit.DirectionInput},
		"tag_a":   {Type: rulekit.FieldTypeBoolean, Direction: rulekit.DirectionOutput},
		"tag_b":   {Type: rulekit.FieldTypeBoolean, Direction: rulekit.DirectionOutput},
	},
	Entry: "node-1",
	Nodes: []rulekit.RuleNode{
		{
			ID:       "node-1",
			Strategy: rulekit.StrategyAllMatches,
			Rules: []rulekit.Rule{
				{
					ID:   "node-1_r0",
					When: []rulekit.Condition{{Field: "score", Op: "gte", Value: 50.0}},
					Then: map[string]any{"tag_a": true},
				},
				{
					ID:   "node-1_r1",
					When: []rulekit.Condition{{Field: "score", Op: "gte", Value: 80.0}},
					Then: map[string]any{"tag_b": true},
				},
			},
		},
	},
}

// ---- Load tests -------------------------------------------------------------

func TestLoad_ValidSingleNode(t *testing.T) {
	data := marshalDSL(t, singleNodeFirstMatch)
	rs, err := rulekit.Load(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rs == nil {
		t.Fatal("expected non-nil Ruleset")
	}
}

func TestLoad_InvalidDSLVersion(t *testing.T) {
	dsl := singleNodeFirstMatch
	dsl.DSLVersion = "v2"
	data := marshalDSL(t, dsl)
	_, err := rulekit.Load(data)
	if err == nil {
		t.Fatal("expected error for invalid dsl_version")
	}
}

func TestLoad_EmptySchema(t *testing.T) {
	dsl := singleNodeFirstMatch
	dsl.Schema = map[string]rulekit.FieldDef{}
	data := marshalDSL(t, dsl)
	_, err := rulekit.Load(data)
	if err == nil {
		t.Fatal("expected error for empty schema")
	}
}

func TestLoad_EntryNodeMissing(t *testing.T) {
	dsl := singleNodeFirstMatch
	dsl.Entry = "does-not-exist"
	data := marshalDSL(t, dsl)
	_, err := rulekit.Load(data)
	if err == nil {
		t.Fatal("expected error for missing entry node")
	}
}

func TestLoad_DuplicateNodeID(t *testing.T) {
	dsl := singleNodeFirstMatch
	dsl.Nodes = append(dsl.Nodes, dsl.Nodes[0])
	data := marshalDSL(t, dsl)
	_, err := rulekit.Load(data)
	if err == nil {
		t.Fatal("expected error for duplicate node ID")
	}
}

func TestLoad_SelfLoopEdge(t *testing.T) {
	dsl := singleNodeFirstMatch
	dsl.Edges = []rulekit.Edge{{From: "node-1", To: "node-1"}}
	data := marshalDSL(t, dsl)
	_, err := rulekit.Load(data)
	if err == nil {
		t.Fatal("expected error for self-loop edge")
	}
}

func TestLoad_EdgeMapUnknownField(t *testing.T) {
	dsl := rulekit.DSL{
		DSLVersion: "v1",
		Schema: map[string]rulekit.FieldDef{
			"x": {Type: rulekit.FieldTypeNumber, Direction: rulekit.DirectionInput},
			"y": {Type: rulekit.FieldTypeNumber, Direction: rulekit.DirectionOutput},
		},
		Entry: "n1",
		Nodes: []rulekit.RuleNode{
			{ID: "n1", Strategy: rulekit.StrategyFirstMatch, Rules: []rulekit.Rule{}},
			{ID: "n2", Strategy: rulekit.StrategyFirstMatch, Rules: []rulekit.Rule{}},
		},
		Edges: []rulekit.Edge{
			{From: "n1", To: "n2", Map: map[string]string{"not_in_schema": "x"}},
		},
	}
	data := marshalDSL(t, dsl)
	_, err := rulekit.Load(data)
	if err == nil {
		t.Fatal("expected error for edge map referencing unknown schema field")
	}
}

func TestLoad_InvalidOperator(t *testing.T) {
	dsl := rulekit.DSL{
		DSLVersion: "v1",
		Schema: map[string]rulekit.FieldDef{
			"amount": {Type: rulekit.FieldTypeNumber, Direction: rulekit.DirectionInput},
			"tier":   {Type: rulekit.FieldTypeString, Direction: rulekit.DirectionOutput},
		},
		Entry: "node-1",
		Nodes: []rulekit.RuleNode{
			{
				ID:       "node-1",
				Strategy: rulekit.StrategyFirstMatch,
				Rules: []rulekit.Rule{
					{
						ID:   "node-1_r0",
						Name: "bad",
						When: []rulekit.Condition{{Field: "amount", Op: "contains", Value: 100.0}}, // invalid for number
						Then: map[string]any{"tier": "x"},
					},
				},
			},
		},
	}
	data := marshalDSL(t, dsl)
	_, err := rulekit.Load(data)
	if err == nil {
		t.Fatal("expected error for invalid operator")
	}
}

func TestLoad_EnumMissingOptions(t *testing.T) {
	dsl := rulekit.DSL{
		DSLVersion: "v1",
		Schema: map[string]rulekit.FieldDef{
			"amount": {Type: rulekit.FieldTypeNumber, Direction: rulekit.DirectionInput},
			"tier":   {Type: rulekit.FieldTypeString, Direction: rulekit.DirectionOutput},
			"color":  {Type: rulekit.FieldTypeEnum, Direction: rulekit.DirectionInput}, // missing Options
		},
		Entry: "node-1",
		Nodes: singleNodeFirstMatch.Nodes,
	}
	data := marshalDSL(t, dsl)
	_, err := rulekit.Load(data)
	if err == nil {
		t.Fatal("expected error for enum field without options")
	}
}

func TestLoad_InOperatorNonStringSlice(t *testing.T) {
	dsl := rulekit.DSL{
		DSLVersion: "v1",
		Schema: map[string]rulekit.FieldDef{
			"status": {Type: rulekit.FieldTypeEnum, Direction: rulekit.DirectionInput, Options: []string{"a", "b"}},
			"out":    {Type: rulekit.FieldTypeString, Direction: rulekit.DirectionOutput},
		},
		Entry: "n1",
		Nodes: []rulekit.RuleNode{
			{
				ID:       "n1",
				Strategy: rulekit.StrategyFirstMatch,
				Rules: []rulekit.Rule{
					{
						ID:   "n1_r0",
						When: []rulekit.Condition{{Field: "status", Op: "in", Value: "not-a-slice"}},
						Then: map[string]any{"out": "x"},
					},
				},
			},
		},
	}
	data := marshalDSL(t, dsl)
	_, err := rulekit.Load(data)
	if err == nil {
		t.Fatal("expected error for 'in' with non-slice value")
	}
}

// ---- Eval tests -------------------------------------------------------------

func TestEval_FirstMatch(t *testing.T) {
	data := marshalDSL(t, singleNodeFirstMatch)
	rs := must(rulekit.Load(data))

	result, err := rs.Eval(ctx(), map[string]any{"amount": 1500.0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["tier"] != "premium" {
		t.Errorf("expected tier=premium, got %v", result["tier"])
	}

	result, err = rs.Eval(ctx(), map[string]any{"amount": 50.0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["tier"] != "standard" {
		t.Errorf("expected tier=standard, got %v", result["tier"])
	}
}

func TestEval_FirstMatch_StopsAtFirst(t *testing.T) {
	// Both rules could match if evaluation continued; first_match should stop.
	dsl := rulekit.DSL{
		DSLVersion: "v1",
		Schema: map[string]rulekit.FieldDef{
			"x": {Type: rulekit.FieldTypeNumber, Direction: rulekit.DirectionInput},
			"y": {Type: rulekit.FieldTypeString, Direction: rulekit.DirectionOutput},
		},
		Entry: "n1",
		Nodes: []rulekit.RuleNode{
			{
				ID:       "n1",
				Strategy: rulekit.StrategyFirstMatch,
				Rules: []rulekit.Rule{
					{ID: "n1_r0", When: []rulekit.Condition{{Field: "x", Op: "gt", Value: 0.0}}, Then: map[string]any{"y": "first"}},
					{ID: "n1_r1", When: []rulekit.Condition{{Field: "x", Op: "gt", Value: 0.0}}, Then: map[string]any{"y": "second"}},
				},
			},
		},
	}
	rs := must(rulekit.Load(marshalDSL(t, dsl)))
	result, err := rs.Eval(ctx(), map[string]any{"x": 1.0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["y"] != "first" {
		t.Errorf("expected y=first (first_match stopped at first), got %v", result["y"])
	}
}

func TestEval_AllMatches(t *testing.T) {
	data := marshalDSL(t, singleNodeAllMatches)
	rs := must(rulekit.Load(data))

	result, err := rs.Eval(ctx(), map[string]any{"score": 90.0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["tag_a"] != true || result["tag_b"] != true {
		t.Errorf("expected both tags true, got %v", result)
	}

	result, err = rs.Eval(ctx(), map[string]any{"score": 60.0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["tag_a"] != true {
		t.Errorf("expected tag_a=true, got %v", result)
	}
	if _, ok := result["tag_b"]; ok {
		t.Errorf("expected tag_b absent, got %v", result["tag_b"])
	}
}

func TestEval_DefaultApplied(t *testing.T) {
	dsl := rulekit.DSL{
		DSLVersion: "v1",
		Schema: map[string]rulekit.FieldDef{
			"x":   {Type: rulekit.FieldTypeNumber, Direction: rulekit.DirectionInput},
			"out": {Type: rulekit.FieldTypeString, Direction: rulekit.DirectionOutput},
		},
		Entry: "n1",
		Nodes: []rulekit.RuleNode{
			{
				ID:       "n1",
				Strategy: rulekit.StrategyFirstMatch,
				Rules: []rulekit.Rule{
					{ID: "n1_r0", When: []rulekit.Condition{{Field: "x", Op: "gt", Value: 100.0}}, Then: map[string]any{"out": "big"}},
				},
				Default: map[string]any{"out": "default"},
			},
		},
	}
	rs := must(rulekit.Load(marshalDSL(t, dsl)))
	result, err := rs.Eval(ctx(), map[string]any{"x": 1.0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["out"] != "default" {
		t.Errorf("expected out=default, got %v", result["out"])
	}
}

func TestEval_MissingInputFieldZeroValue(t *testing.T) {
	data := marshalDSL(t, singleNodeFirstMatch)
	rs := must(rulekit.Load(data))

	// amount missing → zero (0) → matches rule with "lt 1000"
	result, err := rs.Eval(ctx(), map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["tier"] != "standard" {
		t.Errorf("expected tier=standard for zero amount, got %v", result["tier"])
	}
}

func TestEval_UnknownInputFieldIgnored(t *testing.T) {
	data := marshalDSL(t, singleNodeFirstMatch)
	rs := must(rulekit.Load(data))

	_, err := rs.Eval(ctx(), map[string]any{"amount": 500.0, "unknown_field": "ignored"})
	if err != nil {
		t.Fatalf("expected no error for unknown input field, got: %v", err)
	}
}

func TestEval_TypeMismatchError(t *testing.T) {
	data := marshalDSL(t, singleNodeFirstMatch)
	rs := must(rulekit.Load(data))

	_, err := rs.Eval(ctx(), map[string]any{"amount": "not-a-number"})
	if err == nil {
		t.Fatal("expected error for type mismatch")
	}
}

func TestEval_StringOperators(t *testing.T) {
	dsl := rulekit.DSL{
		DSLVersion: "v1",
		Schema: map[string]rulekit.FieldDef{
			"name": {Type: rulekit.FieldTypeString, Direction: rulekit.DirectionInput},
			"out":  {Type: rulekit.FieldTypeString, Direction: rulekit.DirectionOutput},
		},
		Entry: "n1",
		Nodes: []rulekit.RuleNode{
			{
				ID:       "n1",
				Strategy: rulekit.StrategyFirstMatch,
				Rules: []rulekit.Rule{
					{ID: "n1_r0", When: []rulekit.Condition{{Field: "name", Op: "starts_with", Value: "foo"}}, Then: map[string]any{"out": "prefix"}},
					{ID: "n1_r1", When: []rulekit.Condition{{Field: "name", Op: "ends_with", Value: "bar"}}, Then: map[string]any{"out": "suffix"}},
					{ID: "n1_r2", When: []rulekit.Condition{{Field: "name", Op: "contains", Value: "baz"}}, Then: map[string]any{"out": "contains"}},
				},
			},
		},
	}
	rs := must(rulekit.Load(marshalDSL(t, dsl)))

	for _, tc := range []struct{ input, want string }{
		{"fooXYZ", "prefix"},
		{"XYZbar", "suffix"},
		{"XbazY", "contains"},
	} {
		result, err := rs.Eval(ctx(), map[string]any{"name": tc.input})
		if err != nil {
			t.Fatalf("input=%q: %v", tc.input, err)
		}
		if result["out"] != tc.want {
			t.Errorf("input=%q: expected out=%q, got %v", tc.input, tc.want, result["out"])
		}
	}
}

func TestEval_EnumInOperator(t *testing.T) {
	dsl := rulekit.DSL{
		DSLVersion: "v1",
		Schema: map[string]rulekit.FieldDef{
			"status": {Type: rulekit.FieldTypeEnum, Direction: rulekit.DirectionInput, Options: []string{"active", "pending", "closed"}},
			"flag":   {Type: rulekit.FieldTypeBoolean, Direction: rulekit.DirectionOutput},
		},
		Entry: "n1",
		Nodes: []rulekit.RuleNode{
			{
				ID:       "n1",
				Strategy: rulekit.StrategyFirstMatch,
				Rules: []rulekit.Rule{
					{
						ID:   "n1_r0",
						When: []rulekit.Condition{{Field: "status", Op: "in", Value: []any{"active", "pending"}}},
						Then: map[string]any{"flag": true},
					},
				},
				Default: map[string]any{"flag": false},
			},
		},
	}
	rs := must(rulekit.Load(marshalDSL(t, dsl)))

	result := must(rs.Eval(ctx(), map[string]any{"status": "active"}))
	if result["flag"] != true {
		t.Errorf("expected flag=true for active, got %v", result["flag"])
	}
	result = must(rs.Eval(ctx(), map[string]any{"status": "closed"}))
	if result["flag"] != false {
		t.Errorf("expected flag=false for closed, got %v", result["flag"])
	}
}

func TestEval_MultiNodeLinearGraph(t *testing.T) {
	dsl := rulekit.DSL{
		DSLVersion: "v1",
		Schema: map[string]rulekit.FieldDef{
			"amount":   {Type: rulekit.FieldTypeNumber, Direction: rulekit.DirectionInput},
			"tier":     {Type: rulekit.FieldTypeString, Direction: rulekit.DirectionOutput},
			"discount": {Type: rulekit.FieldTypeNumber, Direction: rulekit.DirectionOutput},
		},
		Entry: "node-tier",
		Nodes: []rulekit.RuleNode{
			{
				ID:       "node-tier",
				Strategy: rulekit.StrategyFirstMatch,
				Rules: []rulekit.Rule{
					{ID: "node-tier_r0", When: []rulekit.Condition{{Field: "amount", Op: "gte", Value: 1000.0}}, Then: map[string]any{"tier": "premium"}},
				},
				Default: map[string]any{"tier": "standard"},
			},
			{
				ID:       "node-discount",
				Strategy: rulekit.StrategyFirstMatch,
				Rules: []rulekit.Rule{
					{ID: "node-discount_r0", When: []rulekit.Condition{{Field: "tier", Op: "eq", Value: "premium"}}, Then: map[string]any{"discount": 20.0}},
				},
				Default: map[string]any{"discount": 5.0},
			},
		},
		Edges: []rulekit.Edge{
			{From: "node-tier", To: "node-discount"},
		},
	}
	rs := must(rulekit.Load(marshalDSL(t, dsl)))

	result := must(rs.Eval(ctx(), map[string]any{"amount": 2000.0}))
	if result["tier"] != "premium" || result["discount"] != 20.0 {
		t.Errorf("expected tier=premium, discount=20; got %v", result)
	}

	result = must(rs.Eval(ctx(), map[string]any{"amount": 10.0}))
	if result["tier"] != "standard" || result["discount"] != 5.0 {
		t.Errorf("expected tier=standard, discount=5; got %v", result)
	}
}

func TestEval_MultiNodeEdgeMap(t *testing.T) {
	// edge.Map renames output field "tier" to input field "input_tier" for node-2.
	dsl := rulekit.DSL{
		DSLVersion: "v1",
		Schema: map[string]rulekit.FieldDef{
			"amount":     {Type: rulekit.FieldTypeNumber, Direction: rulekit.DirectionInput},
			"tier":       {Type: rulekit.FieldTypeString, Direction: rulekit.DirectionOutput},
			"input_tier": {Type: rulekit.FieldTypeString, Direction: rulekit.DirectionInput},
			"discount":   {Type: rulekit.FieldTypeNumber, Direction: rulekit.DirectionOutput},
		},
		Entry: "n1",
		Nodes: []rulekit.RuleNode{
			{
				ID:       "n1",
				Strategy: rulekit.StrategyFirstMatch,
				Rules:    []rulekit.Rule{{ID: "n1_r0", When: []rulekit.Condition{{Field: "amount", Op: "gte", Value: 100.0}}, Then: map[string]any{"tier": "gold"}}},
				Default:  map[string]any{"tier": "silver"},
			},
			{
				ID:       "n2",
				Strategy: rulekit.StrategyFirstMatch,
				Rules:    []rulekit.Rule{{ID: "n2_r0", When: []rulekit.Condition{{Field: "input_tier", Op: "eq", Value: "gold"}}, Then: map[string]any{"discount": 15.0}}},
				Default:  map[string]any{"discount": 0.0},
			},
		},
		Edges: []rulekit.Edge{
			{From: "n1", To: "n2", Map: map[string]string{"tier": "input_tier"}},
		},
	}
	rs := must(rulekit.Load(marshalDSL(t, dsl)))

	result := must(rs.Eval(ctx(), map[string]any{"amount": 200.0}))
	if result["discount"] != 15.0 {
		t.Errorf("expected discount=15, got %v", result)
	}
}

// ---- Client tests -----------------------------------------------------------

func setupTestProject(t *testing.T, dslData []byte) (string, string) {
	t.Helper()
	dir := t.TempDir()

	rulekitDir := filepath.Join(dir, ".rulekit", "pricing")
	if err := os.MkdirAll(rulekitDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	dslPath := filepath.Join(rulekitDir, "dsl.json")
	if err := os.WriteFile(dslPath, dslData, 0o644); err != nil {
		t.Fatalf("write dsl.json: %v", err)
	}

	sum := sha256.Sum256(dslData)
	checksum := "sha256:" + hex.EncodeToString(sum[:])

	lock := map[string]any{
		"registry":  "https://registry.example.com",
		"workspace": "default",
		"rulesets": map[string]any{
			"pricing": map[string]any{
				"version":   1,
				"checksum":  checksum,
				"pulled_at": "2026-04-08T14:41:37Z",
			},
		},
	}
	lockData, _ := json.Marshal(lock)
	if err := os.WriteFile(filepath.Join(dir, "rulekit.lock"), lockData, 0o644); err != nil {
		t.Fatalf("write rulekit.lock: %v", err)
	}

	return dir, checksum
}

func TestClient_NewAt_Eval(t *testing.T) {
	dslData := marshalDSL(t, singleNodeFirstMatch)
	dir, _ := setupTestProject(t, dslData)

	client, err := rulekit.NewAt(dir)
	if err != nil {
		t.Fatalf("NewAt: %v", err)
	}

	result, err := client.Eval(ctx(), "pricing", map[string]any{"amount": 2000.0})
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if result["tier"] != "premium" {
		t.Errorf("expected tier=premium, got %v", result["tier"])
	}
}

func TestClient_CachesRuleset(t *testing.T) {
	dslData := marshalDSL(t, singleNodeFirstMatch)
	dir, _ := setupTestProject(t, dslData)

	client, err := rulekit.NewAt(dir)
	if err != nil {
		t.Fatalf("NewAt: %v", err)
	}

	// First call loads from disk.
	if _, err := client.Eval(ctx(), "pricing", map[string]any{"amount": 100.0}); err != nil {
		t.Fatalf("first Eval: %v", err)
	}

	// Delete the dsl.json — second call must still succeed from cache.
	if err := os.Remove(filepath.Join(dir, ".rulekit", "pricing", "dsl.json")); err != nil {
		t.Fatalf("remove dsl.json: %v", err)
	}

	if _, err := client.Eval(ctx(), "pricing", map[string]any{"amount": 100.0}); err != nil {
		t.Fatalf("second Eval (should use cache): %v", err)
	}
}

func TestClient_ChecksumMismatch(t *testing.T) {
	dslData := marshalDSL(t, singleNodeFirstMatch)
	dir, _ := setupTestProject(t, dslData)

	// Tamper with dsl.json after computing correct checksum.
	tampered := append(dslData, ' ')
	if err := os.WriteFile(filepath.Join(dir, ".rulekit", "pricing", "dsl.json"), tampered, 0o644); err != nil {
		t.Fatalf("write tampered dsl.json: %v", err)
	}

	client, err := rulekit.NewAt(dir)
	if err != nil {
		t.Fatalf("NewAt: %v", err)
	}

	_, err = client.Eval(ctx(), "pricing", map[string]any{"amount": 100.0})
	if err == nil {
		t.Fatal("expected checksum mismatch error")
	}
	if !isChecksumMismatch(err) {
		t.Errorf("expected ErrChecksumMismatch, got %v", err)
	}
}

func TestClient_WithVerifyChecksumFalse(t *testing.T) {
	dslData := marshalDSL(t, singleNodeFirstMatch)
	dir, _ := setupTestProject(t, dslData)

	// Tamper with dsl.json — checksum verification disabled, should still succeed.
	tampered := marshalDSL(t, singleNodeFirstMatch) // same content, different bytes due to re-marshal? same. Let's append space.
	tampered = append(tampered, ' ')
	if err := os.WriteFile(filepath.Join(dir, ".rulekit", "pricing", "dsl.json"), tampered, 0o644); err != nil {
		t.Fatalf("write tampered dsl.json: %v", err)
	}

	client, err := rulekit.NewAt(dir, rulekit.WithVerifyChecksum(false))
	if err != nil {
		t.Fatalf("NewAt: %v", err)
	}

	_, err = client.Eval(ctx(), "pricing", map[string]any{"amount": 100.0})
	if err != nil {
		t.Fatalf("expected success with checksum verification disabled: %v", err)
	}
}

func TestClient_New_NoDotRulekit(t *testing.T) {
	// Ensure New() errors when .rulekit/ is not found.
	// We temporarily change to a temp dir with no .rulekit/ anywhere above it.
	tmp := t.TempDir()
	orig, _ := os.Getwd()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	_, err := rulekit.New()
	if err == nil {
		t.Fatal("expected error when no .rulekit/ found")
	}
}

func TestClient_MissingRulesetInLock(t *testing.T) {
	dslData := marshalDSL(t, singleNodeFirstMatch)
	dir, _ := setupTestProject(t, dslData)

	client, err := rulekit.NewAt(dir)
	if err != nil {
		t.Fatalf("NewAt: %v", err)
	}

	_, err = client.Eval(ctx(), "nonexistent", map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing ruleset key in lock")
	}
}

func isChecksumMismatch(err error) bool {
	for err != nil {
		if err == rulekit.ErrChecksumMismatch {
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			break
		}
		err = u.Unwrap()
	}
	return false
}
