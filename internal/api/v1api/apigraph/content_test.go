package apigraph

import (
	"reflect"
	"testing"
)

// The image proxy fetches only what the message itself links to. This is the
// list it checks against: the sources the sanitizer held back, exactly as the
// dashboard sends them back (decoded, and not trimmed or rewritten).
func TestRemoteImageAddressesAreThoseTheMessageLinksTo(t *testing.T) {
	t.Parallel()

	headers := []string{"Content-Type: text/html; charset=utf-8\r\n"}
	body := []byte(`<p>hello</p>` +
		`<img src="https://images.example.test/a.gif?x=1&amp;y=2">` +
		`<img src="  http://images.example.test/b.png ">` +
		`<img src="cid:inline-one">` +
		`<img src="data:image/gif;base64,R0lGODlhAQABAAAAACw=">`)

	got, err := RemoteImageAddresses(headers, body)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"https://images.example.test/a.gif?x=1&y=2", "http://images.example.test/b.png"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("RemoteImageAddresses = %q, want %q", got, want)
	}
}

func TestRemoteImageAddressesOfAPlainTextMessageAreNone(t *testing.T) {
	t.Parallel()

	got, err := RemoteImageAddresses([]string{"Content-Type: text/plain\r\n"}, []byte("see https://images.example.test/a.gif\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("a plain text message names pictures: %q", got)
	}
}
