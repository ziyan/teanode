// Package spf implements SPF (Sender Policy Framework) lookup and validation.
//
// Sender Policy Framework (SPF) is a simple email-validation system designed
// to detect email spoofing by providing a mechanism to allow receiving mail
// exchangers to check that incoming mail from a domain comes from a host
// authorized by that domain's administrators [Wikipedia].
//
// This package is intended to be used by SMTP servers to implement SPF
// validation.
//
// All mechanisms and modifiers are supported:
//
//	all
//	include
//	a
//	mx
//	ptr
//	ip4
//	ip6
//	exists
//	redirect
//	exp (ignored)
//	Macros
//
// References:
//
//	https://tools.ietf.org/html/rfc7208
//	https://en.wikipedia.org/wiki/Sender_Policy_Framework
package spf

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/op/go-logging"

	"github.com/ziyan/teanode/internal/util/mailparse"
)

var log = logging.MustGetLogger("spf")

// The Result of an SPF check. Note the values have meaning, we use them in
// headers.  https://tools.ietf.org/html/rfc7208#section-8
type Result string

// Valid results.
const (
	// https://tools.ietf.org/html/rfc7208#section-8.1
	// Not able to reach any conclusion.
	ResultNone Result = "none"

	// https://tools.ietf.org/html/rfc7208#section-8.2
	// No definite assertion (positive or negative).
	ResultNeutral = "neutral"

	// https://tools.ietf.org/html/rfc7208#section-8.3
	// Client is authorized to inject mail.
	ResultPass = "pass"

	// https://tools.ietf.org/html/rfc7208#section-8.4
	// Client is *not* authorized to use the domain.
	ResultFail = "fail"

	// https://tools.ietf.org/html/rfc7208#section-8.5
	// Not authorized, but unwilling to make a strong policy statement/
	ResultSoftFail = "softfail"

	// https://tools.ietf.org/html/rfc7208#section-8.6
	// Transient error while performing the check.
	ResultTempError = "temperror"

	// https://tools.ietf.org/html/rfc7208#section-8.7
	// Records could not be correctly interpreted.
	ResultPermError = "permerror"
)

var mapQualifierResult = map[byte]Result{
	'+': ResultPass,
	'-': ResultFail,
	'~': ResultSoftFail,
	'?': ResultNeutral,
}

// Resolver implements the methods we use to resolve DNS queries.
// It is intentionally compatible with *net.Resolver.
type Resolver interface {
	LookupTXT(ctx context.Context, name string) ([]string, error)
	LookupMX(ctx context.Context, name string) ([]*net.MX, error)
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
	LookupAddr(ctx context.Context, addr string) (names []string, err error)
}

type CheckOptions struct {
	Resolver Resolver
}

// Check fetches SPF records for `sender`'s domain, parses them,
// and evaluates them to determine if `ip` is permitted to send mail for it.
// The `helo` domain is used if the sender has no domain part.
//
// The `opts` optional parameter can be used to adjust some specific
// behaviours, such as the maximum number of DNS lookups allowed.
//
// The function returns a Result, which corresponds with the SPF result for
// the check as per RFC, as well as an error for debugging purposes. Note that
// the error may be non-nil even on successful checks.
//
// Reference: https://tools.ietf.org/html/rfc7208#section-4
func Check(ctx context.Context, ip net.IP, domain, sender string, options *CheckOptions) (Result, error) {
	log.Debugf("spf check: ip = %q, domain = %q, sender = %q", ip, domain, sender)
	self := &spf{
		ip:             ip,
		maxResolutions: 10,
		sender:         sender,
		ctx:            ctx,
		resolver:       net.DefaultResolver,
	}
	if options != nil && options.Resolver != nil {
		self.resolver = options.Resolver
	}
	return self.check(domain)
}

type spf struct {
	ip net.IP

	// resolution limit
	resolutions    uint
	maxResolutions uint

	// sender email address
	sender string

	// Result of doing a reverse lookup for ip (so we only do it once).
	rdns []string

	// Context for this spf.
	ctx context.Context

	// DNS resolver to use.
	resolver Resolver
}

