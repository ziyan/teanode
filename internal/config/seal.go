package config

import (
	"fmt"
	"reflect"

	"github.com/ziyan/teanode/internal/util/secretbox"
)

// The agent section's secrets — provider keys, the search key, what a
// connected server is reached with — are sealed before they are stored and
// opened when they are read, with the server secret and the same box the
// domain table's keys use. A database dump then holds ciphertext where it
// would have held keys that spend money.
//
// The server secret itself cannot be sealed with itself, and the other
// sections predate this and are left as they were.

// agentSecretLabel binds the box to this use; a value sealed for the
// domain table does not open here, nor the other way round.
const agentSecretLabel = "teanode configuration: agent secrets"

// agentSecretBox is the box for the configuration's secret, or nil when
// there is no secret to derive one from.
func agentSecretBox(configuration *Configuration) (*secretbox.Box, error) {
	secret := configuration.Secret()
	if len(secret) == 0 {
		return nil, nil
	}
	return secretbox.New(secret, agentSecretLabel)
}

// sealAgentSecrets seals every secret in the agent section that is not
// sealed already. Without a server secret the values stay as they are.
func sealAgentSecrets(configuration *Configuration) error {
	box, err := agentSecretBox(configuration)
	if err != nil {
		return err
	}
	if box == nil {
		return nil
	}
	return mapSecrets(reflect.ValueOf(&configuration.Agent), func(value string) (string, error) {
		if value == "" || secretbox.Sealed(value) {
			return value, nil
		}
		return box.Seal([]byte(value))
	})
}

// openAgentSecrets opens every sealed secret in the agent section. A value
// that was never sealed — written before this existed — passes through.
func openAgentSecrets(configuration *Configuration) error {
	box, err := agentSecretBox(configuration)
	if err != nil {
		return err
	}
	return mapSecrets(reflect.ValueOf(&configuration.Agent), func(value string) (string, error) {
		if !secretbox.Sealed(value) {
			return value, nil
		}
		if box == nil {
			return "", fmt.Errorf("config: an agent secret is sealed but the server has no secret to open it with")
		}
		opened, err := box.Open(value)
		if err != nil {
			return "", fmt.Errorf("config: cannot open an agent secret: %w", err)
		}
		return string(opened), nil
	})
}

// mapSecrets walks a value the way Redact does and rewrites every string
// tagged as a secret with the transform.
func mapSecrets(value reflect.Value, transform func(string) (string, error)) error {
	switch value.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !value.IsNil() {
			return mapSecrets(value.Elem(), transform)
		}
	case reflect.Slice, reflect.Array:
		for index := 0; index < value.Len(); index++ {
			if err := mapSecrets(value.Index(index), transform); err != nil {
				return err
			}
		}
	case reflect.Struct:
		for index := 0; index < value.NumField(); index++ {
			field := value.Type().Field(index)
			if !field.IsExported() {
				continue
			}
			target := value.Field(index)
			if IsSecretField(field) && target.Kind() == reflect.String {
				if !target.CanSet() {
					return fmt.Errorf("config: the secret %s cannot be rewritten", field.Name)
				}
				rewritten, err := transform(target.String())
				if err != nil {
					return err
				}
				target.SetString(rewritten)
				continue
			}
			if err := mapSecrets(target, transform); err != nil {
				return err
			}
		}
	}
	return nil
}
