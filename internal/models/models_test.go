package models_test

import (
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

// The console's name is not an account's to take, whatever the path that
// names an account: the dashboard, the command line, or a sign-in through
// an identity provider all validate here.
func TestTheConsolesNameIsReserved(t *testing.T) {
	t.Parallel()

	user := &models.User{Username: models.LocalUsername}
	if err := user.Validate(); err == nil {
		t.Errorf("%q was accepted as an account name", models.LocalUsername)
	}
	user.Username = "(Local)"
	if err := user.Validate(); err == nil {
		t.Errorf("%q was accepted as an account name", user.Username)
	}
	user.Username = "local"
	if err := user.Validate(); err != nil {
		t.Errorf("an ordinary name was refused: %s", err)
	}
}
