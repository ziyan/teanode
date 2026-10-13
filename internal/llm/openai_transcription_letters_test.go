package llm

import "testing"

func TestJoinSpelledLettersWritesAcronymsAsWords(test *testing.T) {
	for spelled, joined := range map[string]string{
		"reporting it to the P_L_C_ today": "reporting it to the PLC today",
		"the M_H_E_ bridges":               "the MHE bridges",
		"version 2_0_ of it":               "version 20 of it",
		"a snake_case name stays":          "a snake_case name stays",
		"no letters here":                  "no letters here",
	} {
		if got := joinSpelledLetters(spelled); got != joined {
			test.Errorf("%q became %q, want %q", spelled, got, joined)
		}
	}
}
