package dmarc

import (
	"context"
	"fmt"
	"net"
	"strings"
)

type Resolver interface {
	LookupTXT(ctx context.Context, domain string) ([]string, error)
}

type LookupOptions struct {
	Resolver Resolver
}

// Lookup queries a DMARC record for a specified domain.
func Lookup(ctx context.Context, domain string, options *LookupOptions) (*Record, error) {
	record := fmt.Sprintf("_dmarc.%s", domain)
	var resolver Resolver
	if options != nil && options.Resolver != nil {
		resolver = options.Resolver
	} else {
		resolver = net.DefaultResolver
	}
	txts, err := resolver.LookupTXT(ctx, record)
	if err != nil {
		if dnsErr, ok := err.(*net.DNSError); ok && dnsErr.IsNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("dmarc: failed to lookup txt record %q: %w", record, err)
	}
	// Only a DMARC record counts, RFC 7489 §6.6.3: a name carries other TXT
	// records too — an ownership proof, say — and one of those joined onto
	// the policy made it unreadable, which refused every message from the
	// domain. More than one DMARC record is no record.
	var records []string
	for _, txt := range txts {
		if strings.HasPrefix(strings.TrimSpace(txt), "v=DMARC1") {
			records = append(records, txt)
		}
	}
	if len(records) == 0 {
		return nil, nil
	}
	if len(records) > 1 {
		return nil, nil
	}
	return Parse(records[0])
}
