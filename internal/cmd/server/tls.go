package server

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/cmd"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/util/atomicfile"
)

// NewTLSCommand builds "teanode tls".
func NewTLSCommand() *cli.Command {
	return &cli.Command{
		Name:  "tls",
		Usage: "manage the certificate used for HTTPS and STARTTLS",
		Commands: []*cli.Command{
			{
				Name:  "self-signed",
				Usage: "generate a self-signed certificate for development",
				Description: "Writes a certificate and key into the data directory and points the\n" +
					"configuration at them. Nothing will trust this certificate, so it is for\n" +
					"development only — but without one the submission port cannot offer\n" +
					"STARTTLS, and without STARTTLS it will not accept a password.",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "certificate-file",
						Usage: "where to write the certificate, relative to the data directory",
						Value: "self-signed.crt",
					},
					&cli.StringFlag{
						Name:  "private-key-file",
						Usage: "where to write the key, relative to the data directory",
						Value: "self-signed.key",
					},
					&cli.DurationFlag{
						Name:  "lifetime",
						Usage: "how long the certificate is valid for",
						Value: 365 * 24 * time.Hour,
					},
				},
				Action: runTlsSelfSigned,
			},
		},
	}
}

func runTlsSelfSigned(ctx context.Context, command *cli.Command) error {
	store, closeDatabase, err := cmd.OpenLocalStore()
	if err != nil {
		return err
	}
	defer closeDatabase()
	defer func() {
		_ = store.Close()
	}()

	configuration := store.Current()
	if err := configuration.EnsureDataDirectory(); err != nil {
		return err
	}

	hosts := configuration.TLS.Hosts
	if len(hosts) == 0 {
		hosts = []string{configuration.Server.Name}
	}

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("cannot generate a key: %w", err)
	}

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return fmt.Errorf("cannot generate a serial number: %w", err)
	}

	now := time.Now()
	template := x509.Certificate{
		SerialNumber:          serialNumber,
		Subject:               pkix.Name{CommonName: hosts[0], Organization: []string{"TeaNode development"}},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(command.Duration("lifetime")),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	for _, host := range hosts {
		if address := net.ParseIP(host); address != nil {
			template.IPAddresses = append(template.IPAddresses, address)
		} else {
			template.DNSNames = append(template.DNSNames, host)
		}
	}
	// A development server is reached on the loopback address as often as by
	// name, so make the certificate cover both.
	template.DNSNames = append(template.DNSNames, "localhost")
	template.IPAddresses = append(template.IPAddresses, net.ParseIP("127.0.0.1"), net.ParseIP("::1"))

	encoded, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return fmt.Errorf("cannot create the certificate: %w", err)
	}

	certificateFile := command.String("certificate-file")
	privateKeyFile := command.String("private-key-file")

	if err := writePem(configuration.Path(certificateFile), "CERTIFICATE", encoded, 0o644); err != nil {
		return err
	}
	if err := writePem(configuration.Path(privateKeyFile), "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(privateKey), 0o600); err != nil {
		return err
	}

	// Point the configuration at what was just written and turn ACME off,
	// since asking a certificate authority for a name it cannot reach only
	// produces noise in the log.
	if err := store.Update(func(configuration *config.Configuration) error {
		configuration.TLS.CertificateFile = certificateFile
		configuration.TLS.PrivateKeyFile = privateKeyFile
		configuration.TLS.ACME.Enabled = false
		return nil
	}); err != nil {
		return err
	}

	fmt.Printf("wrote %s and %s\n", configuration.Path(certificateFile), configuration.Path(privateKeyFile))
	fmt.Printf("valid for %v until %s\n\n", template.DNSNames, template.NotAfter.Format(time.RFC1123))
	fmt.Printf("%s now points at them and ACME is disabled.\n", store.Filename())
	fmt.Printf("Nothing will trust this certificate. Do not use it in production.\n")
	return nil
}

func writePem(filename, blockType string, content []byte, mode os.FileMode) error {
	file, err := atomicfile.Create(filename)
	if err != nil {
		return fmt.Errorf("cannot write %s: %w", filename, err)
	}
	defer func() {
		_ = atomicfile.Discard(file)
	}()
	if err := file.Chmod(mode); err != nil {
		return fmt.Errorf("cannot write %s: %w", filename, err)
	}
	if err := pem.Encode(file, &pem.Block{Type: blockType, Bytes: content}); err != nil {
		return fmt.Errorf("cannot write %s: %w", filename, err)
	}
	if err := atomicfile.Commit(file); err != nil {
		return fmt.Errorf("cannot write %s: %w", filename, err)
	}
	return nil
}