var (
	aFieldPattern   = regexp.MustCompile(`^(a$|a:|a/)`)
	mxFieldPattern  = regexp.MustCompile(`^(mx$|mx:|mx/)`)
	ptrFieldPattern = regexp.MustCompile(`^(ptr$|ptr:)`)
)

func (self *spf) check(domain string) (Result, error) {
	self.resolutions++
	txt, err := self.resolveRecord(domain)
	if err != nil {
		log.Errorf("failed to resolve record %q: %s", domain, err)
		if isTemporaryError(err) {
			return ResultTempError, err
		}
		// Could not resolve the name, it may be missing the record.
		// https://tools.ietf.org/html/rfc7208#section-2.6.1
		return ResultPermError, err
	}
	log.Debugf("domain = %q, resolutions = %d / %d, record = %q", domain, self.resolutions, self.maxResolutions, txt)
	if txt == "" {
		// No record => None.
		// https://tools.ietf.org/html/rfc7208#section-4.5
		return ResultNone, fmt.Errorf("spf: no spf record found for %q", domain)
	}

	// redirects must be handled after the rest; instead of having two loops,
	// we just move them to the end.
	var nonRedirectFields, redirectFields []string
	for _, field := range strings.Fields(txt) {
		if strings.HasPrefix(field, "redirect=") {
			redirectFields = append(redirectFields, field)
		} else {
			nonRedirectFields = append(nonRedirectFields, field)
		}
	}
	if len(redirectFields) > 1 {
		// At most a single redirect is allowed.
		// https://tools.ietf.org/html/rfc7208#section-6
		return ResultPermError, fmt.Errorf("spf: too many redirect fields for %q", domain)
	}
	fields := make([]string, 0, len(nonRedirectFields)+len(redirectFields))
	fields = append(fields, nonRedirectFields...)
	fields = append(fields, redirectFields...)

	for _, field := range fields {
		// The version check should be case-insensitive (it's a
		// case-insensitive constant in the standard).
		// https://tools.ietf.org/html/rfc7208#section-12
		if strings.HasPrefix(field, "v=") || strings.HasPrefix(field, "V=") {
			continue
		}

		// Limit the number of spfs.
		// https://tools.ietf.org/html/rfc7208#section-4.6.4
		if self.resolutions > self.maxResolutions {
			return ResultPermError, fmt.Errorf("spf: lookup limit reached")
		}

		// See if we have a qualifier, defaulting to + (pass).
		// https://tools.ietf.org/html/rfc7208#section-4.6.2
		result, ok := mapQualifierResult[field[0]]
		if ok {
			field = field[1:]
		} else {
			result = ResultPass
		}

		// Mechanism and modifier names are case-insensitive.
		// https://tools.ietf.org/html/rfc7208#section-4.6.1
		lowerField := strings.ToLower(field)
		if lowerField == "all" {
			// https://tools.ietf.org/html/rfc7208#section-5.1
			return result, nil
		} else if strings.HasPrefix(lowerField, "include:") {
			if ok, result, err := self.handleIncludeField(result, field, domain); ok {
				return result, err
			}
		} else if aFieldPattern.MatchString(lowerField) {
			if ok, result, err := self.handleAField(result, field, domain); ok {
				return result, err
			}
		} else if mxFieldPattern.MatchString(lowerField) {
			if ok, result, err := self.handleMxField(result, field, domain); ok {
				return result, err
			}
		} else if strings.HasPrefix(lowerField, "ip4:") || strings.HasPrefix(lowerField, "ip6:") {
			if ok, result, err := self.handleIpField(result, field); ok {
				return result, err
			}
		} else if ptrFieldPattern.MatchString(lowerField) {
			if ok, result, err := self.handlePtrField(result, field, domain); ok {
				return result, err
			}
		} else if strings.HasPrefix(lowerField, "exists:") {
			if ok, result, err := self.handleExistsField(result, field, domain); ok {
				return result, err
			}
		} else if strings.HasPrefix(lowerField, "exp=") {
			continue
		} else if strings.HasPrefix(lowerField, "redirect=") {
			return self.handleRedirectField(field, domain)
		} else {
			// http://www.openspf.org/SPF_Record_Syntax
			return ResultPermError, fmt.Errorf("spf: unknown field %q", field)
		}
	}

	// Got to the end of the evaluation without a result => Neutral.
	// https://tools.ietf.org/html/rfc7208#section-4.7
	return ResultNeutral, nil
}

