package spf_test

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/util/spf"
)

// Testing DNS resolver.
//
// Not exported since this is not part of the public API and only used
// internally on tests.
type testResolver struct {
	Txt    map[string][]string
	Mx     map[string][]*net.MX
	Ip     map[string][]net.IP
	Addr   map[string][]string
	Errors map[string]error
}

func newTestResolver() *testResolver {
	return &testResolver{
		Txt:    map[string][]string{},
		Mx:     map[string][]*net.MX{},
		Ip:     map[string][]net.IP{},
		Addr:   map[string][]string{},
		Errors: map[string]error{},
	}
}

func (self *testResolver) LookupTXT(ctx context.Context, domain string) (txts []string, err error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	domain = strings.ToLower(domain)
	domain = strings.TrimRight(domain, ".")
	return self.Txt[domain], self.Errors[domain]
}

func (self *testResolver) LookupMX(ctx context.Context, domain string) (mxs []*net.MX, err error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	domain = strings.ToLower(domain)
	domain = strings.TrimRight(domain, ".")
	return self.Mx[domain], self.Errors[domain]
}

func (self *testResolver) LookupIPAddr(ctx context.Context, host string) (as []net.IPAddr, err error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	host = strings.ToLower(host)
	host = strings.TrimRight(host, ".")
	return ipsToAddrs(self.Ip[host]), self.Errors[host]
}

func ipsToAddrs(ips []net.IP) []net.IPAddr {
	as := []net.IPAddr{}
	for _, ip := range ips {
		as = append(as, net.IPAddr{IP: ip, Zone: ""})
	}
	return as
}

func (self *testResolver) LookupAddr(ctx context.Context, host string) (addrs []string, err error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	host = strings.ToLower(host)
	host = strings.TrimRight(host, ".")
	return self.Addr[host], self.Errors[host]
}

var ip1110 = net.ParseIP("1.1.1.0")
var ip1111 = net.ParseIP("1.1.1.1")
var ip6666 = net.ParseIP("2001:db8::68")
var ip6660 = net.ParseIP("2001:db8::0")

var (
	errNoResult           = fmt.Errorf("spf: no spf record found for")
	errUnknownField       = fmt.Errorf("spf: unknown field")
	errInvalidMask        = fmt.Errorf("spf: invalid mask")
	errInvalidIp          = fmt.Errorf("spf: invalid ip")
	errInvalidDomain      = fmt.Errorf("spf: invalid domain")
	errLookupLimitReached = fmt.Errorf("spf: lookup limit reached")
	errInvalidMacro       = fmt.Errorf("spf: invalid macro")
	errInvalidInclude     = fmt.Errorf("spf: invalid result from include")
	errRedirectNoneResult = fmt.Errorf("spf: redirect resulted in none")
)

func errorsEqual(err1, err2 error) bool {
	if err1 == nil || err2 == nil {
		return err1 == nil && err2 == nil
	}
	return strings.HasPrefix(err1.Error(), err2.Error())
}

