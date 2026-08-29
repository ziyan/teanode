// Package smtpd implements a basic SMTP server
package smtpd

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"strings"
	"time"

	"github.com/op/go-logging"

	"github.com/ziyan/teanode/internal/util/bufferpool"
	"github.com/ziyan/teanode/internal/util/deferutil"
	"github.com/ziyan/teanode/internal/util/dropper"
	"github.com/ziyan/teanode/internal/util/geoip"
	"github.com/ziyan/teanode/internal/util/mailparse"
	"github.com/ziyan/teanode/internal/util/ratelimit"
	"github.com/ziyan/teanode/internal/util/security"
)

var log = logging.MustGetLogger("smtpd")

type HandleFunc func(ctx context.Context, envelope *mailparse.Envelope) error

type Resolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
	LookupAddr(ctx context.Context, addr string) ([]string, error)
}

type Settings struct {
	Outgoing       bool
	Greeting       string
	Timeout        time.Duration
	MaxSize        int
	MaxRecipients  int
	TLSConfig      *tls.Config
	Secret         []byte
	TrustedSenders []string
	Delay          time.Duration

	// RequireReverseDNS refuses a connection whose address has no reverse DNS
	// record resolving back to it. Only meaningful for incoming mail; a
	// submitting client is authenticated instead.
	RequireReverseDNS bool

	// AuthLimiter bounds how often one address may attempt to authenticate.
	// Nil disables the limit.
	//
	// Keyed by address rather than by credential on purpose: the credential
	// in a guess is the guess, so counting per credential counts the attacker's
	// input and limits nothing.
	AuthLimiter *ratelimit.Registry
}

func Serve(listener net.Listener, handle HandleFunc, locator geoip.Locator, resolver Resolver, dropper dropper.Dropper, settings *Settings) error {
	defer func() { _ = listener.Close() }()

	trustedSenders := make([]string, 0, len(settings.TrustedSenders))
	for _, trustedSender := range settings.TrustedSenders {
		trustedSenders = append(trustedSenders, fmt.Sprintf(".%s.", strings.Trim(trustedSender, ".")))
	}
	log.Debugf("trusted senders: %q", trustedSenders)

	for {
		conn, err := listener.Accept()
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			return err
		}

		// TODO: we should wait for the ongoing connections before exit
		go func(conn net.Conn) {
			defer deferutil.Recover()
			start := time.Now()
			log.Infof("accepted connection from %s", conn.RemoteAddr())
			defer func() {
				_ = conn.Close()
				log.Infof("closed connection from %s, elapsed %s", conn.RemoteAddr(), time.Since(start))
			}()

			// check ip against drop list
			ip := conn.RemoteAddr().(*net.TCPAddr).IP
			location := locator.Locate(ip)
			drop, err := dropper.Drop(ip)
			if err != nil {
				log.Errorf("failed to check ip %q: %s", ip, err)
				return
			}
			if drop {
				log.Warningf("drop ip %q from %q according to drop list", ip, location)
				return
			}

			// check rdns
			rdns := checkIp(ip, resolver, 5*time.Second)

			// discourage spammers
			if !settings.Outgoing {
				var trusted bool
				for _, trustedSender := range trustedSenders {
					if strings.HasSuffix(rdns, trustedSender) {
						trusted = true
						break
					}
				}
				if !trusted {
					log.Debugf("sender %q ip %q from %q is not trusted, delaying for %s", rdns, ip, location, settings.Delay)
					if err := delay(conn, settings.Delay); err != nil {
						log.Errorf("failed to delay connection %s from %q: %s", ip, location, err)
						return
					}
				}
			}

			session := &session{
				outgoing: settings.Outgoing,
				conn:     conn,
				text:     textproto.NewConn(conn),
				ip:       ip,
				rdns:     rdns,
				handle:   handle,
				settings: settings,
				location: location,
			}
			// serve always returns an error; io.EOF is how handleQuit
			// unwinds it after a client says QUIT, which is an ordinary end
			// to a conversation rather than something to log.
			if err := session.serve(); !errors.Is(err, io.EOF) {
				log.Errorf("%s: failed to serve connection: %s", session, err)
			}
		}(conn)
	}
}

