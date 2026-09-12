// Package skills is how tools arrive without a release: a skill is a file
// of declarations, fetched from a registry that signs what it publishes,
// read as data and carried out by the interpreter here. Nothing in a skill
// is code this server runs, and a skill's commands never run on this
// server at all -- they are carried to the computer the person attached.
package skills

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/util/safefetch"
)

// OfficialIndex is the registry this server knows, and publicKeyPEM the
// half of its signing key that checks what it publishes. The key is built
// in rather than fetched: a key taken from the same place as the thing it
// vouches for proves nothing, since whoever can serve one can serve both.
const OfficialIndex = "https://raw.githubusercontent.com/teanode/teanode-skills/main/index.json"

//go:embed keys/teanode-skills-ed25519-public.pem
var publicKeyPEM []byte

const (
	// indexBytes and skillBytes are the most that is read of either, and
	// fetchTimeout how long either is waited for.
	indexBytes   = 1 << 20
	skillBytes   = 1 << 20
	fetchTimeout = 30 * time.Second

	// mostSteps is the longest a workflow may be.
	mostSteps = 10
)

// Index is what a registry publishes about everything it has.
type Index struct {
	Publisher string   `json:"publisher"`
	Skills    []*Entry `json:"skills"`
}

// Entry is one skill in the index: where it is, what it should hash to,
// and the publisher's signature over those.
type Entry struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Version     string   `json:"version"`
	URL         string   `json:"url"`
	SHA256      string   `json:"sha256"`
	Signature   string   `json:"signature"`
	Tags        []string `json:"tags"`
}

// Registry reads one registry. The zero value reads the official one.
type Registry struct {
	// Address is the index to read; empty means the official one.
	Address string

	// PublicKey checks the signatures; nil means the built-in one.
	PublicKey ed25519.PublicKey

	// Client is the HTTP client; nil means one with the fetch timeout.
	Client *http.Client
}

func (self *Registry) address() string {
	if strings.TrimSpace(self.Address) != "" {
		return self.Address
	}
	return OfficialIndex
}

// client fetches the index and the files. It is the guarded one the rest
// of this server fetches through, so a registry that redirects into the
// network this server sits in is refused rather than followed.
func (self *Registry) client() *http.Client {
	if self.Client != nil {
		return self.Client
	}
	guarded := safefetch.Client()
	guarded.Timeout = fetchTimeout
	return guarded
}

// key is the public half that checks a signature: the one given, or the
// one built in.
func (self *Registry) key() (ed25519.PublicKey, error) {
	if len(self.PublicKey) > 0 {
		return self.PublicKey, nil
	}
	return BuiltinKey()
}

// BuiltinKey is the official registry's signing key, as shipped.
func BuiltinKey() (ed25519.PublicKey, error) {
	block, _ := pem.Decode(publicKeyPEM)
	if block == nil || block.Type != "PUBLIC KEY" {
		return nil, fmt.Errorf("skills: the built-in key is not a public key in PEM")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("skills: the built-in key cannot be read: %w", err)
	}
	key, ok := parsed.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("skills: the built-in key is %T, not an Ed25519 key", parsed)
	}
	return key, nil
}

// Index fetches what the registry publishes. Nothing is trusted yet: every
// entry is still checked against the key before it is used.
func (self *Registry) Index(ctx context.Context) (*Index, error) {
	body, err := self.fetch(ctx, self.address(), indexBytes)
	if err != nil {
		return nil, err
	}
	var index Index
	if err := json.Unmarshal(body, &index); err != nil {
		return nil, fmt.Errorf("skills: %s is not an index: %w", self.address(), err)
	}
	return &index, nil
}

// signedMessage is what the publisher signs for one entry. The shape is
// the registry's, and the three newlines are part of it.
func signedMessage(entry *Entry) string {
	return entry.Name + "\n" + entry.Version + "\n" + entry.URL + "\n" + strings.ToLower(entry.SHA256)
}

// Verify says whether an entry is the publisher's, by its signature over
// the name, the version, the address and the hash together. Signing all
// four is what stops an entry being pointed at another file, or another
// version being passed off as this one.
func (self *Registry) Verify(entry *Entry) error {
	if entry == nil {
		return fmt.Errorf("skills: there is no entry to check")
	}
	key, err := self.key()
	if err != nil {
		return err
	}
	signature, err := base64.StdEncoding.DecodeString(strings.TrimSpace(entry.Signature))
	if err != nil {
		return fmt.Errorf("skills: the signature of %s is not base64: %w", entry.Name, err)
	}
	if len(signature) != ed25519.SignatureSize {
		return fmt.Errorf("skills: the signature of %s is %d bytes, not %d", entry.Name, len(signature), ed25519.SignatureSize)
	}
	if !ed25519.Verify(key, []byte(signedMessage(entry)), signature) {
		return fmt.Errorf("skills: the signature of %s is not this registry's; it is refused", entry.Name)
	}
	return nil
}

// Download fetches an entry's file and checks it against the hash the
// entry carries, which the signature has already vouched for. The bytes
// are returned only when both hold.
func (self *Registry) Download(ctx context.Context, entry *Entry) ([]byte, error) {
	if err := self.Verify(entry); err != nil {
		return nil, err
	}
	body, err := self.fetch(ctx, entry.URL, skillBytes)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(body)
	got := hex.EncodeToString(sum[:])
	if want := strings.ToLower(strings.TrimSpace(entry.SHA256)); got != want {
		return nil, fmt.Errorf("skills: %s is not what the registry signed for: it hashes to %s, and %s was signed", entry.Name, got, want)
	}
	return body, nil
}

// Publisher is who the registry says it is. It is not proof of anything
// on its own -- the signatures are -- but it is what an operator sees
// beside an installed skill.
func (self *Registry) Publisher() string {
	if strings.TrimSpace(self.Address) != "" {
		return self.Address
	}
	return "github.com/teanode/teanode-skills"
}

// Newer says whether the first version is later than the second, read as
// numbers separated by dots. Anything that is not a number sorts as zero,
// so a version nobody can read never looks newer than one that can.
func Newer(candidate, installed string) bool {
	left := strings.Split(strings.TrimSpace(candidate), ".")
	right := strings.Split(strings.TrimSpace(installed), ".")
	for index := 0; index < len(left) || index < len(right); index++ {
		if numberAt(left, index) != numberAt(right, index) {
			return numberAt(left, index) > numberAt(right, index)
		}
	}
	return false
}

func numberAt(parts []string, index int) int {
	if index >= len(parts) {
		return 0
	}
	value, err := strconv.Atoi(strings.TrimSpace(parts[index]))
	if err != nil {
		return 0
	}
	return value
}

// Find is one entry of the index by name, already checked.
func (self *Registry) Find(ctx context.Context, name string) (*Entry, error) {
	index, err := self.Index(ctx)
	if err != nil {
		return nil, err
	}
	for _, entry := range index.Skills {
		if strings.EqualFold(entry.Name, name) {
			if err := self.Verify(entry); err != nil {
				return nil, err
			}
			return entry, nil
		}
	}
	return nil, fmt.Errorf("skills: the registry has no skill called %q", name)
}

func (self *Registry) fetch(ctx context.Context, address string, most int64) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json, text/plain, */*")
	response, err := self.client().Do(request)
	if err != nil {
		return nil, fmt.Errorf("skills: cannot reach %s: %w", address, err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("skills: %s answered %d", address, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, most))
	if err != nil {
		return nil, fmt.Errorf("skills: cannot read %s: %w", address, err)
	}
	return body, nil
}