func TestBasic(t *testing.T) {
	t.Parallel()

	dns := newTestResolver()

	cases := []struct {
		txt string
		res spf.Result
		err error
	}{
		{"", spf.ResultNone, errNoResult},
		{"blah", spf.ResultNone, errNoResult},
		{"v=spf1", spf.ResultNeutral, nil},
		{"v=spf1 ", spf.ResultNeutral, nil},
		{"v=spf1 -", spf.ResultPermError, errUnknownField},
		{"v=spf1 all", spf.ResultPass, nil},
		{"v=spf1 exp=blah +all", spf.ResultPass, nil},
		{"v=spf1  +all", spf.ResultPass, nil},
		{"v=spf1 -all ", spf.ResultFail, nil},
		{"v=spf1 ~all", spf.ResultSoftFail, nil},
		{"v=spf1 ?all", spf.ResultNeutral, nil},
		{"v=spf1 a ~all", spf.ResultSoftFail, nil},
		{"v=spf1 a/24", spf.ResultNeutral, nil},
		{"v=spf1 a:d1110/24", spf.ResultPass, nil},
		{"v=spf1 a:d1110/montoto", spf.ResultPermError, errInvalidMask},
		{"v=spf1 a:d1110/99", spf.ResultPermError, errInvalidMask},
		{"v=spf1 a:d1110/32", spf.ResultNeutral, nil},
		{"v=spf1 a:d1110", spf.ResultNeutral, nil},
		{"v=spf1 a:d1111", spf.ResultPass, nil},
		{"v=spf1 a:nothing/24", spf.ResultNeutral, nil},
		{"v=spf1 mx", spf.ResultNeutral, nil},
		{"v=spf1 mx/24", spf.ResultNeutral, nil},
		{"v=spf1 mx:a/montoto ~all", spf.ResultPermError, errInvalidMask},
		{"v=spf1 mx:d1110/24 ~all", spf.ResultPass, nil},
		{"v=spf1 mx:d1110/24//100 ~all", spf.ResultPass, nil},
		{"v=spf1 mx:d1110/24//129 ~all", spf.ResultPermError, errInvalidMask},
		{"v=spf1 mx:d1110/24/100 ~all", spf.ResultPermError, errInvalidMask},
		{"v=spf1 mx:d1110/99 ~all", spf.ResultPermError, errInvalidMask},
		{"v=spf1 ip4:1.2.3.4 ~all", spf.ResultSoftFail, nil},
		{"v=spf1 ip6:12 ~all", spf.ResultPermError, errInvalidIp},
		{"v=spf1 ip4:1.1.1.1 -all", spf.ResultPass, nil},
		{"v=spf1 ip4:1.1.1.1/24 -all", spf.ResultPass, nil},
		{"v=spf1 ip4:1.1.1.1/lala -all", spf.ResultPermError, errInvalidMask},
		{"v=spf1 ip4:1.1.1.1/33 -all", spf.ResultPermError, errInvalidMask},
		{"v=spf1 include:doesnotexist", spf.ResultPermError, errInvalidInclude},
		{"v=spf1 ptr -all", spf.ResultPass, nil},
		{"v=spf1 ptr:d1111 -all", spf.ResultPass, nil},
		{"v=spf1 ptr:lalala -all", spf.ResultPass, nil},
		{"v=spf1 ptr:doesnotexist -all", spf.ResultFail, nil},
		{"v=spf1 blah", spf.ResultPermError, errUnknownField},
		{"v=spf1 exists:d1111 -all", spf.ResultPass, nil},
		{"v=spf1 redirect=", spf.ResultPermError, errInvalidDomain},
	}

	dns.Ip["d1111"] = []net.IP{ip1111}
	dns.Ip["d1110"] = []net.IP{ip1110}
	dns.Mx["d1110"] = []*net.MX{mx("d1110", 5), mx("nothing", 10)}
	dns.Addr["1.1.1.1"] = []string{"lalala.", "xx.domain.", "d1111."}
	dns.Ip["lalala"] = []net.IP{ip1111}
	dns.Ip["xx.domain"] = []net.IP{ip1111}

	for _, c := range cases {
		dns.Txt["domain"] = []string{c.txt}
		res, err := spf.Check(context.TODO(), ip1111, "domain", "", &spf.CheckOptions{
			Resolver: dns,
		})
		if (res == spf.ResultTempError || res == spf.ResultPermError) && (err == nil) {
			t.Errorf("%q: expected error, got nil", c.txt)
		}
		if res != c.res {
			t.Errorf("%q: expected %q, got %q", c.txt, c.res, res)
		}
		if !errorsEqual(err, c.err) {
			t.Errorf("%q: expected error [%v], got [%v]", c.txt, c.err, err)
		}
	}
}

