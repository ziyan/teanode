package config

import (
	"fmt"
	"reflect"

	"github.com/ziyan/teanode/internal/util/secretbox"
)

// Every secret in the settings -- provider keys, the session key, the TLS
// and ACME keys, the storage keys, an identity provider's client secret --
// is sealed before it is stored and opened when it is read, with the server
// secret and the same box the domain table's keys use. A database dump then
// holds ciphertext where it would have held keys, and once the server secret
// is kept in a file (--secret-file), nothing in the dump opens.
//
// The server secret itself cannot be sealed with itself: it is stored as it
// is, or kept out of the database altogether.

// agentSecretLabel binds the agent section's box to its use; a value sealed
// for the domain table does not open here, nor the other way round. The
// agent section was sealed first, under its own label, and keeps it.
const agentSecretLabel = "teanode configuration: agent secrets"

// settingsSecretLabel is the box for every other section's secrets.
const settingsSecretLabel = "teanode configuration: settings secrets"

// secretBoxes are the boxes for the configuration's secrets, by section, or
// nil when there is no server secret to derive them from.
func secretBoxes(configuration *Configuration) (map[string]*secretbox.Box, error) {
	secret := configuration.Secret()
	if len(secret) == 0 {
		return nil, nil
	}
	agentBox, err := secretbox.New(secret, agentSecretLabel)
	if err != nil {
		return nil, err
	}
	settingsBox, err := secretbox.New(secret, settingsSecretLabel)
	if err != nil {
		return nil, err
	}
	boxes := map[string]*secretbox.Box{}
	for key := range configuration.sections() {
		switch key {
		case settingServer:
			// Only the server secret, which cannot seal itself.
		case settingAgent:
			boxes[key] = agentBox
		default:
			boxes[key] = settingsBox
		}
	}
	return boxes, nil
}

// sealSecrets seals every secret in the settings that is not sealed
// already. Without a server secret the values stay as they are.
func sealSecrets(configuration *Configuration) error {
	boxes, err := secretBoxes(configuration)
	if err != nil || boxes == nil {
		return err
	}
	for key, section := range configuration.sections() {
		box := boxes[key]
		if box == nil {
			continue
		}
		if err := mapSecrets(reflect.ValueOf(section), func(value string) (string, error) {
			if value == "" || secretbox.Sealed(value) {
				return value, nil
			}
			return box.Seal([]byte(value))
		}); err != nil {
			return err
		}
	}
	return nil
}

// openSecrets opens every sealed secret in the settings. A value that was
// never sealed -- written before its section was -- passes through.
func openSecrets(configuration *Configuration) error {
	boxes, err := secretBoxes(configuration)
	if err != nil {
		return err
	}
	for key, section := range configuration.sections() {
		if key == settingServer {
			continue
		}
		box := boxes[key]
		if err := mapSecrets(reflect.ValueOf(section), func(value string) (string, error) {
			if !secretbox.Sealed(value) {
				return value, nil
			}
			if box == nil {
				return "", fmt.Errorf("config: a %s secret is sealed but the server has no secret to open it with", key)
			}
			opened, err := box.Open(value)
			if err != nil {
				return "", fmt.Errorf("config: cannot open a %s secret: %w", key, err)
			}
			return string(opened), nil
		}); err != nil {
			return err
		}
	}
	return nil
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
