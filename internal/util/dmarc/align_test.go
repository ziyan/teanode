package dmarc

import "testing"

func TestAlignmentComparesOrganizationalDomainsWhenRelaxed(t *testing.T) {
	testCases := []struct {
		name                string
		authenticatedDomain string
		fromDomain          string
		mode                AlignmentMode
		isAligned           bool
	}{
		{"the same name", "example.com", "example.com", AlignmentStrict, true},
		{"case and a trailing dot do not matter", "Example.COM.", "example.com", AlignmentStrict, true},
		{"a subdomain is not strict", "mail.example.com", "example.com", AlignmentStrict, false},
		{"a subdomain signing for its parent", "mail.example.com", "example.com", AlignmentRelaxed, true},
		{"a parent signing for its subdomain", "example.com", "alerts.example.com", AlignmentRelaxed, true},
		{"a sibling signing for a deeper sibling", "mail.example.com", "news.letters.example.com", AlignmentRelaxed, true},
		{"another organization", "example.net", "example.com", AlignmentRelaxed, false},
		{"a name ending the same way is not a subdomain", "badexample.test", "example.test", AlignmentRelaxed, false},
		{"two registrations under one suffix", "first.test", "second.test", AlignmentRelaxed, false},
		{"siblings under an unlisted suffix", "mail.example.test", "news.example.test", AlignmentRelaxed, true},
		{"an empty domain", "", "example.com", AlignmentRelaxed, false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := IsAligned(testCase.authenticatedDomain, testCase.fromDomain, testCase.mode); got != testCase.isAligned {
				t.Fatalf("IsAligned(%q, %q, %q) = %v, want %v", testCase.authenticatedDomain, testCase.fromDomain, testCase.mode, got, testCase.isAligned)
			}
		})
	}
}
