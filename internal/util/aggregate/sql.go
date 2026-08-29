package aggregate

import (
	"fmt"
	"strings"
)

// Columns says which fields a caller may name, mapping the name used in the
// API to the column in the table.
//
// An allow list rather than an escape: a field name reaches SQL as an
// identifier, and identifiers cannot be parameterised. Every value goes
// through a placeholder, but the column a caller asks to sort by has to be one
// this table actually offered — otherwise "sort by" is "run this".
type Columns map[string]string

// Condition is a WHERE fragment and the values its placeholders take.
type Condition struct {
	SQL    string
	Values []any
}

// BuildFilter turns a filter tree into a condition.
func BuildFilter(filter *Filter, columns Columns) (*Condition, error) {
	if filter == nil {
		return &Condition{SQL: "", Values: nil}, nil
	}
	if err := filter.Validate(); err != nil {
		return nil, err
	}

	switch filter.Operation {
	case OperationAnd, OperationOr:
		joiner := " AND "
		if filter.Operation == OperationOr {
			joiner = " OR "
		}
		parts := make([]string, 0, len(filter.Filters))
		var values []any
		for _, operand := range filter.Filters {
			built, err := BuildFilter(operand, columns)
			if err != nil {
				return nil, err
			}
			parts = append(parts, built.SQL)
			values = append(values, built.Values...)
		}
		return &Condition{SQL: "(" + strings.Join(parts, joiner) + ")", Values: values}, nil

	case OperationNot:
		// n-ary, like the reference: NOT over the conjunction of its operands.
		inner, err := BuildFilter(&Filter{Operation: OperationAnd, Filters: filter.Filters}, columns)
		if err != nil {
			return nil, err
		}
		return &Condition{SQL: "NOT " + inner.SQL, Values: inner.Values}, nil
	}

	column, err := columns.resolve(filter.Field)
	if err != nil {
		return nil, err
	}

	switch filter.Operation {
	case OperationIsNull:
		return &Condition{SQL: fmt.Sprintf("%s IS NULL", column)}, nil
	case OperationNotNull:
		return &Condition{SQL: fmt.Sprintf("%s IS NOT NULL", column)}, nil

	case OperationEqual:
		return &Condition{SQL: fmt.Sprintf("%s = ?", column), Values: []any{*filter.Value}}, nil
	case OperationNotEqual:
		return &Condition{SQL: fmt.Sprintf("%s <> ?", column), Values: []any{*filter.Value}}, nil
	case OperationLess:
		return &Condition{SQL: fmt.Sprintf("%s < ?", column), Values: []any{*filter.Value}}, nil
	case OperationLessEqual:
		return &Condition{SQL: fmt.Sprintf("%s <= ?", column), Values: []any{*filter.Value}}, nil
	case OperationGreater:
		return &Condition{SQL: fmt.Sprintf("%s > ?", column), Values: []any{*filter.Value}}, nil
	case OperationGreaterEqual:
		return &Condition{SQL: fmt.Sprintf("%s >= ?", column), Values: []any{*filter.Value}}, nil

	case OperationContains:
		// ILIKE with the pattern characters in the value escaped, so a
		// filter for "50%" is a search for "50%" and not for "50 anything".
		return &Condition{
			SQL:    fmt.Sprintf("%s ILIKE ? ESCAPE '\\'", column),
			Values: []any{"%" + escapeLike(*filter.Value) + "%"},
		}, nil

	case OperationIn:
		return &Condition{SQL: fmt.Sprintf("%s IN (?)", column), Values: []any{toAny(filter.Values)}}, nil
	case OperationNotIn:
		return &Condition{SQL: fmt.Sprintf("%s NOT IN (?)", column), Values: []any{toAny(filter.Values)}}, nil
	}

	return nil, fmt.Errorf("aggregate: %q is not an operation", filter.Operation)
}

// BuildSort turns sort terms into an ORDER BY clause.
func BuildSort(orders []*Order, columns Columns) (string, error) {
	if len(orders) == 0 {
		return "", nil
	}
	parts := make([]string, 0, len(orders))
	for _, order := range orders {
		column, err := columns.resolve(order.Field)
		if err != nil {
			return "", err
		}
		direction := "ASC"
		if order.Direction == DirectionDescending {
			direction = "DESC"
		}
		// NULLS LAST in both directions: a row with no value is not the
		// most interesting thing in the table, whichever way it is sorted.
		parts = append(parts, fmt.Sprintf("%s %s NULLS LAST", column, direction))
	}
	return strings.Join(parts, ", "), nil
}

// BuildDistinct returns the grouped columns for a facet query.
func BuildDistinct(fields []string, columns Columns) (string, error) {
	if len(fields) == 0 {
		return "", nil
	}
	parts := make([]string, 0, len(fields))
	for _, field := range fields {
		column, err := columns.resolve(field)
		if err != nil {
			return "", err
		}
		parts = append(parts, column)
	}
	return strings.Join(parts, ", "), nil
}

func (self Columns) resolve(field string) (string, error) {
	column, ok := self[strings.TrimSpace(field)]
	if !ok {
		return "", fmt.Errorf("aggregate: %q is not a field that can be filtered or sorted here", field)
	}
	return column, nil
}

// escapeLike neutralises the wildcards, so the value is matched literally.
func escapeLike(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(value)
}

func toAny(values []string) []any {
	converted := make([]any, len(values))
	for index, value := range values {
		converted[index] = value
	}
	return converted
}
