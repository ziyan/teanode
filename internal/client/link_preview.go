package client

import "context"

// What the pages behind links say about themselves: the same operation as the
// card under a message in the dashboard and the iPhone app.

// LinkPreview is one link's preview. When IsAvailable is false the page could
// not be read, and only the link, cleaned, and its short text are filled in.
type LinkPreview struct {
	URL               string  `json:"url"`
	CleanedURL        string  `json:"cleanedUrl"`
	DisplayText       string  `json:"displayText"`
	IsAvailable       bool    `json:"isAvailable"`
	Title             string  `json:"title"`
	Description       string  `json:"description"`
	SiteName          string  `json:"siteName"`
	ImageURL          *string `json:"imageUrl"`
	ImageWidthPixels  *int    `json:"imageWidthPixels"`
	ImageHeightPixels *int    `json:"imageHeightPixels"`
}

// MostLinkPreviewURLs is how many links one question may be about.
const MostLinkPreviewURLs = 10

const DocumentReadLinkPreviews = `query ($urls: [String!]!) {
  ReadLinkPreviews(urls: $urls) {
    url cleanedUrl displayText isAvailable title description siteName imageUrl imageWidthPixels imageHeightPixels
  }
}`

// ReadLinkPreviews is the preview of each link, in the order given.
func ReadLinkPreviews(ctx context.Context, connection *Client, urls []string) ([]*LinkPreview, error) {
	var result struct {
		ReadLinkPreviews []*LinkPreview `json:"ReadLinkPreviews"`
	}
	if err := connection.Execute(ctx, DocumentReadLinkPreviews, map[string]any{"urls": urls}, &result); err != nil {
		return nil, err
	}
	return result.ReadLinkPreviews, nil
}
