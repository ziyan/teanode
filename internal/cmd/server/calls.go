package server

import (
	"context"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/call"
	"github.com/ziyan/teanode/internal/config"
)

// callAddressLookupWait bounds resolving the dashboard's name for the media
// server's address at start.
const callAddressLookupWait = 10 * time.Second

// openCallServer starts the media server of calls in chats when calls are on.
// A server whose ports cannot be opened, or that has no address to give
// devices, starts without calls and says why: mail and everything else go on.
func (self *server) openCallServer(configuration *config.Configuration) {
	settings := configuration.Calls
	if !settings.Enabled {
		return
	}
	publicAddress := strings.TrimSpace(settings.PublicAddress)
	if publicAddress == "" {
		publicAddress = resolveDashboardAddress(configuration)
	}
	if publicAddress == "" {
		log.Warningf("calls are off: calls.publicAddress is not set and the dashboard's name does not resolve to an IPv4 address")
		return
	}
	server, err := call.New(call.Settings{PublicAddress: publicAddress, UDPPort: int(settings.UDPPort), TCPPort: int(settings.TCPPort)})
	if err != nil {
		log.Warningf("calls are off: %s", err)
		return
	}
	// Under the data directory, so that what a stopped server kept of a call
	// being transcribed is there when it starts again, to become the call's
	// recording; a call's files are removed once they have.
	recordings := filepath.Join(configuration.DataDirectory(), "call-recordings")
	if err := os.MkdirAll(recordings, 0o700); err != nil {
		log.Warningf("calls cannot be transcribed: %s", err)
	}
	recorder := call.NewRecorder(recordings)
	server.SetAudioRecorder(recorder.Record)
	self.callServer, self.callRecorder = server, recorder
	self.onClose(func() {
		if err := server.Close(); err != nil {
			log.Warningf("cannot stop the media server: %s", err)
		}
	})
	log.Infof("calls go through %s, UDP and TCP ports %d and %d", publicAddress, settings.UDPPort, settings.TCPPort)
}

// resolveDashboardAddress is the IPv4 address the dashboard's name resolves
// to, or empty.
func resolveDashboardAddress(configuration *config.Configuration) string {
	base, err := url.Parse(configuration.DashboardBase())
	if err != nil || base.Hostname() == "" {
		return ""
	}
	if parsed := net.ParseIP(base.Hostname()); parsed != nil && parsed.To4() != nil {
		return parsed.String()
	}
	ctx, cancel := context.WithTimeout(context.Background(), callAddressLookupWait)
	defer cancel()
	addresses, err := net.DefaultResolver.LookupIP(ctx, "ip4", base.Hostname())
	if err != nil || len(addresses) == 0 {
		return ""
	}
	return addresses[0].String()
}
