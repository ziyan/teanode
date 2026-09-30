package config_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db/dbtest"
)

// A server whose secret is in the database, started with a file holding the
// same secret, removes it from the database and keeps running on the file's;
// the next start without the file is refused rather than given a new secret.
func TestOpeningWithASecretFileMovesTheSecretOut(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}

	_, seed := writeValidConfiguration(t, nil)
	seed.Server.Secret = strings.Repeat("m", 40)
	seed.Session.Key = strings.Repeat("n", 40)
	if _, err := config.Initialize(database, func() (*config.Configuration, error) { return seed, nil }, nil); err != nil {
		t.Fatal(err)
	}

	secretFilePath := filepath.Join(t.TempDir(), "server.secret")
	if err := config.WriteSecretFile(secretFilePath, []byte(seed.Server.Secret)); err != nil {
		t.Fatal(err)
	}
	secretFile, err := config.ReadSecretFile(secretFilePath)
	if err != nil {
		t.Fatal(err)
	}
	store, err := config.OpenStore(database, seed.Database, secretFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(store.Current().Secret()) != seed.Server.Secret {
		t.Fatal("the store does not hand out the file's secret")
	}
	_ = store.Close()

	rows, err := database.LoadConfiguration()
	if err != nil {
		t.Fatal(err)
	}
	for key, stored := range rows.Settings {
		if strings.Contains(stored, seed.Server.Secret) {
			t.Fatalf("the %s row still holds the secret", key)
		}
	}
	if stored, err := config.LoadStoredSecret(database); err != nil || len(stored) != 0 {
		t.Fatalf("the database still reads as holding a secret: %v", err)
	}

	if _, err := config.OpenStore(database, seed.Database, nil); err == nil {
		t.Fatal("a database whose secret is in a file was opened without it")
	}
}
