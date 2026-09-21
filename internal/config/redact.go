package config

import (
	"reflect"
)

// Redacted is what a secret is replaced with. It is a fixed string rather than
// an empty one so that the reader can tell "there is a secret here, and you
// are not being shown it" from "this is not configured".
const Redacted = "(redacted)"

// Redact replaces every secret in a copy of the configuration and returns it,
// leaving the original untouched.
//
// A field is a secret when it carries `secret:"true"`. Tagging is what makes
// this reliable: redaction is one function that cannot forget a field, rather
// than a list at every place a configuration is shown, and a new secret is
// redacted everywhere the moment it is tagged. TestEverySecretIsTagged
// enforces the tagging itself.
//
// Use it before showing configuration to anybody — a GraphQL reply, a support
// bundle, a log line. Not for writing the file back: Store writes the real
// configuration.
func (self *Configuration) Redact() (*Configuration, error) {
	duplicate, err := clone(self)
	if err != nil {
		return nil, err
	}
	redactValue(reflect.ValueOf(duplicate))
	return duplicate, nil
}

// redactValue walks a value and blanks every string tagged as a secret.
func redactValue(value reflect.Value) {
	switch value.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !value.IsNil() {
			redactValue(value.Elem())
		}
	case reflect.Slice, reflect.Array:
		for index := 0; index < value.Len(); index++ {
			redactValue(value.Index(index))
		}
	case reflect.Map:
		// Map values are not addressable, so a secret inside one could not be
		// replaced in place. There are none today; the test below fails if
		// one is ever added.
		for _, key := range value.MapKeys() {
			redactValue(value.MapIndex(key))
		}
	case reflect.Struct:
		for index := 0; index < value.NumField(); index++ {
			field := value.Type().Field(index)
			if !field.IsExported() {
				continue
			}
			target := value.Field(index)
			if IsSecretField(field) && target.Kind() == reflect.String {
				if target.String() != "" && target.CanSet() {
					target.SetString(Redacted)
				}
				continue
			}
			redactValue(target)
		}
	}
}

// IsSecretField reports whether a struct field holds a secret.
func IsSecretField(field reflect.StructField) bool {
	return field.Tag.Get("secret") == "true"
}
