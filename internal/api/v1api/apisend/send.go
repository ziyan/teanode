package apisend

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"net"
	"net/http"
	"net/mail"
	"strings"

	"github.com/gorilla/mux"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/mailparse"
	"github.com/ziyan/teanode/internal/util/security"
)

func (self *send) sendView(response http.ResponseWriter, request *http.Request) {
	if err := self.database.Transaction(func(tx db.Transaction) error {
		// decode authorization
		authorization := strings.Fields(request.Header.Get("Authorization"))
		if len(authorization) != 2 || authorization[0] != "Basic" {
			return api.ErrInvalidCredential
		}
		decoded, err := base64.StdEncoding.DecodeString(authorization[1])
		if err != nil {
			return api.ErrInvalidCredential
		}
		decodedParts := strings.SplitN(string(decoded), ":", 2)
		if len(decodedParts) != 2 {
			return api.ErrInvalidCredential
		}
		credentialId, credentialKey, err := security.DecodeCredential(decodedParts[0], decodedParts[1], self.settings.Secret)
		if err != nil {
			return api.ErrInvalidCredential
		}
		var credential *models.Credential
		if err := self.database.Transaction(func(tx db.Transaction) error {
			var err error
			credential, err = tx.GetCredential(credentialId)
			return err
		}); err != nil {
			return err
		}
		if credential == nil || credential.Disabled || credential.Key != credentialKey {
			return api.ErrInvalidCredential
		}

		// parse the input
		var data struct {
			Sender     string   `json:"sender"`
			Recipients []string `json:"recipients"`

			// Locale the recipient should read, such as "zh-CN". The
			// template's closest translation is used; its default when it
			// has none, or when this is empty.
			Locale string `json:"locale"`

			Variables map[string]interface{} `json:"variables"`
		}
		if err := json.NewDecoder(request.Body).Decode(&data); err != nil {
			return api.ErrInvalidArguments
		}

		// parse the sender address
		parsedSender, err := mail.ParseAddress(data.Sender)
		if err != nil {
			return api.ErrInvalidArguments
		}
		senderAlias, senderDomain := mailparse.SplitAddress(parsedSender.Address)
		requestVariables := mux.Vars(request)
		if senderDomain != requestVariables["domain"] {
			return api.ErrPermissionDenied
		}
		if credential.Alias != "" && credential.Alias != senderAlias {
			return api.ErrInvalidCredential
		}

		// parse the receipient addresses
		recipients := make([]string, 0, len(data.Recipients))
		for _, recipient := range data.Recipients {
			parsedRecipient, err := mail.ParseAddress(recipient)
			if err != nil {
				return api.ErrInvalidArguments
			}
			recipients = append(recipients, parsedRecipient.Address)
		}
		if len(recipients) == 0 {
			return api.ErrInvalidArguments
		}

		ip := net.ParseIP(request.RemoteAddr)
		return self.mailer.SendMail(request.Context(), &mailparse.Envelope{
			Sender:        parsedSender.Address,
			Recipients:    recipients,
			IP:            ip,
			Location:      self.locator.Locate(ip),
			CredentialID:  credentialId,
			CredentialKey: credentialKey,
			TLS:           request.TLS,
		}, requestVariables["template"], data.Locale, data.Variables)
	}); err != nil {
		switch err {
		case api.ErrInvalidArguments:
			http.Error(response, err.Error(), http.StatusBadRequest)
		case api.ErrInvalidCredential, api.ErrInvalidToken:
			http.Error(response, err.Error(), http.StatusUnauthorized)
		case api.ErrNotFound:
			http.Error(response, err.Error(), http.StatusNotFound)
		case api.ErrPermissionDenied:
			http.Error(response, err.Error(), http.StatusForbidden)
		default:
			log.Errorf("failed to execute request: %s", err)
			http.Error(response, fmt.Sprintf("failed to execute request: %s", err), http.StatusInternalServerError)
		}
		return
	}

	var result interface{}
	response.Header().Set("Content-Type", mime.FormatMediaType("application/json", map[string]string{"charset": "utf-8"}))
	response.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(response).Encode(result); err != nil {
		log.Errorf("failed to encode response: %s", err)
		return
	}
}
