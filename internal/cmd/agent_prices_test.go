package cmd

import "testing"

// A price of zero is how the configuration says it does not know the price,
// so it is written as not set rather than as free.
func TestAPriceOfZeroIsNotSetNotFree(t *testing.T) {
	for _, each := range []struct {
		price float64
		want  string
	}{
		{0, "not set"},
		{0.25, "0.25"},
		{12, "12"},
		{0.075, "0.075"},
	} {
		if got := formatPricePerMillion(each.price); got != each.want {
			t.Errorf("%v: %q, want %q", each.price, got, each.want)
		}
	}
}
