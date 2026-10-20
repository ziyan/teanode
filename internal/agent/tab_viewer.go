package agent

import (
	"encoding/json"
	"strconv"
	"sync"
	"time"
)

// Watching the person's browser, and taking it over. The dashboard's
// browser page opens a socket of its own and becomes a viewer: the tab it
// watches is streamed to it as pictures, and while the person has taken
// the tab over, what they click and type there is passed to the tab. The
// agent can still read a tab the person holds, but not act in it, until
// they hand it back. Watching alone changes nothing.
//
// What is watched and who holds it belong to the person, not to the
// extension's connection, so a browser that reconnects keeps its viewers
// and its controller; the room is kept per agent beside the tabs.

// TabViewer is one live view of the person's browser: the dashboard page.
type TabViewer interface {
	// Send writes a message the viewer must not miss: what there is to
	// watch, who holds it, a refusal.
	Send(message []byte) error
	// SendFrame writes a picture of the tab. A picture the viewer has not
	// been sent yet is replaced by a newer one rather than queued behind
	// it, so a slow viewer sees the tab as it is now, not as it was.
	SendFrame(message []byte)
}

// TabListing is one tab the agent may act in, as the extension reports it.
type TabListing struct {
	Tab       string `json:"tab"`
	Title     string `json:"title"`
	URL       string `json:"url"`
	IsOwn     bool   `json:"isOwn"`
	IsCurrent bool   `json:"isCurrent"`
}

// The holders of a tab: the agent, or the person who took it over.
const (
	TabControllerAgent  = "agent"
	TabControllerPerson = "person"
)

// takeOverLapse is how long a tab the person took over stays theirs after
// their last view of it went away without handing it back: long enough to
// ride out a phone reconnecting, short enough that one which locked does
// not leave the agent refused for good.
const takeOverLapse = 30 * time.Second

// Casting is what a picture of a tab is asked for at: as big as a laptop
// screen, and a JPEG quality that keeps text readable at half the bytes.
const (
	castMaximumSide = 1280
	castQuality     = 60
)

// tabRoom is one person's viewers and who holds their tab.
type tabRoom struct {
	// tellMutex keeps the viewers' state messages in the order they were
	// made, and castMutex the extension's cast requests: each is held
	// from reading what to say to having said it, which never blocks.
	tellMutex sync.Mutex
	castMutex sync.Mutex

	mutex sync.Mutex
	// viewers are the open views, each with the tab it watches.
	viewers map[TabViewer]string
	// tabs is the extension's last word on what there is to watch.
	tabs []TabListing
	// controller is TabControllerAgent or TabControllerPerson.
	controller string
	// lapse turns a tab back to the agent once nobody watches it.
	lapse *time.Timer
	// lapseAfter is takeOverLapse, shortened by tests.
	lapseAfter time.Duration
	// casting are the tabs the extension was asked to stream.
	casting map[string]bool
	// strayStopAt is when the extension was last told to stop a stream
	// nobody asked for, by tab, so a burst of its pictures asks once.
	strayStopAt map[string]time.Time
}

func newTabRoom() *tabRoom {
	return &tabRoom{viewers: map[TabViewer]string{}, controller: TabControllerAgent, lapseAfter: takeOverLapse, casting: map[string]bool{}, strayStopAt: map[string]time.Time{}}
}

// roomFor is the agent's room, made on first use.
func (self *Agent) roomFor(agentId string) *tabRoom {
	self.tabsMutex.Lock()
	defer self.tabsMutex.Unlock()
	if self.tabRooms == nil {
		self.tabRooms = map[string]*tabRoom{}
	}
	room := self.tabRooms[agentId]
	if room == nil {
		room = newTabRoom()
		self.tabRooms[agentId] = room
	}
	return room
}

// existingRoom is the agent's room, or nil when nobody ever watched.
func (self *Agent) existingRoom(agentId string) *tabRoom {
	self.tabsMutex.Lock()
	defer self.tabsMutex.Unlock()
	return self.tabRooms[agentId]
}

// castingTab is the attached browser when it can stream a tab, or nil.
func (self *Agent) castingTab(agentId string) *attachedTab {
	tab := self.tabFor(agentId)
	if tab == nil || !tab.canCast.Load() {
		return nil
	}
	return tab
}

