package apigraph

import "testing"

// Who may change what about an account.
//
// /settings/profile is the page about you: what to call you, what you sign in
// with, where notifications go. Changing any of it used to need user:manage —
// the permission to administer everybody — so a person could not correct
// their own name unless they were an administrator.
//
// What stays administrative is the part that is not about who you are but
// about what you may do: whether the account may sign in at all, and which
// groups it is in. Nobody grants themselves those, so asking for them turns
// the request back into one that needs the permission.
func TestSelfService(t *testing.T) {
	t.Parallel()

	yes := true
	groups := []string{"01group"}
	name := "Ann"

	tests := []struct {
		name      string
		own       bool
		arguments UpdateUserArguments
		want      bool
	}{
		{
			name:      "your own name",
			own:       true,
			arguments: UpdateUserArguments{Name: &name},
			want:      true,
		},
		{
			name:      "your own account, changing nothing in particular",
			own:       true,
			arguments: UpdateUserArguments{},
			want:      true,
		},
		{
			name:      "somebody else's name",
			own:       false,
			arguments: UpdateUserArguments{Name: &name},
		},
		{
			// Disabling yourself is still an administrative act, and allowing
			// it here would be a way to lock an account out without the
			// permission that governs accounts.
			name:      "disabling your own account",
			own:       true,
			arguments: UpdateUserArguments{Disabled: &yes},
		},
		{
			// The one that matters: this is how somebody would give
			// themselves every permission there is.
			name:      "putting yourself in a group",
			own:       true,
			arguments: UpdateUserArguments{GroupIDs: &groups},
		},
		{
			name:      "your own name, and a group with it",
			own:       true,
			arguments: UpdateUserArguments{Name: &name, GroupIDs: &groups},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := selfService(test.own, test.arguments); got != test.want {
				t.Errorf("selfService = %v, want %v", got, test.want)
			}
		})
	}
}