func TestIPv6(t *testing.T) {
	t.Parallel()

	dns := newTestResolver()

	cases := []struct {
		txt string
		res spf.Result
		err error
	}{
		{"v=spf1 all", spf.ResultPass, nil},
		{"v=spf1 a ~all", spf.ResultSoftFail, nil},
		{"v=spf1 a/24", spf.ResultNeutral, nil},
		{"v=spf1 a:d6660//24", spf.ResultPass, nil},
		{"v=spf1 a:d6660/24//100", spf.ResultPass, nil},
		{"v=spf1 a:d6660", spf.ResultNeutral, nil},
		{"v=spf1 a:d6666", spf.ResultPass, nil},
		{"v=spf1 a:nothing//24", spf.ResultNeutral, nil},
		{"v=spf1 mx:d6660//24 ~all", spf.ResultPass, nil},
		{"v=spf1 mx:d6660/24//100 ~all", spf.ResultPass, nil},
		{"v=spf1 mx:d6660/24/100 ~all", spf.ResultPermError, errInvalidMask},
		{"v=spf1 ip6:2001:db8::68 ~all", spf.ResultPass, nil},
		{"v=spf1 ip6:2001:db8::1/24 ~all", spf.ResultPass, nil},
		{"v=spf1 ip6:2001:db8::1/100 ~all", spf.ResultPass, nil},
		{"v=spf1 ptr -all", spf.ResultPass, nil},
		{"v=spf1 ptr:d6666 -all", spf.ResultPass, nil},
		{"v=spf1 ptr:sonlas6 -all", spf.ResultPass, nil},
		{"v=spf1 ptr:sonlas7 -all", spf.ResultFail, nil},
	}

	dns.Ip["d6666"] = []net.IP{ip6666}
	dns.Ip["d6660"] = []net.IP{ip6660}
	dns.Mx["d6660"] = []*net.MX{mx("d6660", 5), mx("nothing", 10)}
	dns.Addr["2001:db8::68"] = []string{"sonlas6.", "domain.", "d6666."}
	dns.Ip["domain"] = []net.IP{ip1111}
	dns.Ip["sonlas6"] = []net.IP{ip6666}

	for _, c := range cases {
		dns.Txt["domain"] = []string{c.txt}
		res, err := spf.Check(context.TODO(), ip6666, "domain", "", &spf.CheckOptions{
			Resolver: dns,
		})
		if (res == spf.ResultTempError || res == spf.ResultPermError) && (err == nil) {
			t.Errorf("%q: expected error, got nil", c.txt)
		}
		if res != c.res {
			t.Errorf("%q: expected %q, got %q", c.txt, c.res, res)
		}
		if !errorsEqual(err, c.err) {
			t.Errorf("%q: expected error [%v], got [%v]", c.txt, c.err, err)
		}
	}
}

func TestInclude(t *testing.T) {
	t.Parallel()

	// Test that the include is doing a recursive lookup.
	// If we got a match on 1.1.1.1, is because include:domain2 did not match.
	dns := newTestResolver()
	dns.Txt["domain"] = []string{"v=spf1 include:domain2 ip4:1.1.1.1"}

	cases := []struct {
		txt string
		res spf.Result
		err error
	}{
		{"", spf.ResultPermError, errInvalidInclude},
		{"v=spf1 all", spf.ResultPass, nil},

		// domain2 did not pass, so continued and matched parent's ip4.
		{"v=spf1", spf.ResultPass, nil},
		{"v=spf1 -all", spf.ResultPass, nil},
	}

	for _, c := range cases {
		dns.Txt["domain2"] = []string{c.txt}
		res, err := spf.Check(context.TODO(), ip1111, "domain", "", &spf.CheckOptions{
			Resolver: dns,
		})
		if res != c.res || !errorsEqual(err, c.err) {
			t.Errorf("%q: expected [%v/%v], got [%v/%v]",
				c.txt, c.res, c.err, res, err)
		}
	}
}

func TestRecursionLimit(t *testing.T) {
	t.Parallel()

	dns := newTestResolver()
	dns.Txt["domain"] = []string{"v=spf1 include:domain ~all"}

	res, err := spf.Check(context.TODO(), ip1111, "domain", "", &spf.CheckOptions{
		Resolver: dns,
	})
	if res != spf.ResultPermError || !errorsEqual(err, errLookupLimitReached) {
		t.Errorf("expected permerror, got %v (%v)", res, err)
	}
}

func TestRedirect(t *testing.T) {
	t.Parallel()

	dns := newTestResolver()
	dns.Txt["domain"] = []string{"v=spf1 redirect=domain2"}
	dns.Txt["domain2"] = []string{"v=spf1 ip4:1.1.1.1 -all"}

	res, err := spf.Check(context.TODO(), ip1111, "domain", "", &spf.CheckOptions{
		Resolver: dns,
	})
	if res != spf.ResultPass {
		t.Errorf("expected pass, got %v (%v)", res, err)
	}
}

func TestInvalidRedirect(t *testing.T) {
	t.Parallel()

	// Redirect to a non-existing host; the inner check returns spf.ResultNone, but due
	// to the redirection, this lookup should return spf.ResultPermError.
	// https://tools.ietf.org/html/rfc7208#section-6.1
	dns := newTestResolver()
	dns.Txt["domain"] = []string{"v=spf1 redirect=doesnotexist"}

	res, err := spf.Check(context.TODO(), ip1111, "doesnotexist", "", &spf.CheckOptions{
		Resolver: dns,
	})
	if res != spf.ResultNone {
		t.Errorf("expected none, got %v (%v)", res, err)
	}

	res, err = spf.Check(context.TODO(), ip1111, "domain", "", &spf.CheckOptions{
		Resolver: dns,
	})
	if res != spf.ResultPermError || !errorsEqual(err, errRedirectNoneResult) {
		t.Errorf("expected permerror, got %v (%v)", res, err)
	}
}

