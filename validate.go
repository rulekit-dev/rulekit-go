package rulekit

import "fmt"

var validFieldTypes = map[FieldType]bool{
	FieldTypeNumber:  true,
	FieldTypeString:  true,
	FieldTypeBoolean: true,
	FieldTypeEnum:    true,
}

var validDirections = map[Direction]bool{
	DirectionInput:  true,
	DirectionOutput: true,
}

var validOpsForType = map[FieldType]map[string]bool{
	FieldTypeNumber: {
		"eq": true, "ne": true, "gt": true, "gte": true, "lt": true, "lte": true,
	},
	FieldTypeString: {
		"eq": true, "ne": true, "contains": true, "starts_with": true, "ends_with": true,
	},
	FieldTypeBoolean: {
		"eq": true, "ne": true,
	},
	FieldTypeEnum: {
		"eq": true, "ne": true, "in": true,
	},
}

func validateDSL(dsl *DSL) error {
	if dsl.DSLVersion != "v1" {
		return fmt.Errorf("rulekit: dsl_version must be \"v1\", got %q", dsl.DSLVersion)
	}

	if len(dsl.Schema) == 0 {
		return fmt.Errorf("rulekit: schema must not be empty")
	}

	for fieldName, fieldDef := range dsl.Schema {
		if !validFieldTypes[fieldDef.Type] {
			return fmt.Errorf("rulekit: schema field %q: invalid type %q", fieldName, fieldDef.Type)
		}
		if !validDirections[fieldDef.Direction] {
			return fmt.Errorf("rulekit: schema field %q: invalid direction %q", fieldName, fieldDef.Direction)
		}
		if fieldDef.Type == FieldTypeEnum && len(fieldDef.Options) == 0 {
			return fmt.Errorf("rulekit: schema field %q: enum type requires non-empty options", fieldName)
		}
	}

	if len(dsl.Nodes) == 0 {
		return fmt.Errorf("rulekit: nodes must not be empty")
	}

	nodeIDs := make(map[string]bool, len(dsl.Nodes))
	for _, node := range dsl.Nodes {
		if nodeIDs[node.ID] {
			return fmt.Errorf("rulekit: duplicate node ID %q", node.ID)
		}
		nodeIDs[node.ID] = true
	}

	if !nodeIDs[dsl.Entry] {
		return fmt.Errorf("rulekit: entry node %q not found in nodes list", dsl.Entry)
	}

	for _, node := range dsl.Nodes {
		if err := validateNode(&node, dsl); err != nil {
			return err
		}
	}

	for i, edge := range dsl.Edges {
		if !nodeIDs[edge.From] {
			return fmt.Errorf("rulekit: edge %d: from node %q not found", i, edge.From)
		}
		if !nodeIDs[edge.To] {
			return fmt.Errorf("rulekit: edge %d: to node %q not found", i, edge.To)
		}
		if edge.From == edge.To {
			return fmt.Errorf("rulekit: edge %d: self-loop on node %q", i, edge.From)
		}
		for outField := range edge.Map {
			if _, ok := dsl.Schema[outField]; !ok {
				return fmt.Errorf("rulekit: edge %d: map key %q not found in schema", i, outField)
			}
		}
	}

	return nil
}

func validateNode(node *RuleNode, dsl *DSL) error {
	if node.Strategy != StrategyFirstMatch && node.Strategy != StrategyAllMatches {
		return fmt.Errorf("rulekit: node %s: invalid strategy %q", node.ID, node.Strategy)
	}

	for _, rule := range node.Rules {
		if err := validateRule(&rule, node, dsl); err != nil {
			return err
		}
	}
	return nil
}

func validateRule(rule *Rule, node *RuleNode, dsl *DSL) error {
	for i, cond := range rule.When {
		if err := validateCondition(cond, i, rule, node, dsl); err != nil {
			return err
		}
	}
	return nil
}

func validateCondition(cond Condition, idx int, rule *Rule, node *RuleNode, dsl *DSL) error {
	fieldDef, ok := dsl.Schema[cond.Field]
	if !ok {
		return fmt.Errorf("rulekit: node %s, rule %s, condition %d: unknown field %q", node.ID, rule.ID, idx, cond.Field)
	}

	validOps := validOpsForType[fieldDef.Type]
	if !validOps[cond.Op] {
		return fmt.Errorf("rulekit: node %s, rule %s, condition %d: operator %q not valid for type %q", node.ID, rule.ID, idx, cond.Op, fieldDef.Type)
	}

	if cond.Op == "in" {
		if _, err := toStringSlice(cond.Value); err != nil {
			return fmt.Errorf("rulekit: node %s, rule %s, condition %d: \"in\" value must be []string: %w", node.ID, rule.ID, idx, err)
		}
	}

	return nil
}