// tabViewerState is what a viewer is told of how things stand.
type tabViewerState struct {
	Type        string       `json:"type"`
	IsConnected bool         `json:"isConnected"`
	CanCast     bool         `json:"canCast"`
	Tabs        []TabListing `json:"tabs"`
	Watching    string       `json:"watching,omitempty"`
	Controller  string       `json:"controller"`
}

// tellViewers sends every viewer how things stand. Called without the
// room's lock; a viewer's Send only queues, so this never waits on one.
func (self *Agent) tellViewers(agentId string) {
	room := self.existingRoom(agentId)
	if room == nil {
		return
	}
	room.tellMutex.Lock()
	defer room.tellMutex.Unlock()
	tab := self.tabFor(agentId)
	room.mutex.Lock()
	tabs := append([]TabListing{}, room.tabs...)
	if tab == nil {
		tabs = nil
	}
	controller := room.controller
	viewers := make(map[TabViewer]string, len(room.viewers))
	for viewer, watching := range room.viewers {
		viewers[viewer] = watching
	}
	room.mutex.Unlock()
	for viewer, watching := range viewers {
		state := tabViewerState{
			Type: "state", IsConnected: tab != nil, CanCast: tab != nil && tab.canCast.Load(),
			Tabs: tabs, Watching: watching, Controller: controller,
		}
		if state.Tabs == nil {
			state.Tabs = []TabListing{}
		}
		message, _ := json.Marshal(state)
		_ = viewer.Send(message)
	}
}

// AddTabViewer opens a view of the person's browser, watching nothing until
// the viewer says which tab.
func (self *Agent) AddTabViewer(agentId string, viewer TabViewer) {
	room := self.roomFor(agentId)
	room.mutex.Lock()
	room.viewers[viewer] = ""
	if room.lapse != nil {
		room.lapse.Stop()
		room.lapse = nil
	}
	room.mutex.Unlock()
	self.tellViewers(agentId)
}

// RemoveTabViewer closes a view. The last view of a tab stops its stream,
// and a tab the person holds goes back to the agent a while after the last
// view of all closed.
func (self *Agent) RemoveTabViewer(agentId string, viewer TabViewer) {
	room := self.existingRoom(agentId)
	if room == nil {
		return
	}
	room.mutex.Lock()
	delete(room.viewers, viewer)
	if len(room.viewers) == 0 && room.controller == TabControllerPerson && room.lapse == nil {
		var lapse *time.Timer
		lapse = time.AfterFunc(room.lapseAfter, func() {
			room.mutex.Lock()
			// A timer stopped while this waited for the lock, or replaced
			// by a newer one, does nothing.
			if room.lapse != lapse {
				room.mutex.Unlock()
				return
			}
			isLapsed := len(room.viewers) == 0 && room.controller == TabControllerPerson
			if isLapsed {
				room.controller = TabControllerAgent
			}
			room.lapse = nil
			room.mutex.Unlock()
			if isLapsed {
				log.Noticef("a taken over tab went back to the agent: nobody was watching it")
			}
		})
		room.lapse = lapse
	}
	room.mutex.Unlock()
	self.recast(agentId)
}

// WatchTab is a viewer saying which tab it watches: one the extension
// listed.
func (self *Agent) WatchTab(agentId string, viewer TabViewer, tab string) {
	room := self.existingRoom(agentId)
	if room == nil {
		return
	}
	room.mutex.Lock()
	if _, isOpen := room.viewers[viewer]; !isOpen {
		room.mutex.Unlock()
		return
	}
	isListed := false
	for _, listing := range room.tabs {
		if listing.Tab == tab {
			isListed = true
			break
		}
	}
	if isListed {
		room.viewers[viewer] = tab
	}
	room.mutex.Unlock()
	self.recast(agentId)
	self.tellViewers(agentId)
}

// recast asks the extension to stream exactly the tabs somebody watches.
func (self *Agent) recast(agentId string) {
	room := self.existingRoom(agentId)
	if room == nil {
		return
	}
	room.castMutex.Lock()
	defer room.castMutex.Unlock()
	tab := self.castingTab(agentId)
	room.mutex.Lock()
	wanted := map[string]bool{}
	for _, watching := range room.viewers {
		if watching != "" {
			wanted[watching] = true
		}
	}
	var starting, stopping []string
	for watching := range wanted {
		if !room.casting[watching] {
			starting = append(starting, watching)
		}
	}
	for casting := range room.casting {
		if !wanted[casting] {
			stopping = append(stopping, casting)
		}
	}
	if tab == nil {
		// Nothing to ask: the streams start when a browser that can cast
		// connects (castAgain).
		room.casting = map[string]bool{}
		room.mutex.Unlock()
		return
	}
	room.casting = wanted
	room.mutex.Unlock()
	for _, watching := range starting {
		tab.sendCast(watching, true)
	}
	for _, watching := range stopping {
		tab.sendCast(watching, false)
	}
}