func checkIp(ip net.IP, resolver Resolver, timeout time.Duration) string {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	addresses, err := resolver.LookupAddr(ctx, ip.String())
	if err != nil {
		log.Warningf("reverse dns lookup for %q failed: %s", ip, err)
		return ""
	}
	for _, address := range addresses {
		resolutions, err := resolver.LookupIPAddr(ctx, address)
		if err != nil {
			log.Warningf("failed to resolve domain %q: %s", address, err)
			continue
		}
		for _, resolution := range resolutions {
			if ip.Equal(resolution.IP) {
				return address
			}
		}
	}
	return ""
}

func delay(conn net.Conn, duration time.Duration) error {
	if err := conn.SetReadDeadline(time.Now().Add(duration)); err != nil {
		return err
	}
	buffer := make([]byte, 1)
	if _, err := conn.Read(buffer); err != nil {
		if err, ok := err.(net.Error); ok && err.Timeout() {
			return nil
		}
		return err
	}
	return fmt.Errorf("smtpd: received data too early")
}

type session struct {
	outgoing bool

	handle   HandleFunc
	settings *Settings

	conn net.Conn
	text *textproto.Conn

	ip       net.IP
	rdns     string
	location *geoip.Location

	tls *tls.ConnectionState

	// remote hostname as supplied with ehlo
	hello string

	// auth
	credentialId  string
	credentialKey string

	// envelope id
	id string

	// also known as return path
	sender *string

	// all recipients
	recipients []string

	specialPrefix string
	specialId     string
}

func (self *session) String() string {
	var parameters []string
	if self.outgoing {
		parameters = append(parameters, "outgoing")
	} else {
		parameters = append(parameters, "incoming")
	}
	parameters = append(parameters, fmt.Sprintf("ip=%q", self.conn.RemoteAddr()))
	if self.rdns != "" {
		parameters = append(parameters, fmt.Sprintf("rdns=%q", self.rdns))
	}
	parameters = append(parameters, fmt.Sprintf("location=%q", self.location))
	if self.tls != nil {
		parameters = append(parameters, "tls")
	}
	if self.hello != "" {
		parameters = append(parameters, fmt.Sprintf("hello=%q", self.hello))
	}
	if self.id != "" {
		parameters = append(parameters, fmt.Sprintf("id=%q", self.id))
	}
	return fmt.Sprintf("<session(%s)>", strings.Join(parameters, ", "))
}

func (self *session) reset() {
	self.id = security.NewULID()
	self.sender = nil
	self.recipients = nil
	self.specialPrefix = ""
	self.specialId = ""
}

func (self *session) logout() {
	self.hello = ""
	self.credentialId = ""
	self.credentialKey = ""
	self.reset()
}

// Function called to handle connection requests.
func (self *session) serve() error {
	if err := self.writeLines(220, "", self.settings.Greeting); err != nil {
		return err
	}

	for {
		// Attempt to read a line from the socket.
		// On timeout, send a timeout message and return from serve().
		// On error, assume the client has gone away i.e. return from serve().
		verb, args, err := self.readCommand()
		if err != nil {
			if err != io.EOF {
				_ = self.writeLines(421, "4.3.0", "Error")
			}
			return err
		}
		switch verb {
		case "QUIT":
			err = self.handleQuit()
		case "STARTTLS":
			err = self.handleStartTLS(args)
		case "HELO":
			err = self.handleHelo(args)
		case "EHLO":
			err = self.handleEhlo(args)
		case "AUTH":
			if self.outgoing {
				err = self.handleAuth(args)
			} else {
				err = self.writeLines(502, "5.5.1", "Command not implemented")
			}
		case "MAIL":
			err = self.handleMail(args)
		case "RCPT":
			err = self.handleRcpt(args)
		case "DATA":
			err = self.handleData()
		case "RSET":
			err = self.handleRset()
		default:
			err = self.handleDefault(verb)
		}
		if err != nil {
			return err
		}
	}
}

