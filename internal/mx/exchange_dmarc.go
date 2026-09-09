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
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/deferutil"
	"github.com/ziyan/teanode/internal/util/dmarc"
	"github.com/ziyan/teanode/internal/util/geoip"
	"github.com/ziyan/teanode/internal/util/mailparse"
	"github.com/ziyan/teanode/internal/util/security"
)

func (self *exchange) handleRua(ctx context.Context, tx db.Transaction, envelope *mailparse.Envelope) ([]*models.Delivery, error) {
	// extract some important headers
	from, _ := mailparse.ParseAddress(mailparse.DecodeHeaderValue(mailparse.FindHeaderValue(envelope.Headers, "From")))
	subject := mailparse.DecodeHeaderValue(mailparse.FindHeaderValue(envelope.Headers, "Subject"))
	messageId := mailparse.DecodeHeaderValue(mailparse.FindHeaderValue(envelope.Headers, "Message-ID"))

	// The address the report came to names a domain: its report address is
	// derived from the domain's identifier, and that is the domain whose
	// reports arrive there. A report for any other domain is not this
	// address's to take, whoever it is for.
	reportedDomain := self.domainOfReportAddress(envelope.SpecialID)
	if reportedDomain == nil {
		return nil, mailparse.ErrMailBoxUnavailable
	}

	// parse dmarc feedback
	feedbacks, err := self.decodeDmarcFeedbacks(envelope.Headers, envelope.Body)
	if err != nil {
		return nil, err
	}
	kept := feedbacks[:0]
	for _, feedback := range feedbacks {
		feedbackDomain := strings.Trim(strings.ToLower(feedback.Domain), ".")
		if feedbackDomain != strings.ToLower(reportedDomain.Domain) {
			log.Warningf("dropping a dmarc report for %q that arrived at the report address of %q", feedbackDomain, reportedDomain.Domain)
			continue
		}
		kept = append(kept, feedback)
	}
	feedbacks = kept

	// Resolve reverse names and locations, for the addresses the report
	// names. Bounded and concurrent: a reporter lists the sources it saw,
	// and a report written to be expensive lists as many as it likes, each
	// a reverse lookup against a zone its author controls.
	ipRdns := make(map[string]string)
	ipLocation := make(map[string]*geoip.Location)
	for _, feedback := range feedbacks {
		for index := range feedback.Records {
			record := &feedback.Records[index]
			ip := net.ParseIP(record.SourceIP)
			if ip == nil {
				continue
			}
			record.SourceIP = ip.String()
			if _, ok := ipLocation[record.SourceIP]; !ok {
				ipLocation[record.SourceIP] = self.locator.Locate(ip)
			}
			if _, ok := ipRdns[record.SourceIP]; !ok && len(ipRdns) < maximumReportLookups {
				ipRdns[record.SourceIP] = ""
			}
		}
	}
	self.resolveReportAddresses(ctx, ipRdns)

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
	for _, feedback := range feedbacks {
		domain := reportedDomain

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

// Bounds on an aggregate report. A report is compressed, so its size on the
// wire says nothing about its size decoded; the largest reporters send a
// few hundred kilobytes decoded. Each record becomes a row and a lookup.
const (
	maximumReportSize    = 16 * 1024 * 1024
	maximumReportRecords = 10000
	maximumReportFiles   = 16
	maximumReportLookups = 256
	reportLookupTimeout  = 30 * time.Second
	reportLookupWorkers  = 8
)

// domainOfReportAddress is the domain whose report address a message came
// to, from the identifier signed into that address. Nil when it is none of
// them, which an address that verified should not be unless the domain has
// since been removed.
func (self *exchange) domainOfReportAddress(specialId string) *models.Domain {
	for _, domain := range self.allDomains() {
		if domain != nil && security.DerivedULID(self.settings.Secret, "rua:"+domain.ID) == specialId {
			return domain
		}
	}
	return nil
}

// resolveReportAddresses fills in the reverse name of each address, a few
// at a time and all of them within one deadline.
func (self *exchange) resolveReportAddresses(ctx context.Context, ipRdns map[string]string) {
	ctxWithTimeout, cancel := context.WithTimeout(ctx, reportLookupTimeout)
	defer cancel()
	addresses := make(chan string, len(ipRdns))
	for address := range ipRdns {
		addresses <- address
	}
	close(addresses)
	var mutex sync.Mutex
	var waitGroup sync.WaitGroup
	for worker := 0; worker < reportLookupWorkers; worker++ {
		waitGroup.Add(1)
		go func() {
			defer deferutil.Recover()
			defer waitGroup.Done()
			for address := range addresses {
				if ctxWithTimeout.Err() != nil {
					return
				}
				name := self.checkIp(ctxWithTimeout, net.ParseIP(address), 5*time.Second)
				mutex.Lock()
				ipRdns[address] = name
				mutex.Unlock()
			}
		}()
	}
	waitGroup.Wait()
}

// decodeReport reads one decoded report, refusing one larger than any
// reporter sends or with more records than are worth a row each.
func decodeReport(reader io.Reader) (*dmarc.Feedback, error) {
	feedback, err := dmarc.Decode(io.LimitReader(reader, maximumReportSize))
	if err != nil {
		return nil, err
	}
	if len(feedback.Records) > maximumReportRecords {
		return nil, mailparse.ErrInvalidContentType
	}
	return feedback, nil
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

				feedback, err := decodeReport(gzipReader)
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
			if len(zipReader.File) > maximumReportFiles {
				return mailparse.ErrInvalidContentType
			}
			for _, file := range zipReader.File {
				if err := func(file *zip.File) error {
					readerCloser, err := file.Open()
					if err != nil {
						return err
					}
					defer func() { _ = readerCloser.Close() }()

					feedback, err := decodeReport(readerCloser)
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
