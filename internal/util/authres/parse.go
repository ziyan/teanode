package authres

import (
	"errors"
	"strings"
	"unicode"
)

// ResultValue is an authentication result value, as defined in RFC 5451 section
// 6.3.
type ResultValue string

const (
	ResultNone      ResultValue = "none"
	ResultPass      ResultValue = "pass"
	ResultFail      ResultValue = "fail"
	ResultPolicy    ResultValue = "policy"
	ResultNeutral   ResultValue = "neutral"
	ResultTempError ResultValue = "temperror"
	ResultPermError ResultValue = "permerror"
	ResultHardFail  ResultValue = "hardfail"
	ResultSoftFail  ResultValue = "softfail"
)

// Result is an authentication result.
type Result interface {
	parse(value ResultValue, parameters map[string]string)
	format() (value ResultValue, parameters map[string]string)
}

type AuthResult struct {
	Value  ResultValue
	Reason string
	Auth   string
}

func (self *AuthResult) parse(value ResultValue, parameters map[string]string) {
	self.Value = value
	self.Reason = parameters["reason"]
	self.Auth = parameters["smtp.auth"]
}

func (self *AuthResult) format() (ResultValue, map[string]string) {
	return self.Value, map[string]string{"smtp.auth": self.Auth}
}

type DKIMResult struct {
	Value      ResultValue
	Reason     string
	Domain     string
	Identifier string
}

func (self *DKIMResult) parse(value ResultValue, parameters map[string]string) {
	self.Value = value
	self.Reason = parameters["reason"]
	self.Domain = parameters["header.d"]
	self.Identifier = parameters["header.i"]
}

func (self *DKIMResult) format() (ResultValue, map[string]string) {
	return self.Value, map[string]string{
		"reason":   self.Reason,
		"header.d": self.Domain,
		"header.i": self.Identifier,
	}
}

type DomainKeysResult struct {
	Value  ResultValue
	Reason string
	Domain string
	From   string
	Sender string
}

func (self *DomainKeysResult) parse(value ResultValue, parameters map[string]string) {
	self.Value = value
	self.Reason = parameters["reason"]
	self.Domain = parameters["header.d"]
	self.From = parameters["header.from"]
	self.Sender = parameters["header.sender"]
}

func (self *DomainKeysResult) format() (ResultValue, map[string]string) {
	return self.Value, map[string]string{
		"reason":        self.Reason,
		"header.d":      self.Domain,
		"header.from":   self.From,
		"header.sender": self.Sender,
	}
}

type IPRevResult struct {
	Value  ResultValue
	Reason string
	IP     string
}

func (self *IPRevResult) parse(value ResultValue, parameters map[string]string) {
	self.Value = value
	self.Reason = parameters["reason"]
	self.IP = parameters["policy.iprev"]
}

func (self *IPRevResult) format() (ResultValue, map[string]string) {
	return self.Value, map[string]string{
		"reason":       self.Reason,
		"policy.iprev": self.IP,
	}
}

type SenderIDResult struct {
	Value       ResultValue
	Reason      string
	HeaderKey   string
	HeaderValue string
}

func (self *SenderIDResult) parse(value ResultValue, parameters map[string]string) {
	self.Value = value
	self.Reason = parameters["reason"]

	for key, value := range parameters {
		if strings.HasPrefix(key, "header.") {
			self.HeaderKey = strings.TrimPrefix(key, "header.")
			self.HeaderValue = value
			break
		}
	}
}

func (self *SenderIDResult) format() (value ResultValue, parameters map[string]string) {
	return self.Value, map[string]string{
		"reason": self.Reason,
		"header." + strings.ToLower(self.HeaderKey): self.HeaderValue,
	}
}

type SPFResult struct {
	Value  ResultValue
	Reason string
	From   string
	Helo   string
}

func (self *SPFResult) parse(value ResultValue, parameters map[string]string) {
	self.Value = value
	self.Reason = parameters["reason"]
	self.From = parameters["smtp.mailfrom"]
	self.Helo = parameters["smtp.helo"]
}

func (self *SPFResult) format() (ResultValue, map[string]string) {
	return self.Value, map[string]string{
		"reason":        self.Reason,
		"smtp.mailfrom": self.From,
		"smtp.helo":     self.Helo,
	}
}

type DMARCResult struct {
	Value  ResultValue
	Reason string
	From   string
}

func (self *DMARCResult) parse(value ResultValue, parameters map[string]string) {
	self.Value = value
	self.Reason = parameters["reason"]
	self.From = parameters["header.from"]
}

func (self *DMARCResult) format() (ResultValue, map[string]string) {
	return self.Value, map[string]string{
		"reason":      self.Reason,
		"header.from": self.From,
	}
}