// castAgain asks a newly connected browser for the streams the viewers
// want: the old connection's streams ended with it.
func (self *Agent) castAgain(agentId string) {
	room := self.existingRoom(agentId)
	if room == nil {
		return
	}
	room.castMutex.Lock()
	room.mutex.Lock()
	room.casting = map[string]bool{}
	room.mutex.Unlock()
	room.castMutex.Unlock()
	self.recast(agentId)
	self.tellViewers(agentId)
}

// tabCastMessage asks the extension to stream a tab, or to stop.
type tabCastMessage struct {
	Type      string `json:"type"`
	Tab       int64  `json:"tab"`
	IsOn      bool   `json:"isOn"`
	MaxWidth  int    `json:"maxWidth,omitempty"`
	MaxHeight int    `json:"maxHeight,omitempty"`
	Quality   int    `json:"quality,omitempty"`
}

// sendCast asks the extension to stream a tab or to stop. The extension
// numbers its tabs; the viewers name them as text.
func (self *attachedTab) sendCast(tab string, isOn bool) {
	number, err := strconv.ParseInt(tab, 10, 64)
	if err != nil {
		return
	}
	message := tabCastMessage{Type: "cast", Tab: number, IsOn: isOn}
	if isOn {
		message.MaxWidth, message.MaxHeight, message.Quality = castMaximumSide, castMaximumSide, castQuality
	}
	encoded, _ := json.Marshal(message)
	_ = self.connection.Send(encoded)
}

