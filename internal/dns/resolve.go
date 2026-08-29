package dns

import (
	"context"
	"sort"
	"strings"

	"github.com/miekg/dns"
)

func (self *verifier) resolveMx(ctx context.Context, fqdn string) ([]uint16, []string, error) {
	request := new(dns.Msg)
	request.SetQuestion(dns.Fqdn(fqdn), dns.TypeMX)
	result, _, err := self.client.ExchangeContext(ctx, request, self.settings.Nameserver)
	if err != nil {
		log.Errorf("failed to resolve mx records for %q: %s", fqdn, err)
		return nil, nil, err
	}
	sort.Slice(result.Answer, func(i, j int) bool {
		if mx1, ok := result.Answer[i].(*dns.MX); ok {
			if mx2, ok := result.Answer[j].(*dns.MX); ok {
				return mx1.Preference < mx2.Preference
			}
		}
		return false
	})
	preferences := make([]uint16, 0, len(result.Answer))
	records := make([]string, 0, len(result.Answer))
	for _, answer := range result.Answer {
		if mx, ok := answer.(*dns.MX); ok {
			preferences = append(preferences, mx.Preference)
			records = append(records, mx.Mx)
		}
	}
	return preferences, records, nil
}

func (self *verifier) resolveCname(ctx context.Context, fqdn string) (string, error) {
	request := new(dns.Msg)
	request.SetQuestion(dns.Fqdn(fqdn), dns.TypeCNAME)
	result, _, err := self.client.ExchangeContext(ctx, request, self.settings.Nameserver)
	if err != nil {
		log.Errorf("failed to resolve cname record for %q: %s", fqdn, err)
		return "", err
	}
	if len(result.Answer) > 0 {
		if cname, ok := result.Answer[0].(*dns.CNAME); ok {
			return cname.Target, nil
		}
	}
	return "", nil
}

func (self *verifier) resolveTxt(ctx context.Context, fqdn string) ([]string, error) {
	request := new(dns.Msg)
	request.SetQuestion(dns.Fqdn(fqdn), dns.TypeTXT)
	result, _, err := self.client.ExchangeContext(ctx, request, self.settings.Nameserver)
	if err != nil {
		log.Errorf("failed to resolve txt records for %q: %s", fqdn, err)
		return nil, err
	}
	records := make([]string, 0, len(result.Answer))
	for _, answer := range result.Answer {
		if txt, ok := answer.(*dns.TXT); ok {
			// A long value arrives split into 255 byte chunks and has to be
			// joined back together before it means anything.
			records = append(records, strings.Join(txt.Txt, ""))
		}
	}
	sort.Strings(records)
	return records, nil
}

// resolveAddresses returns the addresses a name resolves to, following a CNAME
// if there is one. A mail host may be published either way.
func (self *verifier) resolveAddresses(ctx context.Context, fqdn string) ([]string, error) {
	var addresses []string
	for _, recordType := range []uint16{dns.TypeA, dns.TypeAAAA} {
		request := new(dns.Msg)
		request.SetQuestion(dns.Fqdn(fqdn), recordType)
		result, _, err := self.client.ExchangeContext(ctx, request, self.settings.Nameserver)
		if err != nil {
			log.Errorf("failed to resolve address records for %q: %s", fqdn, err)
			return nil, err
		}
		for _, answer := range result.Answer {
			switch record := answer.(type) {
			case *dns.A:
				addresses = append(addresses, record.A.String())
			case *dns.AAAA:
				addresses = append(addresses, record.AAAA.String())
			}
		}
	}
	sort.Strings(addresses)
	return addresses, nil
}
