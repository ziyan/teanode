package dmarc_test

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/textproto"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/util/dmarc"
	"github.com/ziyan/teanode/internal/util/mailparse"
)

// reportXML is an aggregate report in the shape every reporter sends: one
// report covering a day, with several source addresses and mixed verdicts.
// It is written out rather than captured so that no real domain, address or
// correspondence is committed.
const reportXML = `<?xml version="1.0" encoding="UTF-8" ?>
<feedback>
  <report_metadata>
    <org_name>reporter.example</org_name>
    <email>noreply-dmarc@reporter.example</email>
    <extra_contact_info>https://reporter.example/dmarc</extra_contact_info>
    <report_id>20260818-000001</report_id>
    <date_range>
      <begin>1755475200</begin>
      <end>1755561600</end>
    </date_range>
  </report_metadata>
  <policy_published>
    <domain>example.com</domain>
    <aDkim>r</aDkim>
    <aSpf>r</aSpf>
    <p>none</p>
    <sp>quarantine</sp>
    <pct>100</pct>
  </policy_published>
  <record>
    <row>
      <source_ip>198.51.100.10</source_ip>
      <count>42</count>
      <policy_evaluated>
        <disposition>none</disposition>
        <dkim>pass</dkim>
        <spf>pass</spf>
      </policy_evaluated>
    </row>
    <identifiers>
      <header_from>example.com</header_from>
      <envelope_from>example.com</envelope_from>
    </identifiers>
    <auth_results>
      <dkim>
        <domain>example.com</domain>
        <selector>selector1</selector>
        <result>pass</result>
      </dkim>
      <spf>
        <domain>example.com</domain>
        <scope>mfrom</scope>
        <result>pass</result>
      </spf>
    </auth_results>
  </record>
  <record>
    <row>
      <source_ip>2001:db8::1</source_ip>
      <count>3</count>
      <policy_evaluated>
        <disposition>quarantine</disposition>
        <dkim>fail</dkim>
        <spf>fail</spf>
        <reason>
          <type>forwarded</type>
          <comment>message was forwarded</comment>
        </reason>
      </policy_evaluated>
    </row>
    <identifiers>
      <header_from>example.com</header_from>
    </identifiers>
    <auth_results>
      <spf>
        <domain>forwarder.example</domain>
        <scope>mfrom</scope>
        <result>softfail</result>
      </spf>
    </auth_results>
  </record>
</feedback>
`

// assertReport checks a decoded report against reportXML.
func assertReport(t *testing.T, feedback *dmarc.Feedback) {
	t.Helper()

	if feedback.OrganizationName != "reporter.example" {
		t.Errorf("organization is %q", feedback.OrganizationName)
	}
	if feedback.ReportID != "20260818-000001" {
		t.Errorf("report id is %q", feedback.ReportID)
	}
	if feedback.Domain != "example.com" {
		t.Errorf("domain is %q", feedback.Domain)
	}
	if feedback.Policy != "none" || feedback.SubdomainPolicy != "quarantine" {
		t.Errorf("policy is %q and subdomain policy is %q", feedback.Policy, feedback.SubdomainPolicy)
	}
	if feedback.Begin != 1755475200 || feedback.End != 1755561600 {
		t.Errorf("date range is %d to %d", feedback.Begin, feedback.End)
	}
	if len(feedback.Records) != 2 {
		t.Fatalf("got %d records, want 2", len(feedback.Records))
	}

	passing := feedback.Records[0]
	if passing.SourceIP != "198.51.100.10" || passing.Count != 42 {
		t.Errorf("first record is %s x%d", passing.SourceIP, passing.Count)
	}
	if passing.Disposition != "none" || passing.DKIM != "pass" || passing.SPF != "pass" {
		t.Errorf("first record evaluated as %s/%s/%s", passing.Disposition, passing.DKIM, passing.SPF)
	}
	if len(passing.DKIMs) != 1 || passing.DKIMs[0].Selector != "selector1" {
		t.Errorf("first record dkim results are %v", passing.DKIMs)
	}

	// The second record is the interesting one: a forwarded message that
	// failed both checks and was quarantined, with an override reason. This is
	// exactly what a report about broken forwarding looks like.
	failing := feedback.Records[1]
	if failing.SourceIP != "2001:db8::1" || failing.Count != 3 {
		t.Errorf("second record is %s x%d", failing.SourceIP, failing.Count)
	}
	if failing.Disposition != "quarantine" || failing.DKIM != "fail" || failing.SPF != "fail" {
		t.Errorf("second record evaluated as %s/%s/%s", failing.Disposition, failing.DKIM, failing.SPF)
	}
	if failing.ReasonType != "forwarded" {
		t.Errorf("second record reason is %q", failing.ReasonType)
	}
	if len(failing.SPFs) != 1 || failing.SPFs[0].Result != "softfail" {
		t.Errorf("second record spf results are %v", failing.SPFs)
	}
}

func TestDecode(t *testing.T) {
	t.Parallel()

	feedback, err := dmarc.Decode(strings.NewReader(reportXML))
	if err != nil {
		t.Fatalf("failed to decode: %s", err)
	}
	assertReport(t, feedback)
}

