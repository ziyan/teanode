package apigraph

import (
	"reflect"
	"testing"
)

// The dashboard's port is part of the origin a browser signs, so a server
// on a port other than 443 allows the relying party with that port too.
func TestDefaultPasskeyOriginsCarryTheDashboardPort(t *testing.T) {
	cases := []struct {
		listen string
		want   []string
	}{
		{":443", []string{"https://mail.example.com"}},
		{":10443", []string{"https://mail.example.com", "https://mail.example.com:10443"}},
		{"0.0.0.0:8443", []string{"https://mail.example.com", "https://mail.example.com:8443"}},
		{"", []string{"https://mail.example.com"}},
	}
	for _, testCase := range cases {
		if got := defaultPasskeyOrigins("mail.example.com", testCase.listen); !reflect.DeepEqual(got, testCase.want) {
			t.Errorf("%q: got %v, want %v", testCase.listen, got, testCase.want)
		}
	}
}