func (self *session) handleDefault(verb string) error {
	switch verb {
	case "NOOP":
		return self.writeLines(250, "2.0.0", "OK")
	case "VRFY":
		return self.writeLines(252, "2.3.0", "Cannot VRFY user, but will accept message and attempt delivery")
	case "HELP", "EXPN":
		// See RFC 5321 section 4.2.4 for usage of 500 & 502 response codes.
		return self.writeLines(502, "5.5.1", "Command not implemented")
	}
	// See RFC 5321 section 4.2.4 for usage of 500 & 502 response codes.
	return self.writeLines(500, "5.5.2", "Syntax error")
}

func (self *session) handleQuit() error {
	_ = self.writeLines(221, "2.0.0", "Bye")
	return io.EOF // close the connection
}

func (self *session) checkHello(args string) error {
	var hello string
	if parts := strings.Fields(args); len(parts) > 0 {
		hello = parts[0]
	}
	if hello == "" {
		return self.writeLines(501, "5.5.2", "Domain or address argument required")
	}
	if !self.outgoing && self.settings.RequireReverseDNS && self.rdns == "" {
		_ = self.writeLines(550, "5.7.25", "Reverse DNS lookup failed")
		return fmt.Errorf("smtpd: reverse dns failed")
	}
	self.hello = hello
	return nil
}

func (self *session) handleHelo(args string) error {
	// RFC 2821 section 4.1.4 specifies that EHLO has the same effect as RSET, so reset for HELO too.
	self.logout()

	if err := self.checkHello(args); err != nil {
		return err
	}
	return self.writeLines(250, "2.0.0", self.settings.Greeting)
}

func (self *session) handleEhlo(args string) error {
	// RFC 2821 section 4.1.4 specifies that EHLO has the same effect as RSET.
	self.logout()

	if err := self.checkHello(args); err != nil {
		return err
	}
	lines := []string{
		self.settings.Greeting,
		fmt.Sprintf("SIZE %d", self.settings.MaxSize),
		"8BITMIME",
		"SMTPUTF8",
		"PIPELINING",
		"ENHANCEDSTATUSCODES",
	}
	// Only offered when there is something to complete the handshake with.
	//
	// Advertising it regardless is worse than not offering it: a sender that
	// takes the offer and then fails the handshake either defers the message
	// or, as Exchange Online does, retries immediately without encryption. So
	// the mail arrives in plaintext, and the fact that it did is recorded
	// nowhere the operator will look. A server with no certificate yet — the
	// first fifteen minutes of a new deployment, before ACME finishes — should
	// simply say it cannot do TLS, and let the sender decide.
	if self.tls == nil && self.canStartTLS() {
		lines = append(lines, "STARTTLS")
	}
	if self.tls != nil && self.outgoing {
		lines = append(lines, "AUTH PLAIN")
	}
	return self.writeLines(250, "", lines...)
}

// canStartTLS reports whether a handshake would have a certificate to offer.
// The question is asked of the same source the handshake would use, so the
// answer cannot drift from it.
func (self *session) canStartTLS() bool {
	configuration := self.settings.TLSConfig
	if configuration == nil {
		return false
	}
	if len(configuration.Certificates) > 0 {
		return true
	}
	if configuration.GetCertificate == nil {
		return false
	}
	// An empty ServerName is what a client sending no SNI produces, which is
	// the least the certificate source has to be able to answer.
	certificate, err := configuration.GetCertificate(&tls.ClientHelloInfo{
		SupportedVersions: []uint16{tls.VersionTLS13, tls.VersionTLS12},
	})
	return err == nil && certificate != nil
}

