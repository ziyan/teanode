package mx

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"io"
	"mime"
	"net"
	"net/textproto"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/dmarc"
	"github.com/ziyan/teanode/internal/util/geoip"
	"github.com/ziyan/teanode/internal/util/mailparse"
)

func (self *exchange) handleRua(ctx context.Context, tx db.Transaction, envelope *mailparse.Envelope) ([]*models.Delivery, error) {
	// extract some important headers
	from, _ := mailparse.ParseAddress(mailparse.DecodeHeaderValue(mailparse.FindHeaderValue(envelope.Headers, "From")))
	subject := mailparse.DecodeHeaderValue(mailparse.FindHeaderValue(envelope.Headers, "Subject"))
	messageId := mailparse.DecodeHeaderValue(mailparse.FindHeaderValue(envelope.Headers, "Message-ID"))

	// parse dmarc feedback
	feedbacks, err := self.decodeDmarcFeedbacks(envelope.Headers, envelope.Body)
	if err != nil {
		return nil, err
	}

	// resolve rdns and location
	ipRdns := make(map[string]string)
	ipLocation := make(map[string]*geoip.Location)
	for _, feedback := range feedbacks {
		for _, record := range feedback.Records {
			ip := net.ParseIP(record.SourceIP)
			if ip == nil {
				continue
			}
			record.SourceIP = ip.String()
			if _, ok := ipRdns[record.SourceIP]; !ok {
				ipRdns[record.SourceIP] = self.checkIp(ctx, ip, 5*time.Second)
			}
			if _, ok := ipLocation[record.SourceIP]; !ok {
				ipLocation[record.SourceIP] = self.locator.Locate(ip)
			}
		}
	}

	// add Received header
	// combine the headers
	headers := mailparse.MergeHeaders([]string{
		self.formatReceivedHeader(envelope),
	}, envelope.Headers)

	// save the mail
	mail, err := tx.CreateMail(&models.Mail{
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
		Kind:           models.MailKindRUA,
	}, nil)
	if err != nil {
		return nil, err
	}

	var reports []*models.Report
	configuration := self.config.Current()
	domains := make(map[string]*config.Domain) // example.com -> configured domain
	for _, feedback := range feedbacks {
		feedbackDomain := strings.Trim(strings.ToLower(feedback.Domain), ".")
		domain, ok := domains[feedbackDomain]
		if !ok {
			domain = configuration.FindDomain(feedbackDomain)
			domains[feedbackDomain] = domain
		}
		if domain == nil {
			continue
		}

		// save a report for each record
		for _, record := range feedback.Records {
			copiedFeedback := *feedback
			copiedFeedback.Records = []dmarc.FeedbackRecord{record}
			report := &models.Report{
				DomainID:     domain.ID,
				MailID:       mail.ID,
				BeginAt:      time.Unix(int64(feedback.Begin), 0),
				EndAt:        time.Unix(int64(feedback.End), 0),
				Count:        record.Count,
				IP:           record.SourceIP,
				RDNS:         ipRdns[record.SourceIP],
				Location:     ipLocation[record.SourceIP],
				FromDomain:   record.HeaderFrom,
				SenderDomain: record.EnvelopeFrom,
				Disposition:  record.Disposition,
				DKIMAligned:  record.DKIM == "pass",
				SPFAligned:   record.SPF == "pass",
				Feedback:     &copiedFeedback,
			}
			if report.FromDomain == "" {
				for _, dkim := range record.DKIMs {
					if dkim.Domain != "" {
						report.FromDomain = dkim.Domain
						break
					}
				}
			}
			if report.SenderDomain == "" {
				for _, spf := range record.SPFs {
					if spf.Domain != "" {
						report.SenderDomain = spf.Domain
						break
					}
				}
			}
			reports = append(reports, report)
		}
	}

	// save reports
	if _, err := tx.CreateReports(reports, nil); err != nil {
		return nil, err
	}

	return nil, nil
}

func (self *exchange) decodeDmarcFeedbacks(headers []string, body []byte) ([]*dmarc.Feedback, error) {
	var feedbacks []*dmarc.Feedback
	if err := mailparse.TraverseParts(headers, body, func(header textproto.MIMEHeader, reader io.Reader) error {
		mediaType, _, err := mime.ParseMediaType(header.Get("Content-Type"))
		if err != nil {
			return mailparse.ErrInvalidContentType
		}
		switch mediaType {
		case "application/gzip", "application/zip":
			switch header.Get("Content-Transfer-Encoding") {
			case "base64":
				reader = base64.NewDecoder(base64.StdEncoding, reader)
			default:
				return mailparse.ErrInvalidTransferEncoding
			}
		}
		switch mediaType {
		case "application/gzip":
			return func() error {
				gzipReader, err := gzip.NewReader(reader)
				if err != nil {
					return err
				}
				defer func() { _ = gzipReader.Close() }()

				feedback, err := dmarc.Decode(gzipReader)
				if err != nil {
					return err
				}
				feedbacks = append(feedbacks, feedback)
				return nil
			}()
		case "application/zip":
			data, err := io.ReadAll(reader)
			if err != nil {
				return err
			}
			zipReader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
			if err != nil {
				return err
			}
			for _, file := range zipReader.File {
				if err := func(file *zip.File) error {
					readerCloser, err := file.Open()
					if err != nil {
						return err
					}
					defer func() { _ = readerCloser.Close() }()

					feedback, err := dmarc.Decode(readerCloser)
					if err != nil {
						return err
					}
					feedbacks = append(feedbacks, feedback)
					return nil
				}(file); err != nil {
					return err
				}
			}
			return nil
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return feedbacks, nil
}
