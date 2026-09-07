package models

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"regexp/syntax"
	"strings"
)

// ValidationError is one thing wrong with a row, named by the field it is
// wrong in, so that a form can put the message beside the field.
type ValidationError struct {
	Path    string
	Message string
}

func (self *ValidationError) Error() string {
	if self.Path == "" {
		return self.Message
	}
	return self.Path + ": " + self.Message
}

// ValidationErrors is every problem found at once, so that fixing a row is
// not a game of whack-a-mole.
type ValidationErrors []*ValidationError

func (self ValidationErrors) Error() string {
	messages := make([]string, 0, len(self))
	for _, err := range self {
		messages = append(messages, err.Error())
	}
	return strings.Join(messages, "; ")
}

// ErrOrNil is the error to return: nil when nothing was found.
func (self ValidationErrors) ErrOrNil() error {
	if len(self) == 0 {
		return nil
	}
	return self
}

func (self *ValidationErrors) add(path, format string, arguments ...any) {
	*self = append(*self, &ValidationError{Path: path, Message: fmt.Sprintf(format, arguments...)})
}

var (
	hostLabelPattern = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)
	emailPattern     = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
)

// IsHostLabel reports whether a value is one label of a host name.
func IsHostLabel(value string) bool {
	return hostLabelPattern.MatchString(value)
}

// IsHostname reports whether a value is a fully qualified host name: the
// names this server publishes and obtains certificates for.
func IsHostname(value string) bool {
	if value == "" || len(value) > 253 {
		return false
	}
	labels := strings.Split(strings.TrimSuffix(value, "."), ".")
	if len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if !IsHostLabel(label) {
			return false
		}
	}
	return true
}

// IsRelayHost reports whether a value names something this server can open
// an SMTP connection to.
//
// Looser than IsHostname on purpose. A relay target is reached over whatever
// network this server is on, not looked up in public DNS: an address, a
// single-label name from a container network or a search domain, or a fully
// qualified name are all ordinary.
func IsRelayHost(value string) bool {
	if value == "" || len(value) > 253 {
		return false
	}
	if net.ParseIP(value) != nil {
		return true
	}
	for _, label := range strings.Split(strings.TrimSuffix(value, "."), ".") {
		if !IsHostLabel(label) {
			return false
		}
	}
	return true
}

// IsEmailAddress reports whether a value looks like one address.
func IsEmailAddress(value string) bool {
	return emailPattern.MatchString(value)
}

// RegexpErrorMessage strips the "error parsing regexp: " prefix that
// regexp.Compile adds, because the surrounding message already says that.
func RegexpErrorMessage(err error) string {
	var parseError *syntax.Error
	if errors.As(err, &parseError) {
		return fmt.Sprintf("%s in %s", parseError.Code, parseError.Expr)
	}
	return err.Error()
}
