package mailparse

import (
	"io"
	"mime"
	"net/textproto"
	"strings"
)

// AttachmentNames lists the file names of a message's attachments: every
// part carrying a name that is not displayed inline. What the reading pane
// lists, and what a search for "has attachment" counts.
func AttachmentNames(headers []string, body []byte) []string {
	var names []string
	_ = TraverseParts(headers, body, func(header textproto.MIMEHeader, reader io.Reader) error {
		_, parameters, err := mime.ParseMediaType(header.Get("Content-Type"))
		if err != nil {
			parameters = map[string]string{}
		}
		filename := parameters["name"]
		disposition := header.Get("Content-Disposition")
		if disposition != "" {
			if _, dispositionParameters, err := mime.ParseMediaType(disposition); err == nil && dispositionParameters["filename"] != "" {
				filename = dispositionParameters["filename"]
			}
		}
		if filename != "" && !strings.HasPrefix(strings.ToLower(disposition), "inline") {
			names = append(names, DecodeHeaderValue(filename))
		}
		return nil
	})
	return names
}
