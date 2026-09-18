package agent

import (
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

// The harder ones from the real graph: a sentence that looks like it adds
// something, where everything it adds is already in the page's own name.
func TestAFactThatOnlyUnpacksThePageName(t *testing.T) {
	for _, row := range []struct {
		page *models.AgentNode
		text string
	}{
		{&models.AgentNode{Path: "projects/4f-paltac-depalletize-2021-demo", Name: "4F Paltac Depalletize 2021 Demo"},
			"4f_paltac-depalletize-2021-demo is a project or work channel concerning Paltac depalletizing and the 2021 demo."},
		{&models.AgentNode{Path: "projects/aeon-kawaguchi-agvarea-onsite", Name: "Aeon Kawaguchi AGV Area Onsite"},
			"aeon_kawaguchi_agvarea_onsite is a project or work channel concerning onsite work for Aeon Kawaguchi's AGV area."},
		{&models.AgentNode{Path: "topics/language-models-and-ai-at-mujin", Name: "Language models and AI at Mujin"},
			"There is a work topic or channel about language models and AI at Mujin."},
		{&models.AgentNode{Path: "projects/fa-projects", Name: "fa-projects"},
			"The fa-projects project exists and had activity in August 2026."},
		// The dashes a model writes between the parts of a name are not
		// ASCII, and a tokenizer that keeps them makes one token of two
		// words and matches nothing.
		{&models.AgentNode{Path: "projects/240159-ftwo-dln-packmaster-yaskawa-nordic-pepsi", Name: "240159-ftwo--dln-packmaster-yaskawa-nordic-pepsi"},
			"Work occurred on the 240159 FTWO\u2013DLN Packmaster\u2013Yaskawa\u2013Nordic\u2013Pepsi project in June 2026."},
		{&models.AgentNode{Path: "projects/ci-medical-dpplpp", Name: "ci_medical_dpplpp"},
			"Activity on ci_medical_dpplpp was recorded in June 2026."},
		{&models.AgentNode{Path: "topics/language-models-and-ai-at-mujin", Name: "Language models and AI at Mujin"},
			"Discussion occurred about language models and AI at Mujin in July 2026."},
	} {
		if saysSomethingNew(row.text, row.page, nil) {
			t.Errorf("everything in %q is already in the page's name", row.text)
		}
	}

	// And the edge of it: "X is a project Ziyan worked on in September
	// 2026" is thin, but it does say who and when, so it is kept. The
	// rule refuses what says nothing, not what says little.
	thin := &models.AgentNode{Path: "projects/230448-toyota-boshoku", Name: "230448 Toyota Boshoku"}
	if !saysSomethingNew("230448 Toyota Boshoku is a project Ziyan worked on in September 2026.", thin, nil) {
		t.Errorf("who worked on it and when is thin, and is not nothing")
	}
	// But on Ziyan's own graph it says nothing at all: every page here is
	// about their life, so "Ziyan worked on it" is "it is here".
	theirs := &models.User{Name: "Ziyan Zhou", Username: "ziyan"}
	if saysSomethingNew("230448 Toyota Boshoku is a project Ziyan worked on in September 2026.", thin, theirs) {
		t.Errorf("the owner's own name says nothing on the owner's own graph")
	}
	if saysSomethingNew("Ziyan works on the mujin project.",
		&models.AgentNode{Path: "projects/mujin", Name: "mujin"}, theirs) {
		t.Errorf("and this is the commonest shape of it")
	}
	// Somebody else's name still counts.
	if !saysSomethingNew("Alice Chen works on the mujin project.",
		&models.AgentNode{Path: "projects/mujin", Name: "mujin"}, theirs) {
		t.Errorf("who else works on it is the whole point of the page")
	}
	// And a sentence about something that happened keeps the word for
	// what happened, so dropping the verbs of mere activity is safe.
	if !saysSomethingNew("Discussion of the gripper deadlock in July 2026.", thin, nil) {
		t.Errorf("what was discussed survives the verb for discussing it")
	}
}
