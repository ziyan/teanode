package appupdate

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func checkerOf(t *testing.T, handler http.HandlerFunc) (*Checker, *time.Time) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	now := time.Date(2030, 3, 6, 15, 0, 0, 0, time.UTC)
	checker := New()
	checker.client = server.Client()
	checker.testFlightEndpoint = server.URL + "/testflight.json"
	checker.appStoreEndpoint = server.URL + "/lookup"
	checker.androidEndpoint = server.URL + "/android.json"
	checker.now = func() time.Time { return now }
	return checker, &now
}

func TestTheChannelsAreReadTheirOwnWay(t *testing.T) {
	checker, _ := checkerOf(t, func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/lookup" {
			_, _ = writer.Write([]byte(`{"resultCount":1,"results":[{"version":"1.2.0","trackViewUrl":"https://apps.apple.com/app/id1?uo=4"}]}`))
			return
		}
		_, _ = writer.Write([]byte(`{"version":"1.3.0","buildNumber":"42","installUrl":"itms-beta://beta.itunes.apple.com/v1/app/1"}`))
	})
	beta, err := checker.Latest(context.Background(), ChannelTestFlight)
	if err != nil || beta == nil || beta.Version != "1.3.0" || beta.BuildNumber != "42" || beta.UpdateChannel != ChannelTestFlight {
		t.Fatalf("%+v %v", beta, err)
	}
	store, err := checker.Latest(context.Background(), ChannelAppStore)
	if err != nil || store == nil || store.Version != "1.2.0" || store.BuildNumber != "" || store.InstallURL != "https://apps.apple.com/app/id1?uo=4" {
		t.Fatalf("%+v %v", store, err)
	}
}

// Before the first build reaches testers, and before the app is on the store,
// there is nothing to offer, which is not a failure.
func TestAChannelWithNothingYetAnswersNothing(t *testing.T) {
	checker, _ := checkerOf(t, func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/lookup" {
			_, _ = writer.Write([]byte(`{"resultCount":0,"results":[]}`))
			return
		}
		writer.WriteHeader(http.StatusNotFound)
	})
	for _, updateChannel := range []string{ChannelTestFlight, ChannelAppStore} {
		if update, err := checker.Latest(context.Background(), updateChannel); err != nil || update != nil {
			t.Fatalf("%s: %+v %v", updateChannel, update, err)
		}
	}
}

// The phone opens what this answers, so an answer leading anywhere but Apple
// is refused, and so is one that is not a version.
func TestAnAnswerLeadingElsewhereIsRefused(t *testing.T) {
	for name, answer := range map[string]string{
		"another host":   `{"version":"1.3.0","buildNumber":"42","installUrl":"https://apps.apple.com.example.net/app"}`,
		"another scheme": `{"version":"1.3.0","buildNumber":"42","installUrl":"javascript:alert(1)"}`,
		"plain http":     `{"version":"1.3.0","buildNumber":"42","installUrl":"http://apps.apple.com/app"}`,
		"no version":     `{"version":"","buildNumber":"42","installUrl":"https://apps.apple.com/app"}`,
		"words":          `{"version":"install me","buildNumber":"42","installUrl":"https://apps.apple.com/app"}`,
		"not JSON":       `<html>`,
	} {
		checker, _ := checkerOf(t, func(writer http.ResponseWriter, _ *http.Request) { _, _ = writer.Write([]byte(answer)) })
		if update, err := checker.Latest(context.Background(), ChannelTestFlight); err == nil {
			t.Errorf("%s: %+v", name, update)
		}
	}
}

