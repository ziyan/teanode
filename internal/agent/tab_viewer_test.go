package agent

import (
	"encoding/json"
	"sync"
	"testing"
	"time"
)

// recordingBrowser is the extension's side of the socket, keeping every
// message the server sends it.
type recordingBrowser struct {
	mutex    sync.Mutex
	messages []map[string]any
}

func (self *recordingBrowser) Send(message []byte) error {
	var decoded map[string]any
	_ = json.Unmarshal(message, &decoded)
	self.mutex.Lock()
	self.messages = append(self.messages, decoded)
	self.mutex.Unlock()
	return nil
}

// sentOfType is the messages of one type, in order.
func (self *recordingBrowser) sentOfType(messageType string) []map[string]any {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	var found []map[string]any
	for _, message := range self.messages {
		if message["type"] == messageType {
			found = append(found, message)
		}
	}
	return found
}

// recordingViewer is the dashboard page, keeping what it is sent.
type recordingViewer struct {
	mutex  sync.Mutex
	states []tabViewerState
	frames []tabFrameMessage
}

func (self *recordingViewer) Send(message []byte) error {
	var state tabViewerState
	_ = json.Unmarshal(message, &state)
	self.mutex.Lock()
	self.states = append(self.states, state)
	self.mutex.Unlock()
	return nil
}

func (self *recordingViewer) SendFrame(message []byte) {
	var frame tabFrameMessage
	_ = json.Unmarshal(message, &frame)
	self.mutex.Lock()
	self.frames = append(self.frames, frame)
	self.mutex.Unlock()
}

func (self *recordingViewer) lastState() tabViewerState {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if len(self.states) == 0 {
		return tabViewerState{}
	}
	return self.states[len(self.states)-1]
}

func (self *recordingViewer) frameCount() int {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return len(self.frames)
}

