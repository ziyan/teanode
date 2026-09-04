// Package smtpc implements an SMTP client.
package smtpc

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"regexp"
	"strings"
	"time"

	"github.com/op/go-logging"

	"github.com/ziyan/teanode/internal/util/connctx"
)

var log = logging.MustGetLogger("smtpd")

// TLSMode says how much this client insists on encryption, and whether it
// checks who it is talking to.
type TLSMode string

const (
	// TLSOpportunistic uses STARTTLS when the server offers it and does not
	// check the certificate. This is the right setting for delivering to
	// somebody else's MX: most present a certificate for the wrong name or
	// none at all, and refusing them would mean not delivering the mail,
	// which is worse than delivering it in the clear.
	TLSOpportunistic TLSMode = ""

	// TLSRequired demands STARTTLS and checks the certificate against
	// ServerName. For a relay this server has been configured to use: there
	// is a name to check, a password about to be sent, and an operator who
	// can fix it if the certificate is wrong.
	TLSRequired TLSMode = "starttls"

	// TLSImplicit expects TLS from the first byte, as port 465 does, and
	// checks the certificate the same way.
	TLSImplicit TLSMode = "tls"
)

type Settings struct {
	Hello   string
	Timeout time.Duration

	// TLS is how encryption is negotiated. The zero value is opportunistic,
	// which is what delivering to an arbitrary MX wants.
	TLS TLSMode

	// ServerName is the name the certificate is checked against, when TLS is
	// checked at all. Empty leaves the check off even in the modes above,
	// which is a mistake the caller has to make deliberately.
	ServerName string

	// RootCAs are the authorities the certificate is checked against. Nil
	// means the host's own, which is what a relay on the internet needs; a
	// pool is for one whose certificate comes from a private authority.
	RootCAs *x509.CertPool
}

// tlsConfig is what a checked connection uses.
func (self *Settings) tlsConfig() *tls.Config {
	if self.ServerName == "" {
		// Nothing to check against. Encryption without authentication still
		// stops a passive listener, which is all opportunistic TLS ever gave.
		return &tls.Config{InsecureSkipVerify: true}
	}
	return &tls.Config{ServerName: self.ServerName, RootCAs: self.RootCAs}
}

func Send(ctx context.Context, conn net.Conn, username, password, from string, recipients []string, data []byte, settings *Settings) error {
	cleanUp := connctx.SetDeadlineAndWatchForCancel(ctx, conn)
	defer cleanUp()

	self := &client{
		settings: settings,
		conn:     conn,
		text:     textproto.NewConn(conn),
	}

	// On an implicit-TLS port the handshake comes before the banner, so there
	// is nothing to negotiate and nothing to read until it is done.
	if settings.TLS == TLSImplicit {
		if err := self.wrapTls(); err != nil {
			return err
		}
	}

	// wait for initial banner
	if err := self.wait(); err != nil {
		return err
	}

	// say hello
	if err := self.hello(); err != nil {
		return err
	}

	// start tls if supported
	if _, ok := self.extensions["STARTTLS"]; ok && settings.TLS != TLSImplicit {
		if err := self.startTls(); err != nil {
			return err
		}
		if err := self.hello(); err != nil {
			return err
		}
	}

	// Asked for and not offered. Continuing would send the password, and the
	// message, in the clear to a server the operator said to encrypt to.
	if settings.TLS == TLSRequired && !self.tls {
		return fmt.Errorf("smtpc: %s does not offer STARTTLS, and this relay is configured to require it", self)
	}

	// auth
	if username != "" || password != "" {
		if err := self.auth(username, password); err != nil {
			return err
		}
	}

	// start mail from
	if err := self.mail(from, len(data)); err != nil {
		return err
	}

	// set recipients
	for _, recipient := range recipients {
		if err := self.rcpt(recipient); err != nil {
			return err
		}
	}

	// send data
	return self.data(data)
}

type client struct {
	settings   *Settings
	conn       net.Conn
	text       *textproto.Conn
	extensions map[string]string
	tls        bool
}

func (self *client) String() string {
	var parameters []string
	parameters = append(parameters, fmt.Sprintf("ip=%q", self.conn.RemoteAddr()))
	return fmt.Sprintf("<client(%s)>", strings.Join(parameters, ", "))
}

func (self *client) wait() error {
	_, _, err := self.receiveResponse(220, time.Minute)
	return err
}

func (self *client) startTls() error {
	if _, _, err := self.sendCommand(220, "STARTTLS"); err != nil {
		return err
	}
	return self.wrapTls()
}

// wrapTls puts the connection inside TLS, whether that was negotiated with
// STARTTLS or expected from the first byte.
//
// The handshake is completed here rather than left to the first read, so that
// a certificate this client will not accept is reported as what it is instead
// of as a confusing failure to read a banner.
func (self *client) wrapTls() error {
	tlsConn := tls.Client(self.conn, self.settings.tlsConfig())
	if err := tlsConn.Handshake(); err != nil {
		return fmt.Errorf("smtpc: tls handshake with %s failed: %w", self, err)
	}
	self.conn = tlsConn
	self.text = textproto.NewConn(tlsConn)
	self.tls = true
	return nil
}

