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
	if len(txts) == 0 {
		return nil, nil
	}

	// long keys are split in multiple parts
	txt := strings.Join(txts, "")
	return Parse(txt)
}