// resolveRecord gets TXT records from the given domain, and returns the SPF
// (if any).  Note that at most one SPF is allowed per a given domain:
// https://tools.ietf.org/html/rfc7208#section-3
// https://tools.ietf.org/html/rfc7208#section-3.2
// https://tools.ietf.org/html/rfc7208#section-4.5
func (self *spf) resolveRecord(domain string) (string, error) {
	txts, err := self.resolver.LookupTXT(self.ctx, domain)
	if err != nil {
		return "", err
	}

	records := []string{}
	for _, txt := range txts {
		// The version check should be case-insensitive (it's a
		// case-insensitive constant in the standard).
		// https://tools.ietf.org/html/rfc7208#section-12
		if strings.HasPrefix(strings.ToLower(txt), "v=spf1 ") {
			records = append(records, txt)
		}

		// An empty record is explicitly allowed:
		// https://tools.ietf.org/html/rfc7208#section-4.5
		if strings.ToLower(txt) == "v=spf1" {
			records = append(records, txt)
		}
	}

	// 0 records is ok, handled by the parent.
	// 1 record is what we expect, return the record.
	// More than that, it's a permanent error:
	// https://tools.ietf.org/html/rfc7208#section-4.5
	switch length := len(records); length {
	case 0:
		return "", nil
	case 1:
		return records[0], nil
	default:
		return "", fmt.Errorf("spf: multiple matching spf records")
	}
}

func (self *spf) resolveRdns() ([]string, error) {
	if self.rdns != nil {
		return self.rdns, nil
	}
	var rdns []string
	self.resolutions++
	names, err := self.resolver.LookupAddr(self.ctx, self.ip.String())
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		// Validate the record by doing a forward spf: it has to
		// have some A/AAAA.
		// https://tools.ietf.org/html/rfc7208#section-5.5
		if self.resolutions > self.maxResolutions {
			return nil, fmt.Errorf("spf: lookup limit reached")
		}
		self.resolutions++
		ips, err := self.resolver.LookupIPAddr(self.ctx, name)
		if err != nil {
			// RFC explicitly says to skip domains which error here.
			continue
		}
		if len(ips) > 0 {
			// Append the lower-case variants so we do a case-insensitive
			// lookup below.
			rdns = append(rdns, strings.ToLower(name))
		}
	}
	self.rdns = rdns
	return rdns, nil
}

func isTemporaryError(err error) bool {
	if err, ok := err.(*net.DNSError); ok && err.Temporary() {
		return true
	}
	return false
}

// handleIpField processes an "ip" field.
func (self *spf) handleIpField(result Result, field string) (bool, Result, error) {
	ipOrCidr := field[len("ip4:"):]
	if strings.Contains(ipOrCidr, "/") {
		_, network, err := net.ParseCIDR(ipOrCidr)
		if err != nil {
			return true, ResultPermError, fmt.Errorf("spf: invalid mask: %w", err)
		}
		if network.Contains(self.ip) {
			return true, result, nil
		}
	} else {
		ip := net.ParseIP(ipOrCidr)
		if ip == nil {
			return true, ResultPermError, fmt.Errorf("spf: invalid ip %q", ipOrCidr)
		}
		if ip.Equal(self.ip) {
			return true, result, nil
		}
	}
	return false, "", nil
}