func TestAnAnswerIsKeptAndOutlivesAFailure(t *testing.T) {
	asked, isFailing := 0, false
	checker, now := checkerOf(t, func(writer http.ResponseWriter, _ *http.Request) {
		asked++
		if isFailing {
			writer.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = writer.Write([]byte(`{"version":"1.3.0","buildNumber":"42","installUrl":"https://testflight.apple.com/v1/app/1"}`))
	})
	for range 3 {
		if _, err := checker.Latest(context.Background(), ChannelTestFlight); err != nil {
			t.Fatal(err)
		}
	}
	if asked != 1 {
		t.Fatalf("asked %d times", asked)
	}
	*now = now.Add(keptFor)
	isFailing = true
	update, err := checker.Latest(context.Background(), ChannelTestFlight)
	if err != nil || update == nil || update.BuildNumber != "42" || asked != 2 {
		t.Fatalf("%+v %v, asked %d times", update, err, asked)
	}
	// A failure waits its turn too: a server that cannot reach out does not
	// spend every phone's question finding that out again.
	if _, err := checker.Latest(context.Background(), ChannelTestFlight); err != nil || asked != 2 {
		t.Fatalf("%v, asked %d times", err, asked)
	}
}

func TestAnotherChannelIsRefused(t *testing.T) {
	if _, err := New().Latest(context.Background(), "sideloaded"); !errors.Is(err, ErrUnknownChannel) {
		t.Fatal(err)
	}
}

// The Android channel reads the file the distribution script publishes, whose
// build may be written as text or as the number it is.
func TestTheAndroidChannelReadsItsOwnFile(t *testing.T) {
	for name, build := range map[string]string{"text": `"42"`, "number": `42`} {
		checker, _ := checkerOf(t, func(writer http.ResponseWriter, request *http.Request) {
			if request.URL.Path != "/android.json" {
				t.Errorf("asked %s", request.URL.Path)
			}
			_, _ = writer.Write([]byte(`{"version":"1.4.0","build":` + build + `,"installUrl":"https://appdistribution.firebase.google.com/testerapps/example"}`))
		})
		update, err := checker.Latest(context.Background(), ChannelAndroid)
		if err != nil || update == nil || update.UpdateChannel != ChannelAndroid || update.Version != "1.4.0" || update.BuildNumber != "42" ||
			update.InstallURL != "https://appdistribution.firebase.google.com/testerapps/example" {
			t.Errorf("%s: %+v %v", name, update, err)
		}
	}
}

func TestTheAndroidChannelWithNothingYetAnswersNothing(t *testing.T) {
	checker, _ := checkerOf(t, func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusNotFound) })
	if update, err := checker.Latest(context.Background(), ChannelAndroid); err != nil || update != nil {
		t.Fatalf("%+v %v", update, err)
	}
}

// The phone opens what this answers, so an answer leading anywhere but the
// tester distribution or Google Play is refused, Apple's addresses included.
func TestAnAndroidAnswerLeadingElsewhereIsRefused(t *testing.T) {
	for name, installURL := range map[string]string{
		"another host":     "https://appdistribution.firebase.google.com.example.net/testerapps/example",
		"Apple":            "https://testflight.apple.com/v1/app/1",
		"Apple's own kind": "itms-beta://beta.itunes.apple.com/v1/app/1",
		"plain http":       "http://play.google.com/store/apps/details?id=com.example.app",
		"another scheme":   "intent://example#Intent;end",
	} {
		checker, _ := checkerOf(t, func(writer http.ResponseWriter, _ *http.Request) {
			_, _ = writer.Write([]byte(`{"version":"1.4.0","build":"42","installUrl":"` + installURL + `"}`))
		})
		if update, err := checker.Latest(context.Background(), ChannelAndroid); err == nil {
			t.Errorf("%s: %+v", name, update)
		}
	}
	// And Google's addresses do not lead an iPhone anywhere.
	checker, _ := checkerOf(t, func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{"version":"1.3.0","buildNumber":"42","installUrl":"https://play.google.com/store/apps/details?id=com.example.app"}`))
	})
	if update, err := checker.Latest(context.Background(), ChannelTestFlight); err == nil {
		t.Errorf("an iPhone was sent to Google Play: %+v", update)
	}
	for _, allowed := range []string{"https://play.google.com/store/apps/details?id=com.example.app", "https://appdistribution.firebase.dev/i/example"} {
		checker, _ := checkerOf(t, func(writer http.ResponseWriter, _ *http.Request) {
			_, _ = writer.Write([]byte(`{"version":"1.4.0","build":"42","installUrl":"` + allowed + `"}`))
		})
		if _, err := checker.Latest(context.Background(), ChannelAndroid); err != nil {
			t.Errorf("%s: %s", allowed, err)
		}
	}
}
