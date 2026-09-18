package reading

import (
	"strings"
	"testing"
)

// Documents nothing here can read yet are counted apart and said apart.
// They are not waiting -- a night could do nothing with a picture today
// -- and calling them read would be a lie: a source of fifty thousand
// screenshots would say it was all read and nothing would have been.
func TestUnreadableFilesAreSaidApartFromTheReading(t *testing.T) {
	progress := &Progress{Read: 40, Waiting: 10, Unreadable: 1020}
	line := progress.Describe()
	if !strings.Contains(line, "40 of 50 documents read, 10 waiting") {
		t.Fatalf("the reading is no longer said: %q", line)
	}
	if !strings.Contains(line, "1,020 files nothing here can read yet") {
		t.Fatalf("the files are not said in words: %q", line)
	}

	// Everything read, and the pictures still waiting for something that
	// can open them.
	done := (&Progress{Read: 40, Unreadable: 1}).Describe()
	if !strings.Contains(done, "all 40 documents read") || !strings.Contains(done, "1 file nothing here can read yet") {
		t.Fatalf("read, with one file left over: %q", done)
	}

	// Nothing but pictures: "nothing has been indexed yet" would be
	// wrong, because something was.
	only := (&Progress{Unreadable: 7}).Describe()
	if only != "7 files nothing here can read yet" {
		t.Fatalf("a source of nothing but files: %q", only)
	}
}

// The sentences that were there before are the sentences that are there
// now, for every source that has no such files.
func TestTheReadingStillReadsTheWayItDid(t *testing.T) {
	for _, want := range []struct {
		progress Progress
		line     string
	}{
		{Progress{}, "nothing has been indexed yet"},
		{Progress{Read: 12}, "all 12 documents read"},
		{Progress{Read: 12, Waiting: 4}, "12 of 16 documents read, 4 waiting"},
		{
			Progress{Read: 12, Waiting: 4, PerHour: 2, HoursLeft: 2},
			"12 of 16 documents read, 4 waiting; about 2 hours left at 2 an hour, if it dreamed without pause",
		},
	} {
		if line := want.progress.Describe(); line != want.line {
			t.Fatalf("%q, not %q", line, want.line)
		}
	}
}

// The three things that can become of a file are told apart, and each in
// its own words.
//
// One number for all of them would be the lie the count was split up to
// avoid: a person shown "1,020 files" cannot tell whether the agent has
// yet to look at them, has looked and decided against them, or has read
// them. Each says which it is.
func TestTheThreeThingsThatBecomeOfAFileAreSaidApart(t *testing.T) {
	progress := &Progress{Read: 40, Waiting: 10, Unreadable: 1020, Declined: 300, Described: 47}
	line := progress.Describe()
	for _, want := range []string{
		"40 of 50 documents read, 10 waiting",
		"1,020 files nothing here can read yet",
		"300 files it decided against opening",
		"47 files it opened and read",
	} {
		if !strings.Contains(line, want) {
			t.Fatalf("the line lacks %q: %q", want, line)
		}
	}

	// One of each, in the singular.
	one := (&Progress{Read: 1, Unreadable: 1, Declined: 1, Described: 1}).Describe()
	for _, want := range []string{
		"1 file nothing here can read yet",
		"1 file it decided against opening",
		"1 file it opened and read",
	} {
		if !strings.Contains(one, want) {
			t.Fatalf("the line lacks %q: %q", want, one)
		}
	}

	// A source of nothing but files the agent decided against: something
	// was indexed, so "nothing has been indexed yet" would be wrong.
	only := (&Progress{Declined: 7}).Describe()
	if only != "7 files it decided against opening" {
		t.Fatalf("a source of nothing but declined files: %q", only)
	}
}