func (self *session) handleMail(args string) error {
	self.reset()

	if self.hello == "" {
		return self.writeLines(503, "5.5.1", "Bad sequence of commands")
	}

	if self.outgoing && self.credentialId == "" {
		return self.writeLines(503, "5.5.1", "Bad sequence of commands")
	}

	if !strings.HasPrefix(strings.ToUpper(args), "FROM:") {
		return self.writeLines(501, "5.5.2", "Syntax error")
	}
	fields := strings.Fields(args[len("FROM:"):])
	if len(fields) == 0 || !strings.HasPrefix(fields[0], "<") || !strings.HasSuffix(fields[0], ">") {
		return self.writeLines(501, "5.5.2", "Syntax error")
	}
	var sender string
	if fields[0] != "<>" {
		address, err := mailparse.ParseAddress(fields[0])
		if err != nil {
			return self.writeLines(501, "5.5.2", "Syntax error")
		}
		sender = address
	}
	self.sender = &sender
	return self.writeLines(250, "2.0.0", "OK")
}

func (self *session) handleRcpt(args string) error {
	if self.hello == "" || self.sender == nil {
		return self.writeLines(503, "5.5.1", "Bad sequence of commands")
	}
	if !strings.HasPrefix(strings.ToUpper(args), "TO:") {
		return self.writeLines(501, "5.5.2", "Syntax error")
	}
	fields := strings.Fields(args[len("TO:"):])
	if len(fields) == 0 || !strings.HasPrefix(fields[0], "<") || !strings.HasSuffix(fields[0], ">") {
		return self.writeLines(501, "5.5.2", "Syntax error")
	}
	recipient, err := mailparse.ParseAddress(fields[0])
	if err != nil {
		return self.writeLines(501, "5.5.2", "Syntax error")
	}

	// special address handling
	if strings.HasPrefix(recipient, "dsn-") || strings.HasPrefix(recipient, "rua-") || strings.HasPrefix(recipient, "ruf-") {
		if prefix, id, err := mailparse.ValidateAddress(recipient, self.settings.Secret); err == nil {
			if len(self.recipients) > 0 {
				return self.writeLines(451, "4.5.3", "Too many recipients")
			}
			self.specialPrefix = prefix
			self.specialId = id
			self.recipients = append(self.recipients, recipient)
			return self.writeLines(250, "2.0.0", "OK")
		} else {
			log.Warningf("mail address %q invalid: %s", recipient, err)
		}
	}

	// non-sepcial address
	// but only one recipient allowed if we've seen a sepcial address
	if self.specialPrefix != "" {
		return self.writeLines(451, "4.5.3", "Too many recipients")
	}

	// require sender to be set if it is non special recipient
	if *self.sender == "" {
		return self.writeLines(501, "5.1.8", "Bad sender address")
	}

	if len(self.recipients) >= self.settings.MaxRecipients {
		return self.writeLines(451, "4.5.3", "Too many recipients")
	}
	for _, r := range self.recipients {
		if r == recipient {
			return self.writeLines(451, "4.5.3", "Duplicate recipients")
		}
	}
	self.recipients = append(self.recipients, recipient)
	return self.writeLines(250, "2.0.0", "OK")
}