func TestDecodeRejectsMalformedXML(t *testing.T) {
	t.Parallel()

	if _, err := dmarc.Decode(strings.NewReader("<feedback><unclosed>")); err == nil {
		t.Error("malformed XML should be an error")
	}
}

// TestDecodeFromReportMail walks the whole path a report arrives by: a message
// with the report attached, compressed, base64 encoded. Reporters use gzip or
// zip, so both are covered.
func TestDecodeFromReportMail(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		mediaType   string
		compression func(t *testing.T) []byte
	}{
		{"gzip", "application/gzip", gzipReport},
		{"zip", "application/zip", zipReport},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			message := reportMail(t, test.mediaType, test.compression(t))
			headers, body, err := mailparse.Split(bytes.NewReader(message))
			if err != nil {
				t.Fatalf("failed to parse the report mail: %s", err)
			}

			var decoded int
			if err := mailparse.TraverseParts(headers, body, func(header textproto.MIMEHeader, reader io.Reader) error {
				mediaType, _, err := mime.ParseMediaType(header.Get("Content-Type"))
				if err != nil || mediaType != test.mediaType {
					return nil
				}
				if header.Get("Content-Transfer-Encoding") != "base64" {
					return fmt.Errorf("unexpected transfer encoding %q", header.Get("Content-Transfer-Encoding"))
				}
				content, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, reader))
				if err != nil {
					return err
				}

				feedback, err := decompress(test.mediaType, content)
				if err != nil {
					return err
				}
				assertReport(t, feedback)
				decoded++
				return nil
			}); err != nil {
				t.Fatalf("failed to walk the report mail: %s", err)
			}
			if decoded != 1 {
				t.Errorf("decoded %d reports, want 1", decoded)
			}
		})
	}
}

func decompress(mediaType string, content []byte) (*dmarc.Feedback, error) {
	switch mediaType {
	case "application/gzip":
		reader, err := gzip.NewReader(bytes.NewReader(content))
		if err != nil {
			return nil, err
		}
		defer func() { _ = reader.Close() }()
		return dmarc.Decode(reader)
	case "application/zip":
		reader, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
		if err != nil {
			return nil, err
		}
		if len(reader.File) != 1 {
			return nil, fmt.Errorf("expected one file in the archive, got %d", len(reader.File))
		}
		file, err := reader.File[0].Open()
		if err != nil {
			return nil, err
		}
		defer func() { _ = file.Close() }()
		return dmarc.Decode(file)
	}
	return nil, fmt.Errorf("unexpected media type %q", mediaType)
}

func gzipReport(t *testing.T) []byte {
	t.Helper()

	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	if _, err := writer.Write([]byte(reportXML)); err != nil {
		t.Fatalf("failed to compress: %s", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("failed to compress: %s", err)
	}
	return buffer.Bytes()
}

func zipReport(t *testing.T) []byte {
	t.Helper()

	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	file, err := writer.Create("reporter.example!example.com!1755475200!1755561600.xml")
	if err != nil {
		t.Fatalf("failed to compress: %s", err)
	}
	if _, err := file.Write([]byte(reportXML)); err != nil {
		t.Fatalf("failed to compress: %s", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("failed to compress: %s", err)
	}
	return buffer.Bytes()
}

// reportMail wraps a compressed report in the multipart message a reporter
// sends it in.
func reportMail(t *testing.T, mediaType string, compressed []byte) []byte {
	t.Helper()

	const boundary = "report-boundary-0000"
	var builder strings.Builder
	builder.WriteString("Date: Tue, 18 Aug 2026 12:00:00 +0000\r\n")
	builder.WriteString("From: noreply-dmarc@reporter.example\r\n")
	builder.WriteString("To: rua@mail.example.com\r\n")
	builder.WriteString("Subject: Report domain: example.com Submitter: reporter.example\r\n")
	builder.WriteString("MIME-Version: 1.0\r\n")
	builder.WriteString("Content-Type: multipart/mixed; boundary=\"" + boundary + "\"\r\n")
	builder.WriteString("\r\n")
	builder.WriteString("--" + boundary + "\r\n")
	builder.WriteString("Content-Type: text/plain; charset=utf-8\r\n\r\n")
	builder.WriteString("This is an aggregate report from reporter.example.\r\n")
	builder.WriteString("\r\n--" + boundary + "\r\n")
	builder.WriteString("Content-Type: " + mediaType + "\r\n")
	builder.WriteString("Content-Transfer-Encoding: base64\r\n")
	builder.WriteString("Content-Disposition: attachment; filename=\"report.xml.gz\"\r\n\r\n")

	encoded := base64.StdEncoding.EncodeToString(compressed)
	for len(encoded) > 76 {
		builder.WriteString(encoded[:76] + "\r\n")
		encoded = encoded[76:]
	}
	builder.WriteString(encoded + "\r\n")
	builder.WriteString("\r\n--" + boundary + "--\r\n")
	return []byte(builder.String())
}
