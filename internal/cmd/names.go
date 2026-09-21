package cmd

import (
	"context"

	"github.com/ziyan/teanode/internal/client"
)

// domainNames maps domain identifiers to names, for lists that carry the
// identifier. A domain added through the API has its name for an identifier
// and needs no help; one created on a first run has a generated one, and a
// column of those is a column nobody can read.
//
// Fetched once per command. When the list cannot be read the identifiers are
// shown as they are, because the list this decorates has already been read
// and is worth more than the decoration.
func domainNames(ctx context.Context, connection *client.Client) map[string]string {
	names := map[string]string{}
	domains, err := client.ListDomains(ctx, connection)
	if err != nil {
		return names
	}
	for _, domain := range domains {
		names[domain.ID] = domain.Domain
	}
	return names
}

// domainName is the name for an identifier, or the identifier when the
// domain is no longer configured — mail received for a deleted domain is
// kept, and still says which domain it was for.
func domainName(names map[string]string, domainId string) string {
	if name, ok := names[domainId]; ok {
		return name
	}
	return domainId
}
