package models

import "testing"

func TestPhoneModelNameReadsAnIdentifierAsTheNameItIsSoldUnder(test *testing.T) {
	for identifier, want := range map[string]string{
		"iPhone12,1": "iPhone 11",
		"iPhone17,3": "iPhone 16",
		"iPhone18,2": "iPhone 17 Pro Max",
		"iPhone18,4": "iPhone Air",
		"iPad16,3":   "iPad Pro 11-inch (M4)",
		"iPad16,4":   "iPad Pro 11-inch (M4)",
		"x86_64":     "Simulator",
		"arm64":      "Simulator",
	} {
		if got := PhoneModelName(identifier); got != want {
			test.Errorf("PhoneModelName(%q) = %q, want %q", identifier, got, want)
		}
	}
}

func TestPhoneModelNameLeavesAnUnknownIdentifierAlone(test *testing.T) {
	for _, identifier := range []string{"iPhone99,9", "iPhone", "", "Watch7,1"} {
		if got := PhoneModelName(identifier); got != identifier {
			test.Errorf("PhoneModelName(%q) = %q, want it unchanged", identifier, got)
		}
	}
}

func TestEveryPhoneModelNameIsFilledIn(test *testing.T) {
	for identifier, name := range phoneModelNames {
		if name == "" {
			test.Errorf("%s has no name", identifier)
		}
	}
}