// handlePtrField processes a "ptr" field.
func (self *spf) handlePtrField(result Result, field, domain string) (bool, Result, error) {
	// Extract the domain if the field is in the form "ptr:domain".
	ptrDomain := domain
	if len(field) >= len("ptr:") {
		ptrDomain = field[len("ptr:"):]
	}
	ptrDomain, err := self.expandMacros(ptrDomain, domain)
	if err != nil {
		return true, ResultPermError, fmt.Errorf("spf: invalid macro: %w", err)
	}
	if ptrDomain == "" {
		return true, ResultPermError, fmt.Errorf("spf: invalid ptr field")
	}
	rdns, err := self.resolveRdns()
	if err != nil {
		// https://tools.ietf.org/html/rfc7208#section-5
		if isTemporaryError(err) {
			return true, ResultTempError, err
		}
		return false, "", err
	}
	ptrDomain = strings.ToLower(ptrDomain)
	for _, name := range rdns {
		if strings.HasSuffix(name, ptrDomain+".") {
			return true, result, nil
		}
	}
	return false, "", nil
}

// handleExistsField processes a "exists" field.
// https://tools.ietf.org/html/rfc7208#section-5.7
func (self *spf) handleExistsField(result Result, field, domain string) (bool, Result, error) {
	// The field is in the form "exists:<domain>".
	existsDomain, err := self.expandMacros(field[len("exists:"):], domain)
	if err != nil {
		return true, ResultPermError, fmt.Errorf("spf: invalid macro: %w", err)
	}
	if existsDomain == "" {
		return true, ResultPermError, fmt.Errorf("spf: invalid domain")
	}
	self.resolutions++
	ips, err := self.resolver.LookupIPAddr(self.ctx, existsDomain)
	if err != nil {
		// https://tools.ietf.org/html/rfc7208#section-5
		if isTemporaryError(err) {
			return true, ResultTempError, err
		}
		return false, "", err
	}
	// Exists only resolutionss if there are IPv4 matches.
	for _, ip := range ips {
		if ip.IP.To4() != nil {
			return true, result, nil
		}
	}
	return false, "", nil
}

// handleIncludeField processes an "include" field.
func (self *spf) handleIncludeField(result Result, field, domain string) (bool, Result, error) {
	// https://tools.ietf.org/html/rfc7208#section-5.2
	includeDomain, err := self.expandMacros(field[len("include:"):], domain)
	if err != nil {
		return true, ResultPermError, fmt.Errorf("spf: invalid macro: %w", err)
	}
	includeResult, err := self.check(includeDomain)
	switch includeResult {
	case ResultPass:
		return true, result, err
	case ResultFail, ResultSoftFail, ResultNeutral:
		return false, includeResult, err
	case ResultTempError:
		return true, ResultTempError, err
	case ResultPermError:
		return true, ResultPermError, err
	}
	return true, ResultPermError, fmt.Errorf("spf: invalid result from include")
}

type dualMasks struct {
	v4 net.IPMask
	v6 net.IPMask
}

func ipMatch(ip, toMatch net.IP, masks dualMasks) bool {
	mask := net.IPMask(nil)
	if toMatch.To4() != nil && masks.v4 != nil {
		mask = masks.v4
	} else if toMatch.To4() == nil && masks.v6 != nil {
		mask = masks.v6
	}

	if mask != nil {
		ipnet := net.IPNet{IP: toMatch, Mask: mask}
		return ipnet.Contains(ip)
	}

	return ip.Equal(toMatch)
}

var aPattern = regexp.MustCompile(`^[aA](:([^/]+))?(/(\w+))?(//(\w+))?$`)
var mxPattern = regexp.MustCompile(`^[mM][xX](:([^/]+))?(/(\w+))?(//(\w+))?$`)

func matchDomainAndMask(re *regexp.Regexp, field, domain string) (string, dualMasks, error) {
	masks := dualMasks{}
	groups := re.FindStringSubmatch(field)
	if groups != nil {
		if groups[2] != "" {
			domain = groups[2]
		}
		if groups[4] != "" {
			i, err := strconv.Atoi(groups[4])
			mask4 := net.CIDRMask(i, 32)
			if err != nil || mask4 == nil {
				return "", masks, fmt.Errorf("spf: invalid mask")
			}
			masks.v4 = mask4
		}
		if groups[6] != "" {
			i, err := strconv.Atoi(groups[6])
			mask6 := net.CIDRMask(i, 128)
			if err != nil || mask6 == nil {
				return "", masks, fmt.Errorf("spf: invalid mask")
			}
			masks.v6 = mask6
		}
	}
	// Test to catch malformed entries: if there's a /, there must be at least
	// one mask.
	if strings.Contains(field, "/") && masks.v4 == nil && masks.v6 == nil {
		return "", masks, fmt.Errorf("spf: invalid mask")
	}
	return domain, masks, nil
}

