package apisso

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ziyan/teanode/internal/web"
)

// refusingAuthenticator is an authenticator whose sign-in limiter has run out.
type refusingAuthenticator struct {
	web.Authenticator
	attempts int
}

func (self *refusingAuthenticator) AllowLoginAttempt(*http.Request) bool {
	self.attempts++
	return false
}

// Both paths are open to anybody and reach out to the identity provider, so
// they count against the sign-in limiter like every other way of signing in.
func TestStartAndCallbackAreLimited(t *testing.T) {
	t.Parallel()
	for name, handler := range map[string]func(*component) http.HandlerFunc{
		"start":    func(self *component) http.HandlerFunc { return self.start },
		"callback": func(self *component) http.HandlerFunc { return self.callback },
	} {
		authenticator := &refusingAuthenticator{}
		response := httptest.NewRecorder()
		handler(&component{authenticator: authenticator})(response, httptest.NewRequest(http.MethodGet, "/api/v1/sso/example/"+name, nil))

		if response.Code != http.StatusTooManyRequests {
			t.Errorf("%s answered %d once the limiter was spent, expected %d", name, response.Code, http.StatusTooManyRequests)
		}
		if authenticator.attempts != 1 {
			t.Errorf("%s counted %d attempts, expected 1", name, authenticator.attempts)
		}
	}
}