func TestRedirectOrder(t *testing.T) {
	t.Parallel()

	// We should only check redirects after all mechanisms, even if the
	// redirect modifier appears before them.
	dns := newTestResolver()
	dns.Txt["faildom"] = []string{"v=spf1 -all"}

	dns.Txt["domain"] = []string{"v=spf1 redirect=faildom"}
	res, err := spf.Check(context.TODO(), ip1111, "domain", "", &spf.CheckOptions{
		Resolver: dns,
	})
	if res != spf.ResultFail || err != nil {
		t.Errorf("expected fail, got %v (%v)", res, err)
	}

	dns.Txt["domain"] = []string{"v=spf1 redirect=faildom all"}
	res, err = spf.Check(context.TODO(), ip1111, "domain", "", &spf.CheckOptions{
		Resolver: dns,
	})
	if res != spf.ResultPass || err != nil {
		t.Errorf("expected pass, got %v (%v)", res, err)
	}
}

func TestNoRecord(t *testing.T) {
	t.Parallel()

	dns := newTestResolver()
	dns.Txt["d1"] = []string{""}
	dns.Txt["d2"] = []string{"loco", "v=spf2"}

	for _, domain := range []string{"d1", "d2", "d3"} {
		res, err := spf.Check(context.TODO(), ip1111, domain, "", &spf.CheckOptions{
			Resolver: dns,
		})
		if res != spf.ResultNone {
			t.Errorf("expected none, got %v (%v)", res, err)
		}
	}
}

func TestDNSTemporaryErrors(t *testing.T) {
	t.Parallel()

	dns := newTestResolver()
	dnsError := &net.DNSError{
		Err:         "temporary error for testing",
		IsTemporary: true,
	}

	// Domain "tmperr" will fail resolution with a temporary error.
	dns.Errors["tmperr"] = dnsError
	dns.Errors["1.1.1.1"] = dnsError
	dns.Mx["tmpmx"] = []*net.MX{mx("tmperr", 10)}

	cases := []struct {
		txt string
		res spf.Result
	}{
		{"v=spf1 include:tmperr", spf.ResultTempError},
		{"v=spf1 a:tmperr", spf.ResultTempError},
		{"v=spf1 mx:tmperr", spf.ResultTempError},
		{"v=spf1 ptr:tmperr", spf.ResultTempError},
		{"v=spf1 mx:tmpmx", spf.ResultTempError},
	}

	for _, c := range cases {
		dns.Txt["domain"] = []string{c.txt}
		res, err := spf.Check(context.TODO(), ip1111, "domain", "", &spf.CheckOptions{
			Resolver: dns,
		})
		if res != c.res {
			t.Errorf("%q: expected %v, got %v (%v)",
				c.txt, c.res, res, err)
		}
	}
}

func TestDNSPermanentErrors(t *testing.T) {
	t.Parallel()

	dns := newTestResolver()
	dnsError := &net.DNSError{
		Err:         "permanent error for testing",
		IsTemporary: false,
	}

	// Domain "tmperr" will fail resolution with a temporary error.
	dns.Errors["tmperr"] = dnsError
	dns.Errors["1.1.1.1"] = dnsError
	dns.Mx["tmpmx"] = []*net.MX{mx("tmperr", 10)}

	cases := []struct {
		txt string
		res spf.Result
	}{
		{"v=spf1 include:tmperr", spf.ResultPermError},
		{"v=spf1 a:tmperr", spf.ResultNeutral},
		{"v=spf1 mx:tmperr", spf.ResultNeutral},
		{"v=spf1 ptr:tmperr", spf.ResultNeutral},
		{"v=spf1 mx:tmpmx", spf.ResultNeutral},
	}

	for _, c := range cases {
		dns.Txt["domain"] = []string{c.txt}
		res, err := spf.Check(context.TODO(), ip1111, "domain", "", &spf.CheckOptions{
			Resolver: dns,
		})
		if res != c.res {
			t.Errorf("%q: expected %v, got %v (%v)",
				c.txt, c.res, res, err)
		}
	}
}

