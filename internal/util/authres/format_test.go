package authres_test

import (
	"testing"

	"github.com/ziyan/teanode/internal/util/authres"
)

func TestFormat(t *testing.T) {
	t.Parallel()
	for _, test := range msgauthTests {
		test := test
		t.Run(test.value, func(t *testing.T) {
			t.Parallel()
			v := authres.Format(test.identifier, test.results)
			if v != test.value {
				t.Errorf("Expected formatted header field to be \n%q\n but got \n%q", test.value, v)
			}
		})
	}
}