func (self *session) handleData() error {
	if self.hello == "" || self.sender == nil || len(self.recipients) == 0 {
		return self.writeLines(503, "5.5.1", "Bad sequence of commands")
	}

	if err := self.writeLines(354, "2.0.0", "End data with <CR><LF>.<CR><LF>"); err != nil {
		return err
	}

	// get a buffer from pool
	buffer, releaseBuffer := bufferpool.AcquireBuffer()
	defer releaseBuffer()

	// read data and check size
	if err := self.readData(buffer, self.settings.MaxSize+1024); err != nil {
		_ = self.writeLines(421, "4.3.0", "Error")
		return err
	}
	size := buffer.Len()
	if size > self.settings.MaxSize {
		_ = self.writeLines(552, "5.3.4", fmt.Sprintf("Message too big (%d)", self.settings.MaxSize))
		return fmt.Errorf("smtpd: message too big")
	}

	headers, body, err := mailparse.Split(buffer)
	if err != nil {
		log.Errorf("failed to split mail: %s", err)
		_ = self.writeLines(421, "4.3.0", "Error")
		return err
	}

	// prepare to handle
	envelope := &mailparse.Envelope{
		ID:            self.id,
		IP:            self.ip,
		RDNS:          self.rdns,
		Location:      self.location,
		Hello:         self.hello,
		CredentialID:  self.credentialId,
		CredentialKey: self.credentialKey,
		Sender:        *self.sender,
		Recipients:    self.recipients,
		SpecialPrefix: self.specialPrefix,
		SpecialID:     self.specialId,
		TLS:           self.tls,
		ReceivedAt:    time.Now().In(time.Local),
		Size:          uint64(size),
		Headers:       headers,
		Body:          body,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// TODO: cancel properly when connection is going away

	if err := self.handle(ctx, envelope); err != nil {
		log.Errorf("failed to handle envelope: %s: %s", envelope, err)
		var detailedError *mailparse.Error
		if errors.As(err, &detailedError) {
			_ = self.writeLines(detailedError.StatusCode(), detailedError.EnhancedStatusCodes(), detailedError.Message())
		} else {
			_ = self.writeLines(421, "4.3.0", "Error") // temporary error
		}
		return err
	}

	self.reset()
	return self.writeLines(250, "2.0.0", "Queued")
}

func (self *session) handleRset() error {
	self.reset()
	return self.writeLines(250, "2.0.0", "OK")
}

func (self *session) handleStartTLS(args string) error {
	self.logout()

	// Parameters are not allowed (RFC 3207 section 4).
	if args != "" {
		return self.writeLines(501, "5.5.2", "Syntax error")
	}

	// Handle case where STARTTLS is received when TLS is already in use.
	if self.tls != nil {
		return self.writeLines(503, "5.5.1", "Bad sequence of commands")
	}

	// A client may try it even though the greeting did not offer it. Saying so
	// is better than saying "Ready" and then failing the handshake, which
	// leaves the sender to guess whether to retry in the clear.
	if !self.canStartTLS() {
		return self.writeLines(454, "4.7.0", "TLS not available at the moment")
	}

	if err := self.writeLines(220, "2.0.0", "Ready"); err != nil {
		return err
	}

	// Establish a TLS connection with the client.
	tlsConn := tls.Server(self.conn, self.settings.TLSConfig)
	if err := tlsConn.SetReadDeadline(time.Now().Add(time.Minute)); err != nil {
		log.Errorf("%s: failed to set read deadline: %s", self, err)
		return err
	}
	if err := tlsConn.SetWriteDeadline(time.Now().Add(time.Minute)); err != nil {
		log.Errorf("%s: failed to set write deadline: %s", self, err)
		return err
	}
	if err := tlsConn.Handshake(); err != nil {
		log.Errorf("%s: failed during tls handshake: %s", self, err)
		return err
	}

	// TLS handshake succeeded, switch to using the TLS connection.
	self.conn = tlsConn
	self.text = textproto.NewConn(tlsConn)
	tls := tlsConn.ConnectionState()
	self.tls = &tls

	// RFC 3207 specifies that the server must discard any prior knowledge obtained from the client.
	return nil
}

func (self *session) handleAuth(args string) error {
	self.reset()

	if !self.outgoing || self.tls == nil || self.hello == "" {
		return self.writeLines(503, "5.5.1", "Bad sequence of commands")
	}

	// Before the credential is even parsed, so that a refused address costs
	// nothing to refuse.
	if self.settings.AuthLimiter != nil && !self.settings.AuthLimiter.Allow(self.ip.String()) {
		log.Warningf("%s: too many authentication attempts", self)
		return self.writeLines(454, "4.7.0", "Too many authentication attempts, try again later")
	}

	parts := strings.Fields(args)
	if len(parts) == 0 {
		return self.writeLines(502, "5.5.4", "Missing parameters")
	}
	switch strings.ToUpper(parts[0]) {
	case "PLAIN":
	default:
		return self.writeLines(504, "5.7.4", "Unsupported authentication method")
	}

	var encoded string
	if len(parts) > 1 {
		encoded = parts[1]
	} else {
		if err := self.writeLines(334, "", ""); err != nil {
			return err
		}
		line, err := self.readLine()
		if err != nil {
			return err
		}
		encoded = strings.TrimSpace(line)
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return self.writeLines(454, "4.7.0", "Invalid base64 data")
	}

	decodedParts := bytes.Split(decoded, []byte{0})
	if len(decodedParts) != 3 || len(decodedParts[0]) > 0 {
		return self.writeLines(454, "4.7.0", "Invalid credentials")
	}
	credentialId, credentialKey, err := security.DecodeCredential(string(decodedParts[1]), string(decodedParts[2]), self.settings.Secret)
	if err != nil {
		log.Errorf("invalid credential: %s", err)
		return self.writeLines(454, "4.7.0", "Invalid credentials")
	}
	self.credentialId = credentialId
	self.credentialKey = credentialKey
	log.Debugf("authenticated as credential id %q", credentialId)
	return self.writeLines(235, "2.7.0", "Authentication succeeded")
}

// Wrapper function for writing a complete line to the socket.
func (self *session) writeLines(statusCode int, enhancedStatusCodes string, lines ...string) error {
	log.Debugf("%s: sending: statusCode = %d, enhancedStatusCodes = %s, lines = %v", self, statusCode, enhancedStatusCodes, lines)

	if err := self.conn.SetWriteDeadline(time.Now().Add(time.Minute)); err != nil {
		log.Errorf("%s: failed to set write deadline: %s", self, err)
		return err
	}

	for index, line := range lines {
		var err error
		if index+1 < len(lines) {
			err = self.text.PrintfLine("%d-%s", statusCode, line)
		} else {
			if enhancedStatusCodes != "" {
				err = self.text.PrintfLine("%d %s %s", statusCode, enhancedStatusCodes, line)
			} else {
				err = self.text.PrintfLine("%d %s", statusCode, line)
			}
		}
		if err != nil {
			// log.Errorf("%s: failed to write: %s", self, err)
			return err
		}
	}
	return nil
}

// Read a complete line from the socket.
func (self *session) readLine() (string, error) {
	if err := self.conn.SetReadDeadline(time.Now().Add(time.Minute)); err != nil {
		log.Errorf("%s: failed to set read deadline: %s", self, err)
		return "", err
	}
	line, err := self.text.ReadLine()
	if err != nil {
		return "", err
	}
	return line, nil
}

func (self *session) readCommand() (string, string, error) {
	line, err := self.readLine()
	if err != nil {
		return "", "", err
	}
	var verb, args string
	if index := strings.Index(line, " "); index >= 0 {
		verb = strings.ToUpper(line[:index])
		args = strings.TrimSpace(line[index+len(" "):])
	} else {
		verb = strings.ToUpper(line)
		args = ""
	}
	log.Debugf("%s: received: %s %s", self, verb, args)
	return verb, args, nil
}

// Read the message data following a DATA command.
func (self *session) readData(writer io.Writer, limit int) error {
	if err := self.conn.SetReadDeadline(time.Now().Add(self.settings.Timeout)); err != nil {
		log.Errorf("%s: failed to set read deadline: %s", self, err)
		return err
	}
	n, err := io.Copy(writer, &io.LimitedReader{
		R: self.text.DotReader(),
		N: int64(limit),
	})
	log.Debugf("%s: received: %d bytes of data, err is %v", self, n, err)
	return err
}
