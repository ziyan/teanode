package mailparse_test

import (
	"testing"

	"github.com/ziyan/teanode/internal/util/mailparse"
)

// A message body carries its charset once, in the Content-Type, and nothing
// about the bytes themselves says what they are. Handed over as they arrived,
// a Japanese newsletter in ISO-2022-JP is a page of escape sequences and a
// French one in Windows-1252 has a replacement character where every accent
// was — which is what the dashboard showed before this decoded them.
func TestDecodeCharsetReadsWhatThePartSaysItIs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content []byte
		label   string
		want    string
	}{
		{
			name: "iso-2022-jp, which is what Japanese mail is written in",
			// ESC $ B switches to JIS X 0208, ESC ( B switches back to ASCII.
			content: []byte("\x1b$B$3$s$K$A$O\x1b(B"),
			label:   "ISO-2022-JP",
			want:    "こんにちは",
		},
		{
			name:    "shift_jis",
			content: []byte{0x82, 0xb1, 0x82, 0xf1, 0x82, 0xc9, 0x82, 0xbf, 0x82, 0xcd},
			label:   "Shift_JIS",
			want:    "こんにちは",
		},
		{
			name:    "windows-1252, where a great deal of European mail still is",
			content: []byte{'c', 'a', 'f', 0xe9},
			label:   "windows-1252",
			want:    "café",
		},
		{
			name:    "gb2312, for simplified Chinese",
			content: []byte{0xc4, 0xe3, 0xba, 0xc3},
			label:   "gb2312",
			want:    "你好",
		},
		{
			name:    "utf-8 is left alone",
			content: []byte("こんにちは"),
			label:   "utf-8",
			want:    "こんにちは",
		},
		{
			name:    "no charset at all is left alone, which RFC 2045 calls us-ascii",
			content: []byte("plain"),
			label:   "",
			want:    "plain",
		},
		{
			name:    "a charset nobody has heard of leaves the bytes as they arrived",
			content: []byte("whatever this is"),
			label:   "x-not-a-charset",
			want:    "whatever this is",
		},
		{
			name:    "the label is matched without regard to case or spacing",
			content: []byte{'c', 'a', 'f', 0xe9},
			label:   " Windows-1252 ",
			want:    "café",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := string(mailparse.DecodeCharset(test.content, test.label))
			if got != test.want {
				t.Errorf("DecodeCharset(%q) = %q, want %q", test.label, got, test.want)
			}
		})
	}
}
