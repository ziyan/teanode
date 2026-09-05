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

	for k, v := range parameters {
		if strings.HasPrefix(k, "header.") {
			self.HeaderKey = strings.TrimPrefix(k, "header.")
			self.HeaderValue = v
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

// Parse parses the provided Authentication-Results header field. It returns the
// authentication service identifier and authentication results.
func Parse(v string) (identifier string, results []Result, err error) {
	parts := strings.Split(v, ";")

	identifier = strings.TrimSpace(parts[0])
	i := strings.IndexFunc(identifier, unicode.IsSpace)
	if i > 0 {
		version := strings.TrimSpace(identifier[i:])
		if version != "1" {
			return "", nil, errors.New("authres: msgauth: unsupported version")
		}

		identifier = identifier[:i]
	}

	for i := 1; i < len(parts); i++ {
		s := strings.TrimSpace(parts[i])
		if s == "" {
			continue
		}

		result, err := parseResult(s)
		if err != nil {
			return identifier, results, err
		}
		if result != nil {
			results = append(results, result)
		}
	}
	return
}

func parseResult(s string) (Result, error) {
	// TODO: ignore header comments in parenthesis

	parts := strings.Fields(s)
	if len(parts) == 0 || parts[0] == "none" {
		return nil, nil
	}

	k, v, err := parseParameter(parts[0])
	if err != nil {
		return nil, err
	}
	method, value := k, ResultValue(strings.ToLower(v))

	parameters := make(map[string]string)
	for i := 1; i < len(parts); i++ {
		k, v, err := parseParameter(parts[i])
		if err != nil {
			continue
		}

		parameters[k] = v
	}

	newResult, ok := results[method]

	var r Result
	if ok {
		r = newResult()
	} else {
		r = &GenericResult{
			Method:     method,
			Value:      value,
			Parameters: parameters,
		}
	}

	r.parse(value, parameters)
	return r, nil
}

func parseParameter(s string) (k string, v string, err error) {
	kv := strings.SplitN(s, "=", 2)
	if len(kv) != 2 {
		return "", "", errors.New("authres: msgauth: malformed authentication method and value")
	}
	return strings.ToLower(strings.TrimSpace(kv[0])), strings.TrimSpace(kv[1]), nil
}