// eventually waits a second for something the relay does on its own time.
func eventually(t *testing.T, what string, isDone func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !isDone() {
		if time.Now().After(deadline) {
			t.Fatalf("%s did not happen", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// connectedBrowser attaches a browser that can stream, with two tabs.
func connectedBrowser(worker *Agent) *recordingBrowser {
	browser := &recordingBrowser{}
	worker.ConnectBrowser("a1", browser, "Shop", "https://shop.example/", true)
	worker.SetBrowserCanCast("a1", browser, true)
	worker.UpdateTabList("a1", []TabListing{
		{Tab: "11", Title: "Shop", URL: "https://shop.example/", IsOwn: true, IsCurrent: true},
		{Tab: "12", Title: "Bank", URL: "https://bank.example/"},
	})
	return browser
}

func TestTabViewerIsToldTheTabsAndWhoHoldsThem(t *testing.T) {
	worker := &Agent{}
	connectedBrowser(worker)
	viewer := &recordingViewer{}
	worker.AddTabViewer("a1", viewer)
	state := viewer.lastState()
	if !state.IsConnected || !state.CanCast || len(state.Tabs) != 2 || state.Controller != TabControllerAgent || state.Watching != "" {
		t.Fatalf("state %+v", state)
	}
	worker.WatchTab("a1", viewer, "12")
	if viewer.lastState().Watching != "12" {
		t.Fatalf("watching %q", viewer.lastState().Watching)
	}
	// A tab that is not listed is not watched.
	worker.WatchTab("a1", viewer, "99")
	if viewer.lastState().Watching != "12" {
		t.Fatalf("an unlisted tab was watched: %q", viewer.lastState().Watching)
	}
}

func TestTabViewerStreamsOnlyWhatIsWatched(t *testing.T) {
	worker := &Agent{}
	browser := connectedBrowser(worker)
	first, second := &recordingViewer{}, &recordingViewer{}
	worker.AddTabViewer("a1", first)
	worker.AddTabViewer("a1", second)
	worker.WatchTab("a1", first, "11")
	worker.WatchTab("a1", second, "11")
	casts := browser.sentOfType("cast")
	if len(casts) != 1 || casts[0]["tab"] != float64(11) || casts[0]["on"] != true {
		t.Fatalf("two viewers of one tab asked for one stream: %v", casts)
	}
	worker.TabFrame("a1", browser, "11", "picture", 1280, 800)
	worker.TabFrame("a1", browser, "12", "other", 1280, 800)
	if first.frameCount() != 1 || second.frameCount() != 1 {
		t.Fatalf("frames %d and %d", first.frameCount(), second.frameCount())
	}
	if frame := first.frames[0]; frame.Data != "picture" || frame.Width != 1280 || frame.Tab != "11" {
		t.Fatalf("frame %+v", frame)
	}
	worker.RemoveTabViewer("a1", first)
	if casts := browser.sentOfType("cast"); len(casts) != 1 {
		t.Fatalf("a stream was stopped while somebody still watched: %v", casts)
	}
	worker.RemoveTabViewer("a1", second)
	casts = browser.sentOfType("cast")
	if len(casts) != 2 || casts[1]["on"] != false {
		t.Fatalf("the last viewer leaving should stop the stream: %v", casts)
	}
}

func TestTabFrameFromAReplacedConnectionReachesNobody(t *testing.T) {
	worker := &Agent{}
	old := connectedBrowser(worker)
	viewer := &recordingViewer{}
	worker.AddTabViewer("a1", viewer)
	worker.WatchTab("a1", viewer, "11")
	replacement := &recordingBrowser{}
	worker.ConnectBrowser("a1", replacement, "Shop", "https://shop.example/", true)
	worker.SetBrowserCanCast("a1", replacement, true)
	worker.TabFrame("a1", old, "11", "late", 1280, 800)
	if viewer.frameCount() != 0 {
		t.Fatal("a picture from the replaced connection was shown")
	}
	// The new connection is asked for the stream the viewer still wants.
	if casts := replacement.sentOfType("cast"); len(casts) != 1 || casts[0]["on"] != true {
		t.Fatalf("the new connection was not asked to stream: %v", casts)
	}
}

func TestTabInputPassesOnlyWhileThePersonHoldsTheTab(t *testing.T) {
	worker := &Agent{}
	browser := connectedBrowser(worker)
	viewer := &recordingViewer{}
	worker.AddTabViewer("a1", viewer)
	worker.WatchTab("a1", viewer, "11")
	click := TabInputMessage{Type: "pointer", Event: "down", X: 10, Y: 20, Button: "left", ClickCount: 1}
	worker.TabInput("a1", viewer, click)
	if inputs := browser.sentOfType("pointer"); len(inputs) != 0 {
		t.Fatalf("input passed while the agent held the tab: %v", inputs)
	}
	worker.TakeOverTab("a1")
	if !worker.IsTabTakenOver("a1") || !worker.tabFor("a1").IsTakenOver() || viewer.lastState().Controller != TabControllerPerson {
		t.Fatal("taking over did not give the person the tab")
	}
	worker.TabInput("a1", viewer, click)
	worker.TabInput("a1", viewer, TabInputMessage{Type: "evaluate", Text: "anything"})
	inputs := browser.sentOfType("pointer")
	if len(inputs) != 1 || inputs[0]["tab"] != float64(11) || inputs[0]["x"] != float64(10) {
		t.Fatalf("inputs %v", inputs)
	}
	if others := browser.sentOfType("evaluate"); len(others) != 0 {
		t.Fatalf("a kind of input that is not one passed: %v", others)
	}
	if !worker.HandBackTab("a1") || worker.IsTabTakenOver("a1") {
		t.Fatal("handing back did not give the agent the tab")
	}
	if worker.HandBackTab("a1") {
		t.Fatal("handing back a tab nobody took over said it was held")
	}
}

func TestTakenOverTabGoesBackToTheAgentWhenNobodyWatches(t *testing.T) {
	worker := &Agent{}
	connectedBrowser(worker)
	viewer := &recordingViewer{}
	worker.AddTabViewer("a1", viewer)
	worker.roomFor("a1").lapseAfter = 20 * time.Millisecond
	worker.TakeOverTab("a1")
	worker.RemoveTabViewer("a1", viewer)
	eventually(t, "the tab going back to the agent", func() bool { return !worker.IsTabTakenOver("a1") })

	// A viewer coming back in time keeps it.
	again := &recordingViewer{}
	worker.AddTabViewer("a1", again)
	worker.TakeOverTab("a1")
	worker.RemoveTabViewer("a1", again)
	worker.AddTabViewer("a1", again)
	time.Sleep(60 * time.Millisecond)
	if !worker.IsTabTakenOver("a1") {
		t.Fatal("the tab lapsed although the person came back to it")
	}
}

func TestTabListDropsAViewOfATabThatWent(t *testing.T) {
	worker := &Agent{}
	browser := connectedBrowser(worker)
	viewer := &recordingViewer{}
	worker.AddTabViewer("a1", viewer)
	worker.WatchTab("a1", viewer, "12")
	worker.UpdateTabList("a1", []TabListing{{Tab: "11", Title: "Shop", IsOwn: true, IsCurrent: true}})
	if viewer.lastState().Watching != "" {
		t.Fatalf("still watching %q", viewer.lastState().Watching)
	}
	if casts := browser.sentOfType("cast"); len(casts) != 2 || casts[1]["on"] != false {
		t.Fatalf("the gone tab's stream was not stopped: %v", casts)
	}
}

func TestTabViewerIsToldWhenTheBrowserGoes(t *testing.T) {
	worker := &Agent{}
	browser := connectedBrowser(worker)
	viewer := &recordingViewer{}
	worker.AddTabViewer("a1", viewer)
	worker.DetachTab("a1", browser)
	eventually(t, "the viewer being told", func() bool { return !viewer.lastState().IsConnected })
	if tabs := viewer.lastState().Tabs; len(tabs) != 0 {
		t.Fatalf("a gone browser still listed %v", tabs)
	}
}
