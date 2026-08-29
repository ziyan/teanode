package aggregate_test

import (
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/util/aggregate"
)

// A field name reaches SQL as an identifier, and an identifier cannot be
// parameterised. The only defence is that the name has to be one the table
// offered, so this checks the gate rather than the escaping — there is no
// escaping to check.
func TestFieldNamesCannotCarrySql(t *testing.T) {
	t.Parallel()
	columns := aggregate.Columns{"status": "\"status\"", "receivedAt": "\"received_at\""}

	hostile := []string{
		"status; DROP TABLE mail --",
		"status\" ; DROP TABLE mail --",
		"(SELECT password_hash FROM users)",
		"status UNION SELECT 1",
		"1=1",
		"*",
		"status)",
		"pg_sleep(10)",
	}

	for _, field := range hostile {
		value := "anything"
		filter := &aggregate.Filter{Operation: aggregate.OperationEqual, Field: field, Value: &value}
		if _, err := aggregate.BuildFilter(filter, columns); err == nil {
			t.Errorf("a filter naming %q was accepted", field)
		}
		orders := []*aggregate.Order{{Field: field, Direction: aggregate.DirectionAscending}}
		if _, err := aggregate.BuildSort(orders, columns); err == nil {
			t.Errorf("a sort naming %q was accepted", field)
		}
		if _, err := aggregate.BuildDistinct([]string{field}, columns); err == nil {
			t.Errorf("a distinct naming %q was accepted", field)
		}
	}
}

// A value, unlike a field name, may be anything at all: it goes to a
// placeholder and never to the statement.
func TestValuesNeverReachTheStatement(t *testing.T) {
	t.Parallel()
	columns := aggregate.Columns{"status": "\"status\""}

	value := "'; DROP TABLE mail; --"
	built, err := aggregate.BuildFilter(
		&aggregate.Filter{Operation: aggregate.OperationEqual, Field: "status", Value: &value},
		columns,
	)
	if err != nil {
		t.Fatalf("a hostile value was refused, when it should simply be passed as a parameter: %s", err)
	}
	if strings.Contains(built.SQL, "DROP") {
		t.Errorf("the value was interpolated into the statement: %q", built.SQL)
	}
	if built.SQL != "\"status\" = ?" {
		t.Errorf("statement is %q, want a placeholder", built.SQL)
	}
	if len(built.Values) != 1 || built.Values[0] != value {
		t.Errorf("values are %v, want the hostile string carried as a parameter", built.Values)
	}
}

// The sort direction is the one other thing that reaches the statement without
// a placeholder, so it must never be anything but ASC or DESC.
func TestSortDirectionIsClosed(t *testing.T) {
	t.Parallel()
	columns := aggregate.Columns{"status": "\"status\""}

	stage := &aggregate.Stage{Sort: []*aggregate.Order{{Field: "status", Direction: "ascending; DROP TABLE mail"}}}
	if err := stage.Validate(); err == nil {
		t.Error("a sort direction carrying SQL was accepted")
	}

	built, err := aggregate.BuildSort([]*aggregate.Order{{Field: "status", Direction: "ascending; DROP TABLE mail"}}, columns)
	if err == nil && strings.Contains(built, "DROP") {
		t.Errorf("an unvalidated direction reached the statement: %q", built)
	}
}
