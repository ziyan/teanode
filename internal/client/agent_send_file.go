package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// DocumentSendAgentFileToMainChat names a file to send to the person's main
// chat and answers where its bytes go.
const DocumentSendAgentFileToMainChat = `mutation ($name: String!, $contentType: String, $caption: String) {
  SendAgentFileToMainChat(name: $name, contentType: $contentType, caption: $caption) { attachmentId name uploadUrl maxBytes }
}`

// AgentFileUpload is a file waiting for its bytes, and where to send them.
type AgentFileUpload struct {
	AttachmentID string `json:"attachmentId"`
	Name         string `json:"name"`
	UploadURL    string `json:"uploadUrl"`
	MaxBytes     int64  `json:"maxBytes"`
}

// sendFileUploadTimeout is how long one file's bytes may take to go up: a
// hundred megabytes over a slow line.
const sendFileUploadTimeout = 10 * time.Minute

// SendAgentFileToMainChat sends the person a file in their main chat, with a
// line to go with it: the file is named, then its bytes go to the address the
// server answered. It is posted when they arrive.
func SendAgentFileToMainChat(ctx context.Context, connection *Client, name, contentType, caption string, content []byte) (*AgentFileUpload, error) {
	if connection.readOnly {
		return nil, &ReadOnlyError{URL: connection.url}
	}
	var result struct {
		SendAgentFileToMainChat *AgentFileUpload `json:"SendAgentFileToMainChat"`
	}
	variables := map[string]any{"name": name, "contentType": contentType, "caption": caption}
	if err := connection.Execute(ctx, DocumentSendAgentFileToMainChat, variables, &result); err != nil {
		return nil, err
	}
	upload := result.SendAgentFileToMainChat
	if upload == nil {
		return nil, errors.New("the server answered no upload address")
	}
	if upload.MaxBytes > 0 && int64(len(content)) > upload.MaxBytes {
		return nil, fmt.Errorf("%s is %d bytes, more than the %d a file sent to the main chat may be", name, len(content), upload.MaxBytes)
	}
	// The path and the signature of the address the server made, sent to
	// the server this command reaches: the address names the server as the
	// request reached it, which behind a proxy may not be how this machine
	// reaches it.
	address, err := url.Parse(upload.UploadURL)
	if err != nil {
		return nil, fmt.Errorf("the server answered an upload address that cannot be read: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, connection.url+address.RequestURI(), bytes.NewReader(content))
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	uploadClient := *connection.client
	uploadClient.Timeout = sendFileUploadTimeout
	response, err := uploadClient.Do(request)
	if err != nil {
		return nil, &ConnectionError{URL: connection.url, Cause: err}
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		var answer struct {
			Error string `json:"error"`
		}
		if json.NewDecoder(response.Body).Decode(&answer) == nil && answer.Error != "" {
			return nil, errors.New(answer.Error)
		}
		return nil, fmt.Errorf("%s answered HTTP %d", connection.url, response.StatusCode)
	}
	return upload, nil
}
