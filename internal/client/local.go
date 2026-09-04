package client

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/config"
)

// LocalTokenLifetime is how long a minted token lasts. One command's worth,
// with room for a slow reply and a clock that is a little out.
const LocalTokenLifetime = 5 * time.Minute

// Local builds a Client for the server described by a configuration file,
// connecting over the loopback interface and authenticating with a token
// minted from the server secret.
//
// This is what "teanode credential add" uses when it is run on the server, and
// the reason no token has to be set up before the tool is usable there.
func Local(configuration *config.Configuration) (*Client, error) {
	token, err := configuration.MintLocalToken(LocalTokenLifetime)
	if err != nil {
		return nil, err
	}

	// Plain HTTP over loopback is preferred: the same handler is served on
	// both listeners, and it avoids having to decide whether a certificate
	// issued for the server's public name should be trusted when reached at
	// 127.0.0.1.
	if port := portOf(configuration.Listen.HTTP); port != "" {
		return New(Options{URL: "http://127.0.0.1:" + port, Token: token})
	}

	port := portOf(configuration.Listen.HTTPS)
	if port == "" {
		return nil, fmt.Errorf("client: the configuration has no listen.http or listen.https address, so there is no API to talk to")
	}

	tlsConfig, err := loopbackTlsConfig(configuration)
	if err != nil {
		return nil, err
	}
	return New(Options{
		URL:        "https://127.0.0.1:" + port,
		Token:      token,
		HTTPClient: &http.Client{Transport: &http.Transport{TLSClientConfig: tlsConfig}},
	})
}

// loopbackTlsConfig trusts exactly the certificate this server is configured
// to present, and nothing else.
//
// The name in that certificate is the server's public one, which 127.0.0.1 is
// not, so ordinary verification cannot be used. Comparing the presented
// certificate against the configured one is stricter than name verification
// anyway: it accepts one certificate rather than any certificate a public
// authority would issue.
func loopbackTlsConfig(configuration *config.Configuration) (*tls.Config, error) {
	expected := loadConfiguredCertificate(configuration)
	if expected == nil {
		// No certificate configured yet, which is the state a server is in
		// before ACME has issued one. The connection stays on loopback and
		// the token is bound to a secret only a local reader has, so there is
		// nothing here for a network attacker to take.
		return &tls.Config{InsecureSkipVerify: true}, nil
	}

	return &tls.Config{
		// Verification is done below instead; Go requires this to be set for
		// VerifyPeerCertificate to be reached with an unverifiable chain.
		InsecureSkipVerify: true,
		VerifyPeerCertificate: func(certificates [][]byte, _ [][]*x509.Certificate) error {
			if len(certificates) == 0 {
				return fmt.Errorf("client: the local server presented no certificate")
			}
			for _, candidate := range expected {
				if string(candidate) == string(certificates[0]) {
					return nil
				}
			}
			return fmt.Errorf("client: the local server presented a certificate that is not the one in the configuration")
		},
	}, nil
}

// loadConfiguredCertificate returns the DER of every certificate the
// configuration says this server presents, from whichever of the three places
// it lives in. Several are returned because a renewal may have replaced the
// inline copy while the running process still holds the old one.
func loadConfiguredCertificate(configuration *config.Configuration) [][]byte {
	var sources []string
	if configuration.TLS.ACME.Certificate != "" {
		sources = append(sources, configuration.TLS.ACME.Certificate)
	}
	for _, filename := range []string{configuration.TLS.CertificateFile, configuration.TLS.ACME.CertificateFile} {
		if filename == "" {
			continue
		}
		content, err := os.ReadFile(configuration.Path(filename))
		if err != nil {
			continue
		}
		sources = append(sources, string(content))
	}

	var leaves [][]byte
	for _, source := range sources {
		if leaf := firstCertificate([]byte(source)); leaf != nil {
			leaves = append(leaves, leaf)
		}
	}
	return leaves
}

// firstCertificate returns the DER of the leaf in a PEM bundle, which is the
// first certificate in it by convention and by what every ACME client writes.
func firstCertificate(content []byte) []byte {
	for {
		block, rest := pem.Decode(content)
		if block == nil {
			return nil
		}
		if block.Type == "CERTIFICATE" {
			return block.Bytes
		}
		content = rest
	}
}

// portOf extracts the port from a listen address such as ":443" or
// "127.0.0.1:443".
func portOf(address string) string {
	address = strings.TrimSpace(address)
	if address == "" {
		return ""
	}
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return ""
	}
	return port
}
