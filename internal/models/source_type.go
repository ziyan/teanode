package models

import "time"

// AgentSourceType is a source type installed on this server: the file that
// says how to read one kind of knowledge source, with what the registry
// said about it, or an operator's own file, marked local.
type AgentSourceType struct {
	Name       string    `json:"name"`
	CreatedAt  time.Time `json:"createdAt"`
	ModifiedAt time.Time `json:"modifiedAt"`

	Version     string `json:"version"`
	Publisher   string `json:"publisher"`
	URL         string `json:"url"`
	SHA256      string `json:"sha256"`
	Description string `json:"description"`

	// Content is the file as it was fetched and checked, or as the
	// operator gave it.
	Content string `json:"-"`

	// IsLocal says the type was added from a file rather than installed
	// from the registry, so nothing but the operator vouches for it.
	IsLocal bool `json:"isLocal"`
}