func TestMacros(t *testing.T) {
	t.Parallel()

	dns := newTestResolver()

	// Most of the cases are covered by the standard test suite, so this is
	// targeted at gaps in coverage.
	cases := []struct {
		txt string
		res spf.Result
		err error
	}{
		{"v=spf1 ptr:%{fff} -all", spf.ResultPermError, errInvalidMacro},
		{"v=spf1 mx:%{fff} -all", spf.ResultPermError, errInvalidMacro},
		{"v=spf1 redirect=%{fff}", spf.ResultPermError, errInvalidMacro},
		{"v=spf1 a:%{o0}", spf.ResultPermError, errInvalidMacro},
		{"v=spf1 +a:sss-%{s}-sss", spf.ResultPass, nil},
		{"v=spf1 +a:ooo-%{o}-ooo", spf.ResultPass, nil},
		{"v=spf1 +a:OOO-%{O}-OOO", spf.ResultPass, nil},
		{"v=spf1 +a:ppp-%{p}-ppp", spf.ResultPass, nil},
		{"v=spf1 +a:vvv-%{v}-vvv", spf.ResultPass, nil},
		{"v=spf1 a:%{x}", spf.ResultPermError, errInvalidMacro},
		{"v=spf1 +a:ooo-%{o7}-ooo", spf.ResultPass, nil},
		{"v=spf1 exists:%{ir}.vvv -all", spf.ResultFail, nil},
	}

	dns.Ip["sss-user@domain-sss"] = []net.IP{ip6666}
	dns.Ip["ooo-domain-ooo"] = []net.IP{ip6666}
	dns.Ip["ppp-unknown-ppp"] = []net.IP{ip6666}
	dns.Ip["vvv-ip6-vvv"] = []net.IP{ip6666}
	dns.Ip["8.6.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.8.b.d.0.1.0.0.2.vvv"] = []net.IP{ip1111}

	for _, c := range cases {
		dns.Txt["domain"] = []string{c.txt}
		res, err := spf.Check(context.TODO(), ip6666, "domain", "user@domain", &spf.CheckOptions{
			Resolver: dns,
		})
		if (res == spf.ResultTempError || res == spf.ResultPermError) && (err == nil) {
			t.Errorf("%q: expected error, got nil", c.txt)
		}
		if res != c.res {
			t.Errorf("%q: expected %q, got %q", c.txt, c.res, res)
		}
		if !errorsEqual(err, c.err) {
			t.Errorf("%q: expected error [%v], got [%v]", c.txt, c.err, err)
		}
	}
}

func TestMacrosV4(t *testing.T) {
	t.Parallel()

	dns := newTestResolver()

	// Like TestMacros above, but specifically for IPv4.
	// It's easier to have a separate suite.
	// While at it, test some of the reversals, for variety.
	cases := []struct {
		txt string
		res spf.Result
		err error
	}{
		{"v=spf1 +a:sr-%{sr}-sr", spf.ResultPass, nil},
		{"v=spf1 +a:sra-%{sr.}-sra", spf.ResultPass, nil},
		{"v=spf1 +a:o7-%{o7}-o7", spf.ResultPass, nil},
		{"v=spf1 +a:o1-%{o1}-o1", spf.ResultPass, nil},
		{"v=spf1 +a:o1r-%{o1r}-o1r", spf.ResultPass, nil},
		{"v=spf1 +a:vvv-%{v}-vvv", spf.ResultPass, nil},
	}

	dns.Ip["sr-com.user@example-sr"] = []net.IP{ip1111}
	dns.Ip["sra-com.user@example-sra"] = []net.IP{ip1111}
	dns.Ip["o7-example.com-o7"] = []net.IP{ip1111}
	dns.Ip["o1-com-o1"] = []net.IP{ip1111}
	dns.Ip["o1r-example-o1r"] = []net.IP{ip1111}
	dns.Ip["vvv-in-addr-vvv"] = []net.IP{ip1111}

	for _, c := range cases {
		dns.Txt["example.com"] = []string{c.txt}
		res, err := spf.Check(context.TODO(), ip1111, "example.com", "user@example.com", &spf.CheckOptions{
			Resolver: dns,
		})
		if (res == spf.ResultTempError || res == spf.ResultPermError) && (err == nil) {
			t.Errorf("%q: expected error, got nil", c.txt)
		}
		if res != c.res {
			t.Errorf("%q: expected %q, got %q", c.txt, c.res, res)
		}
		if !errorsEqual(err, c.err) {
			t.Errorf("%q: expected error [%v], got [%v]", c.txt, c.err, err)
		}
	}
}