// handleAField processes an "a" field.
func (self *spf) handleAField(result Result, field, domain string) (bool, Result, error) {
	// https://tools.ietf.org/html/rfc7208#section-5.3
	aDomain, masks, err := matchDomainAndMask(aPattern, field, domain)
	if err != nil {
		return true, ResultPermError, err
	}
	aDomain, err = self.expandMacros(aDomain, domain)
	if err != nil {
		return true, ResultPermError, fmt.Errorf("spf: invalid macro: %w", err)
	}
	self.resolutions++
	ips, err := self.resolver.LookupIPAddr(self.ctx, aDomain)
	if err != nil {
		// https://tools.ietf.org/html/rfc7208#section-5
		if isTemporaryError(err) {
			return true, ResultTempError, err
		}
		return false, "", err
	}
	for _, ip := range ips {
		if ipMatch(self.ip, ip.IP, masks) {
			return true, result, nil
		}
	}
	return false, "", nil
}

// handleMxField processes an "mx" field.
func (self *spf) handleMxField(result Result, field, domain string) (bool, Result, error) {
	// https://tools.ietf.org/html/rfc7208#section-5.4
	mxDomain, masks, err := matchDomainAndMask(mxPattern, field, domain)
	if err != nil {
		return true, ResultPermError, err
	}
	mxDomain, err = self.expandMacros(mxDomain, domain)
	if err != nil {
		return true, ResultPermError, fmt.Errorf("spf: invalid macro: %w", err)
	}
	self.resolutions++
	mxs, err := self.resolver.LookupMX(self.ctx, mxDomain)
	if err != nil {
		// https://tools.ietf.org/html/rfc7208#section-5
		if isTemporaryError(err) {
			return true, ResultTempError, err
		}
		return false, "", err
	}
	// There's an explicit maximum of 10 MX records per match.
	// https://tools.ietf.org/html/rfc7208#section-4.6.4
	if len(mxs) > 10 {
		return true, ResultPermError, fmt.Errorf("spf: too many MX records")
	}
	var ips []net.IP
	for _, mx := range mxs {
		self.resolutions++
		mxIps, err := self.resolver.LookupIPAddr(self.ctx, mx.Host)
		if err != nil {
			// https://tools.ietf.org/html/rfc7208#section-5
			if isTemporaryError(err) {
				return true, ResultTempError, err
			}
			return false, "", err
		}
		for _, mxIp := range mxIps {
			ips = append(ips, mxIp.IP)
		}
	}
	for _, ip := range ips {
		if ipMatch(self.ip, ip, masks) {
			return true, result, nil
		}
	}
	return false, "", nil
}

// handleRedirectField processes a "redirect=" field.
func (self *spf) handleRedirectField(field, domain string) (Result, error) {
	redirectDomain, err := self.expandMacros(field[len("redirect="):], domain)
	if err != nil {
		return ResultPermError, fmt.Errorf("spf: invalid macro: %w", err)
	}
	if redirectDomain == "" {
		return ResultPermError, fmt.Errorf("spf: invalid domain")
	}
	// https://tools.ietf.org/html/rfc7208#section-6.1
	result, err := self.check(redirectDomain)
	if result == ResultNone {
		return ResultPermError, fmt.Errorf("spf: redirect resulted in none")
	}
	return result, err
}

// Group extraction of macro-string from the formal specification.
// https://tools.ietf.org/html/rfc7208#section-7.1
var macroPattern = regexp.MustCompile(`([slodiphcrtvSLODIPHCRTV])([0-9]+)?([rR])?([-.+,/_=]+)?`)