// tabFrameMessage is a picture of a tab, as a viewer is sent it: the JPEG,
// and the page's size in CSS pixels, which is what input is placed in.
type tabFrameMessage struct {
	Type   string  `json:"type"`
	Tab    string  `json:"tab"`
	Data   string  `json:"data"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// TabFrame is the extension sending a picture of a tab it streams: passed
// to every view of that tab, from the attached connection only.
func (self *Agent) TabFrame(agentId string, connection TabConnection, tab, data string, width, height float64) {
	attached := self.tabFor(agentId)
	if attached == nil || (connection != nil && attached.connection != connection) {
		return
	}
	room := self.existingRoom(agentId)
	if room == nil {
		return
	}
	room.mutex.Lock()
	var watchers []TabViewer
	for viewer, watching := range room.viewers {
		if watching == tab {
			watchers = append(watchers, viewer)
		}
	}
	// A stream nobody asked for, left by a stop that crossed a start: it
	// is stopped, once in a while rather than for every picture.
	isStray := !room.casting[tab] && time.Since(room.strayStopAt[tab]) > 5*time.Second
	if isStray {
		room.strayStopAt[tab] = time.Now()
	}
	room.mutex.Unlock()
	if isStray {
		attached.sendCast(tab, false)
	}
	if len(watchers) == 0 {
		return
	}
	message, _ := json.Marshal(tabFrameMessage{Type: "frame", Tab: tab, Data: data, Width: width, Height: height})
	for _, viewer := range watchers {
		viewer.SendFrame(message)
	}
}

// UpdateTabList is the extension saying which tabs the agent may act in.
func (self *Agent) UpdateTabList(agentId string, tabs []TabListing) {
	room := self.roomFor(agentId)
	room.mutex.Lock()
	room.tabs = append([]TabListing{}, tabs...)
	// A view of a tab that has gone watches nothing.
	for viewer, watching := range room.viewers {
		isListed := false
		for _, listing := range tabs {
			if listing.Tab == watching {
				isListed = true
				break
			}
		}
		if !isListed {
			room.viewers[viewer] = ""
		}
	}
	room.mutex.Unlock()
	self.recast(agentId)
	self.tellViewers(agentId)
}

// TabList is the tabs the extension last listed.
func (self *Agent) TabList(agentId string) []TabListing {
	room := self.existingRoom(agentId)
	if room == nil {
		return []TabListing{}
	}
	room.mutex.Lock()
	defer room.mutex.Unlock()
	return append([]TabListing{}, room.tabs...)
}

// TakeOverTab gives the person control of their tab: the agent's acting
// calls are refused until they hand it back. A browser that cannot stream
// a tab cannot take their input either, so it is not taken over; it says
// whether it was.
func (self *Agent) TakeOverTab(agentId string) bool {
	if self.castingTab(agentId) == nil {
		return false
	}
	room := self.roomFor(agentId)
	room.mutex.Lock()
	isNewlyHeld := room.controller != TabControllerPerson
	room.controller = TabControllerPerson
	room.mutex.Unlock()
	if isNewlyHeld {
		log.Noticef("the person took over their browser tab")
	}
	self.tellViewers(agentId)
	return true
}

// CastEnded is the extension saying a stream ended on its own: Chrome let
// the debugger go, because the person at the machine cancelled it or
// opened the developer tools there. The views of that tab watch nothing
// rather than freeze on the last picture; watching it again asks again.
func (self *Agent) CastEnded(agentId string, connection TabConnection, tab string) {
	attached := self.tabFor(agentId)
	if attached == nil || (connection != nil && attached.connection != connection) {
		return
	}
	room := self.existingRoom(agentId)
	if room == nil {
		return
	}
	room.castMutex.Lock()
	room.mutex.Lock()
	delete(room.casting, tab)
	for viewer, watching := range room.viewers {
		if watching == tab {
			room.viewers[viewer] = ""
		}
	}
	room.mutex.Unlock()
	room.castMutex.Unlock()
	self.tellViewers(agentId)
}

// HandBackTab gives the tab back to the agent. It says whether the person
// held it.
func (self *Agent) HandBackTab(agentId string) bool {
	room := self.existingRoom(agentId)
	if room == nil {
		return false
	}
	room.mutex.Lock()
	wasHeld := room.controller == TabControllerPerson
	room.controller = TabControllerAgent
	if room.lapse != nil {
		room.lapse.Stop()
		room.lapse = nil
	}
	room.mutex.Unlock()
	if wasHeld {
		log.Noticef("the person handed their browser tab back")
	}
	self.tellViewers(agentId)
	return wasHeld
}

// IsTabTakenOver says whether the person holds their tab.
func (self *Agent) IsTabTakenOver(agentId string) bool {
	room := self.existingRoom(agentId)
	if room == nil {
		return false
	}
	room.mutex.Lock()
	defer room.mutex.Unlock()
	return room.controller == TabControllerPerson
}

// TabInputMessage is what the person does in the tab they took over: a
// pointer moving or pressed, the wheel, a key, or text, as the DevTools
// protocol takes them. Read field by field and written again, so nothing
// but these reaches the extension.
type TabInputMessage struct {
	Type       string  `json:"type"`
	Tab        int64   `json:"tab"`
	Event      string  `json:"event,omitempty"`
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	Button     string  `json:"button,omitempty"`
	ClickCount int     `json:"clickCount,omitempty"`
	Modifiers  int     `json:"modifiers,omitempty"`
	DeltaX     float64 `json:"deltaX,omitempty"`
	DeltaY     float64 `json:"deltaY,omitempty"`
	Key        string  `json:"key,omitempty"`
	Code       string  `json:"code,omitempty"`
	KeyCode    int     `json:"keyCode,omitempty"`
	Text       string  `json:"text,omitempty"`
}

// tabInputTypes are the kinds of input a viewer may send.
var tabInputTypes = map[string]bool{"pointer": true, "wheel": true, "key": true, "text": true}

// TabInput passes what the person did in the picture to the tab the viewer
// watches, while they hold it; otherwise it is dropped.
func (self *Agent) TabInput(agentId string, viewer TabViewer, input TabInputMessage) {
	if !tabInputTypes[input.Type] {
		return
	}
	room := self.existingRoom(agentId)
	if room == nil {
		return
	}
	room.mutex.Lock()
	watching, isOpen := room.viewers[viewer]
	isHeld := room.controller == TabControllerPerson
	room.mutex.Unlock()
	if !isOpen || !isHeld || watching == "" {
		return
	}
	tab := self.castingTab(agentId)
	if tab == nil {
		return
	}
	number, err := strconv.ParseInt(watching, 10, 64)
	if err != nil {
		return
	}
	input.Tab = number
	encoded, _ := json.Marshal(input)
	_ = tab.connection.Send(encoded)
}
