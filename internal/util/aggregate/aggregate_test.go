package aggregate

import (
	"strings"
	"testing"
)

var columns = Columns{
	"status":     `"status"`,
	"kind":       `"kind"`,
	"subject":    `"subject"`,
	"receivedAt": `"received_at"`,
}

func value(text string) *string { return &text }

func TestBuildFilter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		filter *Filter
		sql    string
		values []any
	}{
		{
			name:   "equality",
			filter: &Filter{Operation: OperationEqual, Field: "status", Value: value("accepted")},
			sql:    `"status" = ?`,
			values: []any{"accepted"},
		},
		{
			name:   "a list",
			filter: &Filter{Operation: OperationIn, Field: "kind", Values: []string{"incoming", "outgoing"}},
			sql:    `"kind" IN (?)`,
			values: []any{[]any{"incoming", "outgoing"}},
		},
		{
			name:   "a substring",
			filter: &Filter{Operation: OperationContains, Field: "subject", Value: value("invoice")},
			sql:    `"subject" ILIKE ? ESCAPE '\'`,
			values: []any{"%invoice%"},
		},
		{
			name: "a conjunction",
			filter: &Filter{Operation: OperationAnd, Filters: []*Filter{
				{Operation: OperationEqual, Field: "status", Value: value("accepted")},
				{Operation: OperationNotNull, Field: "subject"},
			}},
			sql:    `("status" = ? AND "subject" IS NOT NULL)`,
			values: []any{"accepted"},
		},
		{
			name: "a negation",
			filter: &Filter{Operation: OperationNot, Filters: []*Filter{
				{Operation: OperationEqual, Field: "kind", Value: value("rua")},
			}},
			sql:    `NOT ("kind" = ?)`,
			values: []any{"rua"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			built, err := BuildFilter(test.filter, columns)
			if err != nil {
				t.Fatalf("BuildFilter: %s", err)
			}
			if built.SQL != test.sql {
				t.Errorf("sql is\n  %s\nwant\n  %s", built.SQL, test.sql)
			}
			if len(built.Values) != len(test.values) {
				t.Fatalf("values are %#v, want %#v", built.Values, test.values)
			}
		})
	}
}

// TestBuildRefusesUnknownFields is the property that makes this safe to expose.
//
// Values are parameterised, but a field name reaches SQL as an identifier and
// identifiers cannot be. So a caller may only name a column the table offered,
// and everything else — including anything that looks like an injection — is
// refused before it is built rather than escaped and hoped over.
func TestBuildRefusesUnknownFields(t *testing.T) {
	t.Parallel()

	hostile := []string{
		"password",
		`status"; DROP TABLE mail; --`,
		"1=1",
		"mail.status",
		"",
		"   ",
	}

	for _, field := range hostile {
		t.Run(field, func(t *testing.T) {
			if _, err := BuildFilter(&Filter{Operation: OperationEqual, Field: field, Value: value("x")}, columns); err == nil {
				t.Errorf("filtering on %q should be refused", field)
			}
			if _, err := BuildSort([]*Order{{Field: field}}, columns); err == nil {
				t.Errorf("sorting on %q should be refused", field)
			}
			if _, err := BuildDistinct([]string{field}, columns); err == nil {
				t.Errorf("grouping by %q should be refused", field)
			}
		})
	}
}

// TestContainsIsLiteral covers the wildcards a reader might type without
// meaning them: a filter for "50%" is a search for "50%".
func TestContainsIsLiteral(t *testing.T) {
	t.Parallel()

	built, err := BuildFilter(&Filter{Operation: OperationContains, Field: "subject", Value: value(`50% off_now\x`)}, columns)
	if err != nil {
		t.Fatalf("BuildFilter: %s", err)
	}
	pattern, ok := built.Values[0].(string)
	if !ok {
		t.Fatalf("expected a string pattern, got %#v", built.Values[0])
	}
	if pattern != `%50\% off\_now\\x%` {
		t.Errorf("pattern is %q, want the wildcards escaped", pattern)
	}
}

func TestBuildSort(t *testing.T) {
	t.Parallel()

	clause, err := BuildSort([]*Order{
		{Field: "status", Direction: DirectionAscending},
		{Field: "receivedAt", Direction: DirectionDescending},
	}, columns)
	if err != nil {
		t.Fatalf("BuildSort: %s", err)
	}
	if clause != `"status" ASC NULLS LAST, "received_at" DESC NULLS LAST` {
		t.Errorf("clause is %q", clause)
	}
}

// TestStageIsOneThing keeps a stage from being a match and a sort at once,
// which would leave the order of the two to be guessed.
func TestStageIsOneThing(t *testing.T) {
	t.Parallel()

	both := &Stage{
		Match: &Filter{Operation: OperationNotNull, Field: "subject"},
		Sort:  []*Order{{Field: "status"}},
	}
	if err := both.Validate(); err == nil {
		t.Error("a stage that both matches and sorts should be refused")
	}
	if err := (&Stage{}).Validate(); err == nil {
		t.Error("an empty stage should be refused")
	}
}

func TestFilterValidation(t *testing.T) {
	t.Parallel()

	broken := map[string]*Filter{
		"no operation":              {Field: "status", Value: value("x")},
		"unknown operator":          {Operation: "sudo", Field: "status", Value: value("x")},
		"equal with no value":       {Operation: OperationEqual, Field: "status"},
		"in with no values":         {Operation: OperationIn, Field: "status"},
		"and with no operands":      {Operation: OperationAnd},
		"and with an empty operand": {Operation: OperationAnd, Filters: []*Filter{nil}},
	}

	for name, filter := range broken {
		t.Run(name, func(t *testing.T) {
			if err := filter.Validate(); err == nil {
				t.Error("expected this to be refused")
			}
			if _, err := BuildFilter(filter, columns); err == nil {
				t.Error("expected building this to fail")
			}
		})
	}
}

// TestNothingUnparameterised is a blunt guard: every value a caller supplies
// has to arrive as a placeholder, never inside the SQL text.
func TestNothingUnparameterised(t *testing.T) {
	t.Parallel()

	built, err := BuildFilter(&Filter{Operation: OperationAnd, Filters: []*Filter{
		{Operation: OperationEqual, Field: "status", Value: value("'; DROP TABLE mail; --")},
		{Operation: OperationContains, Field: "subject", Value: value("' OR 1=1 --")},
		{Operation: OperationIn, Field: "kind", Values: []string{"'; DELETE FROM mail; --"}},
	}}, columns)
	if err != nil {
		t.Fatalf("BuildFilter: %s", err)
	}
	// Nothing the caller wrote may appear in the SQL text. The only quote in
	// it is the one in ESCAPE '\', which this code wrote itself.
	for _, supplied := range []string{"'; DROP TABLE mail; --", "' OR 1=1 --", "'; DELETE FROM mail; --"} {
		if strings.Contains(built.SQL, supplied) {
			t.Errorf("a caller's value reached the SQL text: %s", built.SQL)
		}
	}
	for _, forbidden := range []string{"DROP", "DELETE", "1=1"} {
		if strings.Contains(built.SQL, forbidden) {
			t.Errorf("%q reached the SQL text: %s", forbidden, built.SQL)
		}
	}

	// And each one is present as a parameter, so the query still asks what
	// was meant rather than quietly dropping it.
	if len(built.Values) != 3 {
		t.Errorf("expected three parameters, got %#v", built.Values)
	}
}
