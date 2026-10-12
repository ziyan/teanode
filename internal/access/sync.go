package access

import (
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/security"
	"strings"
	"time"
)

// AuthenticateSyncCredential verifies a scoped sync password, never an account password.
func AuthenticateSyncCredential(transaction db.Transaction, username, password string) (*models.SyncCredential, error) {
	parts := strings.SplitN(password, "-", 3)
	refuse := func() (*models.SyncCredential, error) {
		_, _ = security.VerifyPassword(dummyPasswordHash(), password)
		return nil, ErrInvalidAppPassword
	}
	if len(parts) != 3 || parts[0] != "sync" {
		return refuse()
	}
	credential, err := transaction.GetSyncCredential(parts[1])
	if err != nil {
		return nil, err
	}
	if credential == nil {
		return refuse()
	}
	user, err := transaction.GetUser(credential.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil || user.Disabled() || user.Username != strings.TrimSpace(username) {
		return refuse()
	}
	isVerified, err := security.VerifyPassword([]byte(credential.PasswordHash), password)
	if err != nil {
		return nil, err
	}
	if !isVerified {
		return nil, ErrInvalidAppPassword
	}
	if err := transaction.TouchSyncCredential(credential.ID, time.Now()); err != nil {
		return nil, err
	}
	return credential, nil
}
