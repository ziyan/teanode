package skills

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// signedRegistry is a registry of one skill, signed with a key the test
// made, so that the happy path is provable without the network.
func signedRegistry(t *testing.T, content string, damage func(*Entry)) (*Registry, *Entry, func()) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	sum := sha256.Sum256([]byte(content))
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.HasSuffix(request.URL.Path, "/skill.md") {
			_, _ = writer.Write([]byte(content))
			return
		}
		writer.WriteHeader(http.StatusNotFound)
	}))
	entry := &Entry{
		Name: "weather", Description: "the weather", Version: "2.0.0",
		URL: server.URL + "/skill.md", SHA256: hex.EncodeToString(sum[:]),
	}
	entry.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(private, []byte(signedMessage(entry))))
	if damage != nil {
		damage(entry)
	}
	index, _ := json.Marshal(&Index{Publisher: "test", Skills: []*Entry{entry}})
	indexServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write(index)
	}))
	registry := &Registry{Address: indexServer.URL, PublicKey: public}
	return registry, entry, func() { server.Close(); indexServer.Close() }
}

func TestAnEntryTheRegistrySignedIsTaken(t *testing.T) {
	registry, entry, done := signedRegistry(t, "---\nname: weather\n---\n", nil)
	defer done()

	index, err := registry.Index(context.Background())
	if err != nil || len(index.Skills) != 1 {
		t.Fatalf("index: %v %v", index, err)
	}
	if err := registry.Verify(entry); err != nil {
		t.Fatalf("the entry is this registry's: %v", err)
	}
	content, err := registry.Download(context.Background(), entry)
	if err != nil || !strings.Contains(string(content), "name: weather") {
		t.Fatalf("download: %q %v", content, err)
	}
	found, err := registry.Find(context.Background(), "WEATHER")
	if err != nil || found.Version != "2.0.0" {
		t.Fatalf("found by name, whatever the case: %v %v", found, err)
	}
	if _, err := registry.Find(context.Background(), "absent"); err == nil {
		t.Fatal("a skill the registry does not have is said so")
	}
}

// Each of the four things the signature covers is changed in turn. Every
// one of them must make the entry refused, because each is a way of
// passing off a different file, or a different version of this one.
func TestChangingWhatWasSignedRefusesTheEntry(t *testing.T) {
	for name, damage := range map[string]func(*Entry){
		"the name":      func(entry *Entry) { entry.Name = "weather2" },
		"the version":   func(entry *Entry) { entry.Version = "9.9.9" },
		"the address":   func(entry *Entry) { entry.URL = "https://elsewhere.example/skill.md" },
		"the hash":      func(entry *Entry) { entry.SHA256 = strings.Repeat("0", 64) },
		"the signature": func(entry *Entry) { entry.Signature = base64.StdEncoding.EncodeToString(make([]byte, 64)) },
	} {
		registry, entry, done := signedRegistry(t, "---\nname: weather\n---\n", damage)
		err := registry.Verify(entry)
		if err == nil || !strings.Contains(err.Error(), "refused") {
			t.Errorf("%s changed: want a refusal, got %v", name, err)
		}
		done()
	}
}

// A signature that is not base64, or is the wrong length, is refused
// before the key is ever asked about it.
func TestAMalformedSignatureIsRefused(t *testing.T) {
	registry, entry, done := signedRegistry(t, "x", func(entry *Entry) { entry.Signature = "not base64!!" })
	defer done()
	if err := registry.Verify(entry); err == nil || !strings.Contains(err.Error(), "base64") {
		t.Fatalf("want a base64 complaint, got %v", err)
	}
	registry2, entry2, done2 := signedRegistry(t, "x", func(entry *Entry) {
		entry.Signature = base64.StdEncoding.EncodeToString([]byte("short"))
	})
	defer done2()
	if err := registry2.Verify(entry2); err == nil || !strings.Contains(err.Error(), "bytes") {
		t.Fatalf("want a length complaint, got %v", err)
	}
	_ = registry
}

// The file is checked against the hash even though the signature held,
// because the signature covers the hash and not the bytes: a registry that
// serves a different file at the same address is caught here.
func TestAFileThatIsNotWhatWasSignedForIsRefused(t *testing.T) {
	public, private, _ := ed25519.GenerateKey(nil)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte("something else entirely"))
	}))
	defer server.Close()
	sum := sha256.Sum256([]byte("what was promised"))
	entry := &Entry{Name: "weather", Version: "1.0.0", URL: server.URL + "/skill.md", SHA256: hex.EncodeToString(sum[:])}
	entry.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(private, []byte(signedMessage(entry))))
	registry := &Registry{PublicKey: public}
	if _, err := registry.Download(context.Background(), entry); err == nil || !strings.Contains(err.Error(), "hashes to") {
		t.Fatalf("want a hash complaint naming both, got %v", err)
	}
}

// The key that ships with this server is a real Ed25519 public key.
func TestTheBuiltinKeyIsAKey(t *testing.T) {
	key, err := BuiltinKey()
	if err != nil {
		t.Fatalf("the built-in key: %v", err)
	}
	if len(key) != ed25519.PublicKeySize {
		t.Fatalf("want %d bytes, got %d", ed25519.PublicKeySize, len(key))
	}
}