type ARCResult struct {
	Value  ResultValue
	Reason string
}

func (self *ARCResult) parse(value ResultValue, parameters map[string]string) {
	self.Value = value
	self.Reason = parameters["reason"]
}

func (self *ARCResult) format() (ResultValue, map[string]string) {
	return self.Value, map[string]string{
		"reason": self.Reason,
	}
}

type GenericResult struct {
	Method     string
	Value      ResultValue
	Parameters map[string]string
}

func (self *GenericResult) parse(value ResultValue, parameters map[string]string) {
	self.Value = value
	self.Parameters = parameters
}

func (self *GenericResult) format() (ResultValue, map[string]string) {
	return self.Value, self.Parameters
}

type newResultFunc func() Result

var results = map[string]newResultFunc{
	"auth": func() Result {
		return new(AuthResult)
	},
	"dkim": func() Result {
		return new(DKIMResult)
	},
	"domainkeys": func() Result {
		return new(DomainKeysResult)
	},
	"iprev": func() Result {
		return new(IPRevResult)
	},
	"sender-id": func() Result {
		return new(SenderIDResult)
	},
	"spf": func() Result {
		return new(SPFResult)
	},
	"dmarc": func() Result {
		return new(DMARCResult)
	},
	"arc": func() Result {
		return new(ARCResult)
	},
}

// withoutComments removes CFWS comments, which the grammar allows anywhere
// whitespace is allowed. Parentheses nest.
func withoutComments(value string) string {
	var out strings.Builder
	depth := 0
	for at := 0; at < len(value); at++ {
		switch value[at] {
		case '\\':
			if depth == 0 && at+1 < len(value) {
				out.WriteByte(value[at])
				at++
				out.WriteByte(value[at])
				continue
			}
			at++
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
				out.WriteByte(' ')
			}
		default:
			if depth == 0 {
				out.WriteByte(value[at])
			}
		}
	}
	return out.String()
}

// unquoted takes the quotes off a quoted-string identifier.
func unquoted(value string) string {
	if len(value) < 2 || value[0] != '"' || value[len(value)-1] != '"' {
		return value
	}
	inner := value[1 : len(value)-1]
	if strings.Contains(inner, `"`) {
		return value
	}
	return strings.ReplaceAll(inner, `\\`, "")
}

// Parse parses the provided Authentication-Results header field. It returns the
// authentication service identifier and authentication results.
func Parse(field string) (identifier string, results []Result, err error) {
	parts := strings.Split(field, ";")

	// The grammar allows comments in parentheses around the identifier and
	// allows it to be quoted, and neither was handled: a header written
	// "(best effort) mail.example.com; ..." parsed to nothing at all, and a
	// quoted one kept its quotes. Both forms read as this server's name to
	// anything that follows the grammar, so a comparison against our own
	// names matched neither and the header was kept.
	identifier = strings.TrimSpace(withoutComments(parts[0]))
	identifier = unquoted(identifier)
	position := strings.IndexFunc(identifier, unicode.IsSpace)
	if position > 0 {
		version := strings.TrimSpace(identifier[position:])
		if version != "1" {
			return "", nil, errors.New("authres: msgauth: unsupported version")
		}

		identifier = identifier[:position]
	}

	for position := 1; position < len(parts); position++ {
		segment := strings.TrimSpace(parts[position])
		if segment == "" {
			continue
		}

		result, err := parseResult(segment)
		if err != nil {
			return identifier, results, err
		}
		if result != nil {
			results = append(results, result)
		}
	}
	return
}

func parseResult(text string) (Result, error) {
	// TODO: ignore header comments in parenthesis

	parts := strings.Fields(text)
	if len(parts) == 0 || parts[0] == "none" {
		return nil, nil
	}

	methodName, methodValue, err := parseParameter(parts[0])
	if err != nil {
		return nil, err
	}
	method, value := methodName, ResultValue(strings.ToLower(methodValue))

	parameters := make(map[string]string)
	for index := 1; index < len(parts); index++ {
		key, parameterValue, err := parseParameter(parts[index])
		if err != nil {
			continue
		}

		parameters[key] = parameterValue
	}

	newResult, ok := results[method]

	var result Result
	if ok {
		result = newResult()
	} else {
		result = &GenericResult{
			Method:     method,
			Value:      value,
			Parameters: parameters,
		}
	}

	result.parse(value, parameters)
	return result, nil
}

func parseParameter(text string) (key string, value string, err error) {
	kv := strings.SplitN(text, "=", 2)
	if len(kv) != 2 {
		return "", "", errors.New("authres: msgauth: malformed authentication method and value")
	}
	return strings.ToLower(strings.TrimSpace(kv[0])), strings.TrimSpace(kv[1]), nil
}
