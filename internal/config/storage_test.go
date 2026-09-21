package config_test

import (
	"testing"

	"github.com/ziyan/teanode/internal/config"
)

func TestStorageModesPreserveDefaultsAndRequireSharedDurability(test *testing.T) {
	for _, fixture := range []struct {
		mode        string
		directory   string
		isS3Enabled bool
		isValid     bool
	}{
		{"", "mail", false, true},
		{"", "", true, true},
		{"local", "mail", false, true},
		{"local", "mail", true, true},
		{"local", "", true, false},
		{"shared", "", true, true},
		{"shared", "mail", true, false},
		{"shared", "", false, false},
		{"unknown", "mail", false, false},
	} {
		configuration := config.Example()
		configuration.Server.DataDirectory = test.TempDir()
		configuration.Storage.Mode = fixture.mode
		configuration.Storage.Directory = fixture.directory
		configuration.Storage.S3.Enabled = fixture.isS3Enabled
		configuration.Storage.S3.Bucket = "fixture-bucket"
		if err := configuration.Validate(); (err == nil) != fixture.isValid {
			test.Errorf("mode %q, directory %q, S3 %t: %v", fixture.mode, fixture.directory, fixture.isS3Enabled, err)
		}
	}
}
