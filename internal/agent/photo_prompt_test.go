package agent

import (
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/models"
)

// The prompt tells the agent there is a photo library only when the server
// offers one and the person may use it: a model told of a library it cannot
// reach spends a turn being refused, and one not told never looks.
func TestThePromptMentionsThePhotoLibraryOnlyWhereItIsOfferedAndAllowed(test *testing.T) {
	test.Parallel()

	on, off := config.Default(), config.Default()
	on.Services.Photos.IsEnabled = new(true)
	off.Services.Photos.IsEnabled = new(false)
	allowed := &models.EffectivePermissions{Everywhere: []models.Permission{models.PermissionPhotosUse}}
	notAllowed := &models.EffectivePermissions{Everywhere: []models.Permission{models.PermissionCalendarUse}}

	if line := photoLibraryLine(on, allowed); !strings.Contains(line, "photo library") || !strings.Contains(line, "photo tool") || !strings.HasPrefix(line, "- ") {
		test.Errorf("the line is %q", line)
	}
	if line := photoLibraryLine(off, allowed); line != "" {
		test.Errorf("a server without photos told the agent of a library: %q", line)
	}
	if line := photoLibraryLine(on, notAllowed); line != "" {
		test.Errorf("a person without the permission was told of a library: %q", line)
	}
	if line := photoLibraryLine(on, nil); line != "" {
		test.Errorf("a run with no permissions was told of a library: %q", line)
	}
}
