package client

import (
	"os"
	"strings"
	"time"
)

// localZoneName is the IANA name of this machine's zone, as far as it can
// be told: TZ when set, else /etc/timezone, else the name the zone database
// gave the local zone if it is a real one. Empty when nothing says.
func localZoneName() string {
	if zone := strings.TrimSpace(os.Getenv("TZ")); zone != "" && !strings.HasPrefix(zone, ":") {
		if _, err := time.LoadLocation(zone); err == nil {
			return zone
		}
	}
	if content, err := os.ReadFile("/etc/timezone"); err == nil {
		if zone := strings.TrimSpace(string(content)); zone != "" {
			if _, err := time.LoadLocation(zone); err == nil {
				return zone
			}
		}
	}
	if target, err := os.Readlink("/etc/localtime"); err == nil {
		if index := strings.Index(target, "zoneinfo/"); index >= 0 {
			zone := target[index+len("zoneinfo/"):]
			if _, err := time.LoadLocation(zone); err == nil {
				return zone
			}
		}
	}
	return ""
}

// localLanguage is the language the shell speaks, from LANGUAGE, LC_ALL or
// LANG, reduced to a tag like "de" or "pt-BR". Empty when nothing says.
func localLanguage() string {
	for _, variable := range []string{"LANGUAGE", "LC_ALL", "LC_MESSAGES", "LANG"} {
		value := strings.TrimSpace(os.Getenv(variable))
		if value == "" || value == "C" || value == "POSIX" {
			continue
		}
		value, _, _ = strings.Cut(value, ".")
		value, _, _ = strings.Cut(value, "@")
		value, _, _ = strings.Cut(value, ":")
		value = strings.ReplaceAll(value, "_", "-")
		if value == "C" || value == "POSIX" || value == "" {
			continue
		}
		return value
	}
	return ""
}
