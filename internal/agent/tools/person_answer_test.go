package tools

import "testing"

func TestAnswersSkillShareCall(t *testing.T) {
	for commandLine, want := range map[string]bool{
		"":                         false,
		"ls -la":                   false,
		"teanode agent skill list": false,
		"teanode agent skill calls abc --waiting":                          false,
		"teanode agent skill approve 01abc":                                true,
		"teanode agent skill decline 01abc --note 'not now'":               true,
		"TEANODE agent SKILL Approve 01abc":                                true,
		"teanode --profile home agent skill approve 01abc":                 true,
		"teanode agent skill --json approve 01abc":                         true,
		"bash -c 'teanode agent skill approve 01abc'":                      true,
		`sh -c "cd ~ && /usr/local/bin/teanode agent skill approve 01abc"`: true,
		"echo hi; teanode agent skill\napprove 01abc":                      true,
		"(teanode agent skill approve 01abc)":                              true,
		"$(teanode agent skill approve 01abc)":                             true,
		"teanode agent memory approve":                                     false,
		"teanode agent skill share --approval-required":                    false,
	} {
		if got := AnswersSkillShareCall(commandLine); got != want {
			t.Errorf("AnswersSkillShareCall(%q) = %v, want %v", commandLine, got, want)
		}
	}
}
