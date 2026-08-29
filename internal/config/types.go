package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration is a time.Duration that reads and writes as a human string in
// YAML, for example "30s", "5m", "12h" or "30d". Go's own duration syntax has
// no day unit, so "d" is added here because configuration talks in days.
type Duration time.Duration

// Duration returns the value as a time.Duration.
func (self Duration) Duration() time.Duration {
	return time.Duration(self)
}

// String formats the duration, preferring whole days where they divide evenly.
func (self Duration) String() string {
	duration := time.Duration(self)
	if duration == 0 {
		return "0s"
	}
	// Prefer the largest whole unit, so that a 30 day retention reads back as
	// "30d" rather than time.Duration's "720h0m0s".
	switch {
	case duration%(24*time.Hour) == 0:
		return fmt.Sprintf("%dd", duration/(24*time.Hour))
	case duration%time.Hour == 0:
		return fmt.Sprintf("%dh", duration/time.Hour)
	case duration%time.Minute == 0:
		return fmt.Sprintf("%dm", duration/time.Minute)
	case duration%time.Second == 0:
		return fmt.Sprintf("%ds", duration/time.Second)
	}
	return duration.String()
}

// MarshalYAML implements yaml.Marshaler.
func (self Duration) MarshalYAML() (interface{}, error) {
	return self.String(), nil
}

// UnmarshalYAML implements yaml.Unmarshaler. Both "30d" and a bare number of
// seconds are accepted, the latter because it is a natural thing to write.
func (self *Duration) UnmarshalYAML(node *yaml.Node) error {
	var value string
	if err := node.Decode(&value); err != nil {
		var seconds float64
		if err := node.Decode(&seconds); err != nil {
			return fmt.Errorf("config: %q is not a duration", node.Value)
		}
		*self = Duration(time.Duration(seconds * float64(time.Second)))
		return nil
	}
	parsed, err := ParseDuration(value)
	if err != nil {
		return err
	}
	*self = parsed
	return nil
}

// ParseDuration parses a duration, accepting the "d" day unit in addition to
// everything time.ParseDuration understands.
func ParseDuration(value string) (Duration, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, nil
	}
	if strings.HasSuffix(trimmed, "d") {
		days, err := strconv.ParseFloat(strings.TrimSuffix(trimmed, "d"), 64)
		if err == nil {
			return Duration(time.Duration(days * float64(24*time.Hour))), nil
		}
	}
	parsed, err := time.ParseDuration(trimmed)
	if err != nil {
		return 0, fmt.Errorf("config: %q is not a duration, expected something like 30s, 5m, 12h or 30d", value)
	}
	return Duration(parsed), nil
}

// byteSizeUnits are ordered longest suffix first so that "MB" is matched
// before "B".
var byteSizeUnits = []struct {
	suffix string
	scale  uint64
}{
	{"GB", 1024 * 1024 * 1024},
	{"MB", 1024 * 1024},
	{"KB", 1024},
	{"G", 1024 * 1024 * 1024},
	{"M", 1024 * 1024},
	{"K", 1024},
	{"B", 1},
}

// ByteSize is a size in bytes that reads and writes as a human string in
// YAML, for example "70MB". Plain integers are bytes.
type ByteSize uint64

// Bytes returns the value in bytes.
func (self ByteSize) Bytes() uint64 {
	return uint64(self)
}

// String formats the size using the largest unit that divides it evenly.
func (self ByteSize) String() string {
	value := uint64(self)
	for _, unit := range byteSizeUnits {
		if len(unit.suffix) == 2 && value >= unit.scale && value%unit.scale == 0 {
			return fmt.Sprintf("%d%s", value/unit.scale, unit.suffix)
		}
	}
	return strconv.FormatUint(value, 10)
}

// MarshalYAML implements yaml.Marshaler.
func (self ByteSize) MarshalYAML() (interface{}, error) {
	return self.String(), nil
}

// UnmarshalYAML implements yaml.Unmarshaler.
func (self *ByteSize) UnmarshalYAML(node *yaml.Node) error {
	var value string
	if err := node.Decode(&value); err != nil {
		var bytes uint64
		if err := node.Decode(&bytes); err != nil {
			return fmt.Errorf("config: %q is not a size", node.Value)
		}
		*self = ByteSize(bytes)
		return nil
	}
	parsed, err := ParseByteSize(value)
	if err != nil {
		return err
	}
	*self = parsed
	return nil
}

// ParseByteSize parses a size such as "70MB", "1G" or "1048576".
func ParseByteSize(value string) (ByteSize, error) {
	trimmed := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(value), " ", ""))
	if trimmed == "" {
		return 0, nil
	}
	for _, unit := range byteSizeUnits {
		if !strings.HasSuffix(trimmed, unit.suffix) {
			continue
		}
		number, err := strconv.ParseFloat(strings.TrimSuffix(trimmed, unit.suffix), 64)
		if err != nil {
			continue
		}
		return ByteSize(uint64(number * float64(unit.scale))), nil
	}
	bytes, err := strconv.ParseUint(trimmed, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("config: %q is not a size, expected something like 70MB or a number of bytes", value)
	}
	return ByteSize(bytes), nil
}