func (self *client) hello() error {
	_, message, err := self.sendCommand(250, "EHLO %s", self.settings.Hello)
	if err != nil {
		return err
	}
	extensions := make(map[string]string)
	if lines := strings.Split(message, "\n"); len(lines) > 1 {
		for _, line := range lines[1:] {
			parts := strings.SplitN(strings.ToUpper(line), " ", 2)
			if len(parts) > 1 {
				extensions[parts[0]] = parts[1]
			} else {
				extensions[parts[0]] = ""
			}
		}
	}
	self.extensions = extensions
	return nil
}

func (self *client) auth(username, password string) error {
	if !self.tls {
		return fmt.Errorf("smtpc: refuse to authenticate, server does not support starttls")
	}
	encoded := base64.StdEncoding.EncodeToString(bytes.Join([][]byte{
		nil,
		[]byte(username),
		[]byte(password),
	}, []byte{0}))
	if _, _, err := self.sendCommand(235, "AUTH PLAIN %s", encoded); err != nil {
		return err
	}
	return nil
}

func (self *client) mail(from string, size int) error {
	var err error
	if _, ok := self.extensions["SIZE"]; ok {
		_, _, err = self.sendCommand(250, "MAIL FROM:<%s> SIZE=%d", from, size)
	} else {
		_, _, err = self.sendCommand(250, "MAIL FROM:<%s>", from)
	}
	return err
}

func (self *client) rcpt(recipient string) error {
	if _, _, err := self.sendCommand(250, "RCPT TO:<%s>", recipient); err != nil {
		return err
	}
	return nil
}

func (self *client) data(data []byte) error {
	if _, _, err := self.sendCommand(354, "DATA"); err != nil {
		return err
	}

	if err := self.conn.SetWriteDeadline(time.Now().Add(self.settings.Timeout)); err != nil {
		return err
	}

	if err := func() error {
		writerCloser := self.text.DotWriter()
		defer func() { _ = writerCloser.Close() }()

		if _, err := io.Copy(writerCloser, bytes.NewBuffer(data)); err != nil {
			return err
		}
		return nil
	}(); err != nil {
		return err
	}

	if _, _, err := self.receiveResponse(250, self.settings.Timeout); err != nil {
		return err
	}
	return nil
}

func (self *client) sendCommand(expectedStatusCode int, format string, args ...interface{}) (int, string, error) {
	if err := self.conn.SetWriteDeadline(time.Now().Add(time.Minute)); err != nil {
		return 0, "", err
	}

	log.Debugf("%s: sending: %s", self, fmt.Sprintf(format, args...))
	id, err := self.text.Cmd(format, args...)
	if err != nil {
		return 0, "", err
	}

	self.text.StartResponse(id)
	defer self.text.EndResponse(id)
	return self.receiveResponse(expectedStatusCode, time.Minute)
}

func (self *client) receiveResponse(expectedStatusCode int, timeout time.Duration) (int, string, error) {
	if err := self.conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return 0, "", err
	}
	statusCode, message, err := self.text.ReadResponse(expectedStatusCode)
	log.Debugf("%s: received: %d (expecting %d): %s: %v", self, statusCode, expectedStatusCode, message, err)
	return statusCode, message, err
}

// enhancedStatusCode matches the class.subject.detail code of RFC 3463 at the
// start of a line.
var enhancedStatusCode = regexp.MustCompile(`^([245]\.\d{1,3}\.\d{1,3})\s+`)

// CollapseResponse turns a multi-line reply into one line of prose.
//
// textproto strips the "550-" from each continuation line but not the
// enhanced status code after it, which RFC 2034 says appears on every line.
// Joined up, a refusal from Gmail came out as
//
//	5.7.1 [2406:…] Gmail has detected that this 5.7.1 message is likely
//	suspicious due to the very low reputation of the 5.7.1 sending domain…
//
// with the code sprayed through the middle of the sentence. The code is kept
// once, at the front, where it belongs.
//
// Not applied to every reply as it is read: an EHLO reply is multi-line
// because each line is a capability, and flattening that turns the list into
// a sentence the client cannot parse. This is for a reply being recorded as
// an error, where the lines are one message split up.
func CollapseResponse(message string) string {
	lines := strings.Split(message, "\n")
	if len(lines) < 2 {
		return message
	}

	// Only strip the code the first line carries. A continuation that starts
	// with some other number is text, not a code, and removing it would
	// change what the remote server said.
	first := enhancedStatusCode.FindStringSubmatch(lines[0])
	collapsed := make([]string, 0, len(lines))
	collapsed = append(collapsed, strings.TrimSpace(lines[0]))

	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if first != nil {
			line = strings.TrimSpace(strings.TrimPrefix(line, first[1]))
		}
		if line != "" {
			collapsed = append(collapsed, line)
		}
	}
	return strings.Join(collapsed, " ")
}