func mx(host string, pref uint16) *net.MX {
	return &net.MX{Host: host, Pref: pref}
}

// func mkDM(v4, v6 int) dualMasks {
// 	return dualMasks{net.CIDRMask(v4, 32), net.CIDRMask(v6, 128)}
// }

// func TestIPMatchHelper(t *testing.T) {
// 	cases := []struct {
// 		ip      net.IP
// 		tomatch net.IP
// 		masks   dualMasks
// 		ok      bool
// 	}{
// 		{ip1111, ip1110, mkDM(24, -1), true},
// 		{ip1111, ip1111, mkDM(-1, -1), true},
// 		{ip1111, ip1110, mkDM(-1, -1), false},
// 		{ip1111, ip1110, mkDM(32, -1), false},
// 		{ip1111, ip1110, mkDM(99, -1), false},

// 		{ip6666, ip6660, mkDM(-1, 100), true},
// 		{ip6666, ip6666, mkDM(-1, -1), true},
// 		{ip6666, ip6660, mkDM(-1, -1), false},
// 		{ip6666, ip6660, mkDM(-1, 128), false},
// 		{ip6666, ip6660, mkDM(-1, 200), false},
// 	}
// 	for _, c := range cases {
// 		ok := ipMatch(c.ip, c.tomatch, c.masks)
// 		if ok != c.ok {
// 			t.Errorf("[%s %s/%v]: expected %v, got %v",
// 				c.ip, c.tomatch, c.masks, c.ok, ok)
// 		}
// 	}
// }

// func TestInvalidMacro(t *testing.T) {
// 	// Test that the macro expansion detects some invalid macros.
// 	macros := []string{
// 		"%{x}", "%{z}", "%{c}", "%{r}", "%{t}",
// 	}
// 	for _, macro := range macros {
// 		r := spf{
// 			ip:          ip1111,
// 			resolutions: 0,
// 			sender:      "example.com",
// 		}

// 		out, err := r.expandMacros(macro, "example.com")
// 		if out != "" || !errorsEqual(err, errInvalidMacro) {
// 			t.Errorf(`[%s]:expected ""/%v, got %q/%v`,
// 				macro, errInvalidMacro, out, err)
// 		}
// 	}
// }

// Test some corner cases when resolver.LookupIPAddr returns an invalid
// address. This can happen if using a buggy custom resolver.
func TestBadResolverResponse(t *testing.T) {
	t.Parallel()

	dns := newTestResolver()

	// When LookupIPAddr returns an invalid ip, for an "a" field.
	dns.Ip["domain1"] = []net.IP{nil}
	dns.Txt["domain1"] = []string{"v=spf1 a:domain1 -all"}
	res, err := spf.Check(context.TODO(), ip1111, "domain1", "user@domain1", &spf.CheckOptions{
		Resolver: dns,
	})
	if res != spf.ResultFail {
		t.Errorf("expected fail, got %q / %q", res, err)
	}

	// Same as above, except the field has a mask.
	dns.Ip["domain1"] = []net.IP{nil}
	dns.Txt["domain1"] = []string{"v=spf1 a:domain1//24 -all"}
	res, err = spf.Check(context.TODO(), ip1111, "domain1", "user@domain1", &spf.CheckOptions{
		Resolver: dns,
	})
	if res != spf.ResultFail {
		t.Errorf("expected fail, got %q / %q", res, err)
	}

	// When LookupIPAddr returns an invalid ip, for an "mx" field.
	dns.Ip["mx.domain1"] = []net.IP{nil}
	dns.Mx["domain1"] = []*net.MX{mx("mx.domain1", 5)}
	dns.Txt["domain1"] = []string{"v=spf1 mx:domain1 -all"}
	res, err = spf.Check(context.TODO(), ip1111, "domain1", "user@domain1", &spf.CheckOptions{
		Resolver: dns,
	})
	if res != spf.ResultFail {
		t.Errorf("expected fail, got %q / %q", res, err)
	}

	// Same as above, except the field has a mask.
	dns.Ip["mx.domain1"] = []net.IP{nil}
	dns.Mx["domain1"] = []*net.MX{mx("mx.domain1", 5)}
	dns.Txt["domain1"] = []string{"v=spf1 mx:domain1//24 -all"}
	res, err = spf.Check(context.TODO(), ip1111, "domain1", "user@domain1", &spf.CheckOptions{
		Resolver: dns,
	})
	if res != spf.ResultFail {
		t.Errorf("expected fail, got %q / %q", res, err)
	}
}
