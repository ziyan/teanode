// Package aggregate is the shape a list query takes its filtering, sorting
// and faceting in.
//
// A pipeline is a list of stages; each stage is a match, a sort, or a
// distinct. It is the same shape the portal uses, so a reader who knows one
// knows the other, and a table's filter and sort state translates into it
// without the query needing an argument per column.
//
// The point of putting it on the query rather than filtering in the browser
// is that the browser only has the rows it fetched. "Which domains have mail"
// and "the oldest message" are questions about everything, and the answer
// changes when the answer is computed over the most recent five hundred.
package aggregate

import (
	"fmt"
	"strings"
)

// Operation is the operator of a filter node.
type Operation string

const (
	// Logical, over the operands in Filters.
	OperationAnd Operation = "and"
	OperationOr  Operation = "or"
	OperationNot Operation = "not"

	// Null checks, over Field.
	OperationIsNull  Operation = "isNull"
	OperationNotNull Operation = "notNull"

	// Comparisons of Field against Value.
	OperationEqual        Operation = "equal"
	OperationNotEqual     Operation = "notEqual"
	OperationLess         Operation = "less"
	OperationLessEqual    Operation = "lessEqual"
	OperationGreater      Operation = "greater"
	OperationGreaterEqual Operation = "greaterEqual"

	// Field against the list in Values.
	OperationIn    Operation = "in"
	OperationNotIn Operation = "notIn"

	// Case-insensitive substring, which is what a text filter in a column
	// header means when somebody types into it.
	OperationContains Operation = "contains"
)

// Direction is the direction of a sort.
type Direction string

const (
	DirectionAscending  Direction = "ascending"
	DirectionDescending Direction = "descending"
)

// Filter is one node of a boolean expression over a row's fields.
//
// Exactly one shape is valid at a time, decided by Operation: a logical node
// reads Filters, a null check reads Field, a comparison reads Field and
// Value, and a list check reads Field and Values.
type Filter struct {
	Operation Operation `json:"operation"`

	// Field is the column this node is about. Absent on a logical node,
	// which is about its operands rather than a column — so it is optional
	// in the schema, and Validate is what insists on it where it is needed.
	Field string `json:"field,omitempty" graphapi:"nullable"`

	// Value is the single operand of a comparison.
	Value *string `json:"value,omitempty"`

	// Values is the list an "in" is checked against.
	Values []string `json:"values,omitempty" graphapi:"nullable"`

	// Filters are the operands of a logical operation.
	Filters []*Filter `json:"filters,omitempty" graphapi:"nullable"`
}

// Order is one term of a sort.
type Order struct {
	Field string `json:"field"`

	// Ascending when omitted, which is what a first click on a column header
	// means.
	Direction Direction `json:"direction,omitempty" graphapi:"nullable"`
}

// Stage is one step of a pipeline. Exactly one of its three may be set,
// because a stage that both filtered and sorted would leave the order of the
// two to be guessed.
type Stage struct {
	// Match keeps the rows the filter accepts.
	Match *Filter `json:"match,omitempty"`

	// Sort orders the rows, first term first.
	Sort []*Order `json:"sort,omitempty" graphapi:"nullable"`

	// Distinct groups by these fields and counts each group, which is what
	// fills a filter menu with its options and the number beside each.
	Distinct []string `json:"distinct,omitempty" graphapi:"nullable"`
}

// Pipeline is the whole of it.
type Pipeline = []*Stage

// Validate reports the first thing wrong with a stage, so a caller gets one
// clear answer rather than a SQL error from three layers down.
func (self *Stage) Validate() error {
	set := 0
	if self.Match != nil {
		set++
	}
	if len(self.Sort) > 0 {
		set++
	}
	if len(self.Distinct) > 0 {
		set++
	}
	switch set {
	case 0:
		return fmt.Errorf("aggregate: a stage must be a match, a sort or a distinct")
	case 1:
	default:
		return fmt.Errorf("aggregate: a stage may only be one of a match, a sort or a distinct")
	}

	if self.Match != nil {
		return self.Match.Validate()
	}
	for _, order := range self.Sort {
		if order == nil || strings.TrimSpace(order.Field) == "" {
			return fmt.Errorf("aggregate: a sort term needs a field")
		}
		switch order.Direction {
		case "", DirectionAscending, DirectionDescending:
		default:
			return fmt.Errorf("aggregate: %q is not a sort direction", order.Direction)
		}
	}
	for _, field := range self.Distinct {
		if strings.TrimSpace(field) == "" {
			return fmt.Errorf("aggregate: a distinct needs a field")
		}
	}
	return nil
}

// Validate reports the first thing wrong with a filter tree.
func (self *Filter) Validate() error {
	switch self.Operation {
	case OperationAnd, OperationOr, OperationNot:
		if len(self.Filters) == 0 {
			return fmt.Errorf("aggregate: %q needs at least one operand", self.Operation)
		}
		for _, operand := range self.Filters {
			if operand == nil {
				return fmt.Errorf("aggregate: %q has an empty operand", self.Operation)
			}
			if err := operand.Validate(); err != nil {
				return err
			}
		}
		return nil

	case OperationIsNull, OperationNotNull:
		return self.requireField()

	case OperationEqual, OperationNotEqual, OperationLess, OperationLessEqual,
		OperationGreater, OperationGreaterEqual, OperationContains:
		if err := self.requireField(); err != nil {
			return err
		}
		if self.Value == nil {
			return fmt.Errorf("aggregate: %q needs a value", self.Operation)
		}
		return nil

	case OperationIn, OperationNotIn:
		if err := self.requireField(); err != nil {
			return err
		}
		if len(self.Values) == 0 {
			return fmt.Errorf("aggregate: %q needs at least one value", self.Operation)
		}
		return nil

	default:
		return fmt.Errorf("aggregate: %q is not an operation", self.Operation)
	}
}

func (self *Filter) requireField() error {
	if strings.TrimSpace(self.Field) == "" {
		return fmt.Errorf("aggregate: %q needs a field", self.Operation)
	}
	return nil
}