// Expand macros, return the expanded string.
// This expects to be passed the domain-spec within a field, not an entire
// field or larger (that has problematic security implications).
// https://tools.ietf.org/html/rfc7208#section-7
func (self *spf) expandMacros(macro, domain string) (string, error) {
	// Macros/domains shouldn't contain CIDR. Our parsing should prevent it
	// from happening in case where it matters (a, mx), but for the ones which
	// doesn't, prevent them from sneaking through.
	if strings.Contains(macro, "/") {
		return "", fmt.Errorf("spf: macro contains slash")
	}

	// Bypass the complex logic if there are no macros present.
	if !strings.Contains(macro, "%") {
		return macro, nil
	}

	// Are we processing the character right after "%"?
	afterPercent := false

	// Are we inside a macro definition (%{...}) ?
	inMacroDefinition := false

	// Macro string, where we accumulate the values inside the definition.
	macroString := ""

	var err error
	expandedMacro := ""
	for _, character := range macro {
		if afterPercent {
			afterPercent = false
			switch character {
			case '%':
				expandedMacro += "%"
				continue
			case '_':
				expandedMacro += " "
				continue
			case '-':
				expandedMacro += "%20"
				continue
			case '{':
				inMacroDefinition = true
				continue
			}
			return "", fmt.Errorf("spf: invalid macro")
		}
		if inMacroDefinition {
			if character != '}' {
				macroString += string(character)
				continue
			}
			inMacroDefinition = false

			// Extract letter, digit transformer, reverse transformer, and
			// delimiters.
			groups := macroPattern.FindStringSubmatch(macroString)
			macroString = ""
			if groups == nil {
				return "", fmt.Errorf("spf: invalid macro, no match found")
			}
			letter := groups[1]

			digits := 0
			if groups[2] != "" {
				// Use 0 as "no digits given"; an explicit value of 0 is not
				// valid.
				digits, err = strconv.Atoi(groups[2])
				if err != nil || digits <= 0 {
					return "", fmt.Errorf("spf: invalid macro, invalid digits")
				}
			}
			reverse := groups[3] == "r" || groups[3] == "R"
			delimiters := groups[4]
			if delimiters == "" {
				// By default, split strings by ".".
				delimiters = "."
			}

			// Uppercase letters indicate URL escaping of the results.
			urlEscape := letter == strings.ToUpper(letter)
			letter = strings.ToLower(letter)

			var str string
			switch letter {
			case "s":
				str = self.sender
			case "l":
				str, _ = mailparse.SplitAddress(self.sender)
			case "o":
				_, str = mailparse.SplitAddress(self.sender)
			case "d":
				str = domain
			case "i":
				str = self.ip.String()
			case "p":
				// This shouldn't be used, we don't want to support it, it's
				// risky. "unknown" is a safe value.
				// https://tools.ietf.org/html/rfc7208#section-7.3
				str = "unknown"
			case "v":
				if self.ip.To4() != nil {
					str = "in-addr"
				} else {
					str = "ip6"
				}
			case "h":
				str = domain
			default:
				// c, r, t are allowed in exp only, and we don't expand macros
				// in exp so they are just as invalid as the rest.
				return "", fmt.Errorf("spf: invalid macro, unknown letter %q", letter)
			}

			// Split str using the given separators.
			split := strings.FieldsFunc(str, func(r rune) bool {
				return strings.ContainsRune(delimiters, r)
			})

			// Reverse if requested.
			if reverse {
				reverseStrings(split)
			}

			// Leave the last $digits fields, if given.
			if digits > 0 {
				if digits > len(split) {
					digits = len(split)
				}
				split = split[len(split)-digits:]
			}

			// Join back, always with "."
			str = strings.Join(split, ".")

			// Escape if requested. Note this doesn't strictly escape ALL
			// unreserved characters, it's the closest we can get without
			// reimplmenting it ourselves.
			if urlEscape {
				str = url.QueryEscape(str)
			}

			expandedMacro += str
			continue
		}
		if character == '%' {
			afterPercent = true
			continue
		}
		expandedMacro += string(character)
	}
	log.Debugf("macro expanded %q to %q", macro, expandedMacro)
	return expandedMacro, nil
}

func reverseStrings(a []string) {
	for left, right := 0, len(a)-1; left < right; left, right = left+1, right-1 {
		a[left], a[right] = a[right], a[left]
	}
}
