package mx

import (
	"context"
	"fmt"
	"github.com/ziyan/teanode/internal/access"
	"time"

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

	// find the credential, and the domain it belongs to
	var credential *models.Credential
	var domain *models.Domain
	if envelope.CredentialID != "" {
		credential, err = tx.GetCredential(envelope.CredentialID)
		if err != nil {
			return nil, err
		}
		if credential == nil || credential.Disabled {
			return nil, mailparse.ErrInvalidCredentials
		}
		domain, err = tx.GetDomain(credential.DomainID)
		if err != nil {
			return nil, err
		}
		if domain == nil {
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
	} else if envelope.DomainID != "" {
		domain, err = tx.GetDomain(envelope.DomainID)
		if err != nil {
			return nil, err
		}
	} else if envelope.MailboxID != "" {
		// A person's submission names no domain: the sender address does,
		// and whether it is theirs is checked below.
		domain, err = tx.GetDomainByName(senderDomain)
		if err != nil {
			return nil, err
		}
	}
	if domain == nil || domain.Domain != senderDomain {
		return nil, mailparse.ErrInvalidCredentials
	}
	// A person's submission — from the web UI or with an app password —
	// may only be sent as one of their mailbox's own addresses, and only if
	// they may send at all.
	if envelope.MailboxID != "" {
		mailbox, err := tx.GetMailbox(envelope.MailboxID)
		if err != nil {
			return nil, err
		}
		if mailbox == nil {
			return nil, mailparse.ErrInvalidCredentials
		}
		// Both the envelope sender and the From header: a program can set
		// either, and the recipient reads the header.
		for _, sender := range []string{envelope.Sender, from} {
			allowed, err := access.MailboxMaySend(tx, mailbox, sender)
			if err != nil {
				return nil, err
			}
			if !allowed {
				log.Warningf("mailbox %q may not send as %q, rejecting", mailbox.ID, sender)
				return nil, mailparse.ErrInvalidCredentials
			}
		}
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
	if signer, selector, ok := self.signerFor(domain); ok {
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
	// A person's submission goes in their Sent folder, by reference, in the
	// same transaction as the row: there is no moment at which the message
	// exists and their Sent folder does not know it.
	if envelope.MailboxID != "" {
		if err := self.fileInSent(tx, envelope.MailboxID, mail); err != nil {
			return nil, err
		}
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
	recipientDomains := make(map[string]*models.Domain) // example.com -> configured domain
	var deliveries []*models.Delivery
	for _, recipient := range envelope.Recipients {
		recipientAlias, recipientDomainName := mailparse.SplitAddress(recipient)
		if recipientDomainName == domain.Domain {
			// looping back to same domain
			matchedDeliveries, err := self.matchAliases(tx, domain, recipientAlias, mail)
			if err != nil {
				return nil, err
			}
			deliveries = append(deliveries, matchedDeliveries...)
			continue
		}

		// look up domain
		recipientDomain, ok := recipientDomains[recipientDomainName]
		if !ok {
			recipientDomain, err = tx.GetDomainByName(recipientDomainName)
			if err != nil {
				return nil, err
			}
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
		if _, err := tx.CreateDelivery(&models.Delivery{
			MailID:      mail.ID,
			Mail:        mail,
			Recipient:   recipient,
			Kind:        models.DeliveryKindInternal,
			Status:      models.DeliveryStatusDelivered,
			DeliveredAt: &envelope.ReceivedAt,
		}, nil); err != nil {
			return nil, err
		}

		// The recipient's copy used to be a second row of the same bytes,
		// stored under their domain. It is the same row now: a mailbox at
		// the recipient's address holds a reference to the message that was
		// sent, which is what "no copies" means.
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
		matchedDeliveries, err := self.matchAliases(tx, recipientDomain, recipientAlias, mail)
		if err != nil {
			return nil, err
		}
		deliveries = append(deliveries, matchedDeliveries...)
	}

	return tx.CreateDeliveries(deliveries, nil)
}

func (self *exchange) authenticateOutgoing(ctx context.Context, envelope *mailparse.Envelope, mail *models.Mail, domain *models.Domain) error {
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
		if err := self.checkSpam(ctx, envelope, mail, domain.SpamThreshold()); err != nil {
			return err
		}
	}

	// change status
	mail.Status = models.MailStatusAccepted

	log.Debugf("took %s to authenticate outgoing mail %q", time.Since(start), envelope.ID)
	return nil
}

// fileInSent puts a message a person sent in their Sent folder. A mailbox
// with no Sent folder is being deleted, and the message goes out regardless.
func (self *exchange) fileInSent(tx db.Transaction, mailboxId string, mail *models.Mail) error {
	sent, err := tx.GetFolderByKind(mailboxId, models.MailboxFolderKindSent)
	if err != nil {
		return err
	}
	if sent == nil {
		return nil
	}
	seen := true
	if _, err := tx.AddItem(sent.ID, mail.ID, models.MailboxItemFlags{Seen: &seen}); err != nil {
		return err
	}
	return tx.SetMailSearch(mail.ID, searchDocument(mail))
}
