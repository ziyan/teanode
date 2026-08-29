package security

import (
	"errors"

	"golang.org/x/crypto/bcrypt"
)

// PasswordCost is deliberately above the bcrypt default. A login happens
// rarely, so spending a fraction of a second on it costs nothing, while it
// makes an offline attack on a leaked configuration file costlier.
const PasswordCost = 12

// HashPassword returns a bcrypt hash of a password.
func HashPassword(password string) ([]byte, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), PasswordCost)
	if err != nil {
		return nil, err
	}
	return hash, nil
}

// VerifyPassword reports whether a password matches a hash. A mismatch is not
// an error, so that a caller cannot accidentally treat one as a success.
func VerifyPassword(hash []byte, password string) (bool, error) {
	err := bcrypt.CompareHashAndPassword(hash, []byte(password))
	if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
