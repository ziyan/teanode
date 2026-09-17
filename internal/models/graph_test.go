package models

import "testing"

// A page under people is the person by their username, by their name, by
// its slug, or by any name their own page is also called: an account named
// "Ziyan" alone still owns people/ziyan-zhou once self is called that.
func TestIsThePersonKnowsEveryNameTheyGoBy(t *testing.T) {
	owner := &User{Username: "ziyan", Name: "Ziyan"}
	self := &AgentNode{Path: PathSelf, Name: "Who they are", Aliases: []string{"Ziyan Zhou", "zhou@example.net"}}
	for _, path := range []string{"people/ziyan", "people/ziyan-zhou", "people/Ziyan"} {
		if !IsThePerson(path, owner, self) {
			t.Errorf("%s should be the person", path)
		}
	}
	for _, path := range []string{"people/alice-chen", "self", "projects/ziyan-zhou", "people/zhou-example-net"} {
		if IsThePerson(path, owner, self) {
			t.Errorf("%s should not be the person", path)
		}
	}
	if IsThePerson("people/ziyan-zhou", owner, nil) {
		t.Errorf("without the self page, only the account's own names count")
	}
}
