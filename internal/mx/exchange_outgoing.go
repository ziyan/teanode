package mx

import (
	"context"
	"fmt"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/authres"
	"github.com/ziyan/teanode/internal/util/dkim"
	"github.com/ziyan/teanode/internal/util/mailparse"
)

func (self *exchange) handleOutgoing(ctx context.Context, tx db.Transaction, envelope *mailparse.Envelope) ([]*models.Delivery, error) {
	// figure out sender domain
	senderAlias, senderDomain := mailparse.SplitAddress(envelope.Sender)

	// extract some important headers
	from, err := mailparse.ParseAddress(mailparse.DecodeHeaderValue(mailparse.FindHeaderValue(envelope.Headers, "From")))
	if err != nil {
		return nil, mailparse.ErrInvalidFromHeader
	}
	subject := mailparse.DecodeHeaderValue(mailparse.FindHeaderValue(envelope.Headers, "Subject"))
	messageId := mailparse.DecodeHeaderValue(mailparse.FindHeaderValue(envelope.Headers, "Message-ID"))

	// find the credential in the configuration
	configuration := self.config.Current()
	var credential *config.Credential
	var domain *config.Domain
	if envelope.CredentialID != "" {
		domain, credential = configuration.FindCredential(envelope.CredentialID)
		if credential == nil || credential.Disabled {
			return nil, mailparse.ErrInvalidCredentials
		}
		if credential.Key != envelope.CredentialKey {
			return nil, mailparse.ErrInvalidCredentials
		}
		// A credential restricted to one local part may not send as another.
		if credential.Alias != "" && credential.Alias != senderAlias {
			return nil, mailparse.ErrInvalidCredentials
		}
		if envelope.DomainID != "" && envelope.DomainID != domain.ID {
			return nil, mailparse.ErrInvalidCredentials
		}
		envelope.DomainID = domain.ID
	} else {
		domain = configuration.FindDomainByID(envelope.DomainID)
	}
	if domain == nil || domain.Domain != senderDomain {
		return nil, mailparse.ErrInvalidCredentials
	}

	// add Received header
	receivedHeader := self.formatReceivedHeader(envelope)

	// add Authentication-Results
	var authenticationResultsHeader string
	if credential != nil {
		authenticationResultsHeader = mailparse.UnsplitHeader("Authentication-Results", authres.Format(self.receivedBy(envelope), []authres.Result{
			&authres.AuthResult{
				Value: authres.ResultPass,
				Auth:  fmt.Sprintf("%s@%s", credential.ID, domain.Domain),
			},
		}))
	} else {
		authenticationResultsHeader = mailparse.UnsplitHeader("Authentication-Results", authres.Format(self.receivedBy(envelope), []authres.Result{
			&authres.AuthResult{
				Value: authres.ResultPass,
				Auth:  fmt.Sprintf("@%s", domain.Domain),
			},
		}))
	}

	// Sign with this domain's own key. A domain with no key still sends, but
	// unsigned, which receivers may treat with suspicion — so say so once,
	// loudly enough to be noticed.
	var signatureHeaders []string
	if signer, selector, ok := configuration.SignerFor(domain); ok {
		signatureHeaders, err = dkim.Sign(envelope.Headers, envelope.Body, &dkim.SignOptions{
			Domain:   domain.Domain,
			Selector: selector,
			Signer:   signer,
		})
		if err != nil {
			return nil, err
		}
	} else {
		log.Warningf("domain %q has no signing key, so this message is unsigned and may be treated as spam", domain.Domain)
	}

	// combine the headers
	headers := mailparse.MergeHeaders(signatureHeaders, []string{
		authenticationResultsHeader,
		receivedHeader,
	}, envelope.Headers)

	// save mail
	mail := &models.Mail{
		DomainID:       domain.ID,
		Domain:         domain,
		EnvelopeID:     envelope.ID,
		Hello:          envelope.Hello,
		IP:             envelope.IP.String(),
		RDNS:           envelope.RDNS,
		TLSVersion:     getTlsVersion(envelope.TLS),
		TLSCipherSuite: getTlsCipherSuite(envelope.TLS),
		Location:       envelope.Location,
		Sender:         envelope.Sender,
		Recipients:     envelope.Recipients,
		MessageID:      messageId,
		From:           from,
		Subject:        subject,
		Headers:        headers,
		Body:           envelope.Body,
		Size:           envelope.Size,
		ReceivedAt:     envelope.ReceivedAt,
		Kind:           models.MailKindOutgoing,
	}
	if credential != nil {
		mail.CredentialID = credential.ID
		mail.Credential = credential
	}

	if err := self.authenticateOutgoing(ctx, envelope, mail, domain); err != nil {
		log.Warningf("failed to authenticate outgoing mail %q, rejecting: %s", envelope.ID, err)

		// try to save mail before exiting
		if _, err := tx.CreateMail(mail, nil); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}

		// Refused outgoing mail is kept for the same reason as refused
		// incoming: the row says it was rejected, the message says whether
		// that was right.
		self.store(ctx, mail)

		// track usages
		self.trackDomainUsage(envelope.ReceivedAt, domain.ID, domainUsage{
			bytesReceived: envelope.Size,
			mailsRejected: 1,
		})
		if credential != nil {
			self.trackCredentialUsage(envelope.ReceivedAt, credential.ID, credentialUsage{
				bytesReceived: envelope.Size,
				mailsRejected: 1,
			})
		}
		return nil, err
	}

	// save mail and commit
	if _, err := tx.CreateMail(mail, nil); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	// track usages
	self.trackDomainUsage(envelope.ReceivedAt, domain.ID, domainUsage{
		bytesReceived: envelope.Size,
		mailsAccepted: 1,
	})
	if credential != nil {
		self.trackCredentialUsage(envelope.ReceivedAt, credential.ID, credentialUsage{
			bytesReceived: envelope.Size,
			mailsAccepted: 1,
		})
	}

	// create a delivery for each recipient
	recipientDomains := make(map[string]*config.Domain) // example.com -> configured domain
	recipientMails := make(map[string]*models.Mail)     // example.com -> mail model (for lookup cache)
	var deliveries []*models.Delivery
	for _, recipient := range envelope.Recipients {
		recipientAlias, recipientDomainName := mailparse.SplitAddress(recipient)
		if recipientDomainName == domain.Domain {
			// looping back to same domain
			matchedDeliveries, err := self.matchAliases(domain, recipientAlias, mail)
			if err != nil {
				return nil, err
			}
			deliveries = append(deliveries, matchedDeliveries...)
			continue
		}

		// look up domain
		recipientDomain, ok := recipientDomains[recipientDomainName]
		if !ok {
			recipientDomain = configuration.FindDomain(recipientDomainName)
			recipientDomains[recipientDomainName] = recipientDomain
		}

		// A recipient at a domain this server does not serve is external and
		// goes out over SMTP; one at a configured domain loops back inside.
		if recipientDomain == nil {
			deliveries = append(deliveries, &models.Delivery{
				MailID:    mail.ID,
				Mail:      mail,
				Recipient: recipient,
				Kind:      models.DeliveryKindExternal,
			})
			continue
		}

		// create delivery for internal, do not need to queue
		recipientDelivery, err := tx.CreateDelivery(&models.Delivery{
			MailID:      mail.ID,
			Mail:        mail,
			Recipient:   recipient,
			Kind:        models.DeliveryKindInternal,
			Status:      models.DeliveryStatusDelivered,
			DeliveredAt: &envelope.ReceivedAt,
		}, nil)
		if err != nil {
			return nil, err
		}

		// create copy of mail for this recipient domain
		recipientMail, ok := recipientMails[recipientDomainName]
		if !ok {
			m, err := tx.CreateMail(&models.Mail{
				DomainID:              recipientDomain.ID,
				Domain:                recipientDomain,
				DeliveryID:            recipientDelivery.ID,
				Delivery:              recipientDelivery,
				EnvelopeID:            mail.EnvelopeID,
				Hello:                 mail.Hello,
				IP:                    mail.IP,
				RDNS:                  mail.RDNS,
				TLSVersion:            mail.TLSVersion,
				TLSCipherSuite:        mail.TLSCipherSuite,
				Location:              mail.Location,
				Sender:                mail.Sender,
				Recipients:            mail.Recipients,
				MessageID:             mail.MessageID,
				From:                  mail.From,
				Subject:               mail.Subject,
				Headers:               mail.Headers,
				Body:                  mail.Body,
				Size:                  mail.Size,
				Status:                mail.Status,
				AuthenticationResults: mail.AuthenticationResults,
				ReceivedAt:            mail.ReceivedAt,
				Kind:                  models.MailKindExchange,
			}, nil)
			if err != nil {
				return nil, err
			}
			recipientMails[recipientDomainName] = m
			recipientMail = m
		}

		// track usages
		self.trackDomainUsage(envelope.ReceivedAt, domain.ID, domainUsage{
			bytesSent:           envelope.Size,
			deliveriesSucceeded: 1,
		})
		self.trackDomainUsage(envelope.ReceivedAt, recipientDomain.ID, domainUsage{
			bytesReceived: envelope.Size,
			mailsAccepted: 1,
		})

		// recipient is internal, need to resolve alias
		matchedDeliveries, err := self.matchAliases(recipientDomain, recipientAlias, recipientMail)
		if err != nil {
			return nil, err
		}
		deliveries = append(deliveries, matchedDeliveries...)
	}

	return tx.CreateDeliveries(deliveries, nil)
}

func (self *exchange) authenticateOutgoing(ctx context.Context, envelope *mailparse.Envelope, mail *models.Mail, domain *config.Domain) error {
	start := time.Now()
	mail.Status = models.MailStatusRejected

	// prepare parallel checks
	authenticator := newAuthenticator(ctx)
	defer authenticator.stop()

	if envelope.CredentialID != "" {
		self.checkVirus(authenticator, mail.Headers, mail.Body)
	}
	self.checkContent(authenticator, mail.Headers, mail.Body)

	// wait for all
	authenticationResults, _, err := authenticator.wait()
	mail.AuthenticationResults = authenticationResults
	if err != nil {
		return err
	}

	// then score, which reads what those checks established
	if envelope.CredentialID != "" {
		if err := self.checkSpam(ctx, envelope, mail, domain.SpamFilterScoreThreshold); err != nil {
			return err
		}
	}

	// change status
	mail.Status = models.MailStatusAccepted

	log.Debugf("took %s to authenticate outgoing mail %q", time.Since(start), envelope.ID)
	return nil
}
