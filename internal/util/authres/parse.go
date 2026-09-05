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

func (r *AuthResult) parse(value ResultValue, parameters map[string]string) {
	r.Value = value
	r.Reason = parameters["reason"]
	r.Auth = parameters["smtp.auth"]
}

func (r *AuthResult) format() (ResultValue, map[string]string) {
	return r.Value, map[string]string{"smtp.auth": r.Auth}
}

type DKIMResult struct {
	Value      ResultValue
	Reason     string
	Domain     string
	Identifier string
}

func (r *DKIMResult) parse(value ResultValue, parameters map[string]string) {
	r.Value = value
	r.Reason = parameters["reason"]
	r.Domain = parameters["header.d"]
	r.Identifier = parameters["header.i"]
}

func (r *DKIMResult) format() (ResultValue, map[string]string) {
	return r.Value, map[string]string{
		"reason":   r.Reason,
		"header.d": r.Domain,
		"header.i": r.Identifier,
	}
}

type DomainKeysResult struct {
	Value  ResultValue
	Reason string
	Domain string
	From   string
	Sender string
}

func (r *DomainKeysResult) parse(value ResultValue, parameters map[string]string) {
	r.Value = value
	r.Reason = parameters["reason"]
	r.Domain = parameters["header.d"]
	r.From = parameters["header.from"]
	r.Sender = parameters["header.sender"]
}

func (r *DomainKeysResult) format() (ResultValue, map[string]string) {
	return r.Value, map[string]string{
		"reason":        r.Reason,
		"header.d":      r.Domain,
		"header.from":   r.From,
		"header.sender": r.Sender,
	}
}

type IPRevResult struct {
	Value  ResultValue
	Reason string
	IP     string
}

func (r *IPRevResult) parse(value ResultValue, parameters map[string]string) {
	r.Value = value
	r.Reason = parameters["reason"]
	r.IP = parameters["policy.iprev"]
}

func (r *IPRevResult) format() (ResultValue, map[string]string) {
	return r.Value, map[string]string{
		"reason":       r.Reason,
		"policy.iprev": r.IP,
	}
}

type SenderIDResult struct {
	Value       ResultValue
	Reason      string
	HeaderKey   string
	HeaderValue string
}

func (r *SenderIDResult) parse(value ResultValue, parameters map[string]string) {
	r.Value = value
	r.Reason = parameters["reason"]

	for k, v := range parameters {
		if strings.HasPrefix(k, "header.") {
			r.HeaderKey = strings.TrimPrefix(k, "header.")
			r.HeaderValue = v
			break
		}
	}
}

func (r *SenderIDResult) format() (value ResultValue, parameters map[string]string) {
	return r.Value, map[string]string{
		"reason":                                 r.Reason,
		"header." + strings.ToLower(r.HeaderKey): r.HeaderValue,
	}
}

type SPFResult struct {
	Value  ResultValue
	Reason string
	From   string
	Helo   string
}

func (r *SPFResult) parse(value ResultValue, parameters map[string]string) {
	r.Value = value
	r.Reason = parameters["reason"]
	r.From = parameters["smtp.mailfrom"]
	r.Helo = parameters["smtp.helo"]
}

func (r *SPFResult) format() (ResultValue, map[string]string) {
	return r.Value, map[string]string{
		"reason":        r.Reason,
		"smtp.mailfrom": r.From,
		"smtp.helo":     r.Helo,
	}
}

type DMARCResult struct {
	Value  ResultValue
	Reason string
	From   string
}

func (r *DMARCResult) parse(value ResultValue, parameters map[string]string) {
	r.Value = value
	r.Reason = parameters["reason"]
	r.From = parameters["header.from"]
}

func (r *DMARCResult) format() (ResultValue, map[string]string) {
	return r.Value, map[string]string{
		"reason":      r.Reason,
		"header.from": r.From,
	}
}

type ARCResult struct {
	Value  ResultValue
	Reason string
}

func (r *ARCResult) parse(value ResultValue, parameters map[string]string) {
	r.Value = value
	r.Reason = parameters["reason"]
}

func (r *ARCResult) format() (ResultValue, map[string]string) {
	return r.Value, map[string]string{
		"reason": r.Reason,
	}
}

type GenericResult struct {
	Method     string
	Value      ResultValue
	Parameters map[string]string
}

func (r *GenericResult) parse(value ResultValue, parameters map[string]string) {
	r.Value = value
	r.Parameters = parameters
}

func (r *GenericResult) format() (ResultValue, map[string]string) {
	return r.Value, r.Parameters
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
