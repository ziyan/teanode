package dmarc

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/util/mailparse"
)

func Parse(txt string) (*Record, error) {
	parameters, err := mailparse.ParseParameters(txt)
	if err != nil {
		return nil, err
	}
	if parameters["v"] != "DMARC1" {
		return nil, fmt.Errorf("dmarc: unsupported version")
	}

	record := &Record{}
	policy, ok := parameters["p"]
	if !ok {
		return nil, fmt.Errorf("dmarc: record is missing a 'p' parameter")
	}
	record.Policy, err = parsePolicy(policy, "p")
	if err != nil {
		return nil, err
	}

	record.DKIMAlignment = AlignmentRelaxed
	if aDkim, ok := parameters["adkim"]; ok {
		record.DKIMAlignment, err = parseAlignmentMode(aDkim, "adkim")
		if err != nil {
			return nil, err
		}
	}

	record.SPFAlignment = AlignmentRelaxed
	if aSpf, ok := parameters["aspf"]; ok {
		record.SPFAlignment, err = parseAlignmentMode(aSpf, "aspf")
		if err != nil {
			return nil, err
		}
	}

	if fo, ok := parameters["fo"]; ok {
		record.FailureOptions, err = parseFailureOptions(fo)
		if err != nil {
			return nil, err
		}
	}

	if pct, ok := parameters["pct"]; ok {
		parsed, err := strconv.Atoi(pct)
		if err != nil {
			return nil, fmt.Errorf("dmarc: invalid parameter 'pct': %w", err)
		}
		if parsed < 0 || parsed > 100 {
			return nil, fmt.Errorf("dmarc: invalid parameter 'pct': value %v out of bounds", parsed)
		}
		percent := uint64(parsed)
		record.Percent = &percent
	}

	if rf, ok := parameters["rf"]; ok {
		formats := strings.Split(rf, ":")
		record.ReportFormat = make([]ReportFormat, len(formats))
		for index, format := range formats {
			switch format {
			case "afrf":
				record.ReportFormat[index] = ReportFormat(format)
			default:
				return nil, fmt.Errorf("dmarc: invalid parameter 'rf'")
			}
		}
	}

	if ri, ok := parameters["ri"]; ok {
		seconds, err := strconv.Atoi(ri)
		if err != nil {
			return nil, fmt.Errorf("dmarc: invalid parameter 'ri': %w", err)
		}
		if seconds <= 0 {
			return nil, fmt.Errorf("dmarc: invalid parameter 'ri': negative or zero duration")
		}
		record.ReportInterval = time.Duration(seconds) * time.Second
	}

	if rua, ok := parameters["rua"]; ok {
		record.ReportURIAggregate = parseUriList(rua)
	}

	if ruf, ok := parameters["ruf"]; ok {
		record.ReportURIFailure = parseUriList(ruf)
	}

	if sp, ok := parameters["sp"]; ok {
		record.SubdomainPolicy, err = parsePolicy(sp, "sp")
		if err != nil {
			return nil, err
		}
	}

	return record, nil
}

func parsePolicy(value, parameter string) (Policy, error) {
	switch value {
	case "none", "quarantine", "reject":
		return Policy(value), nil
	default:
		return "", fmt.Errorf("dmarc: invalid policy for parameter '%v'", parameter)
	}
}

func parseAlignmentMode(value, parameter string) (AlignmentMode, error) {
	switch value {
	case "r", "s":
		return AlignmentMode(value), nil
	default:
		return "", fmt.Errorf("dmarc: invalid alignment mode for parameter '%v'", parameter)
	}
}

func parseFailureOptions(value string) (FailureOptions, error) {
	options := strings.Split(value, ":")
	var opts FailureOptions
	for _, option := range options {
		switch strings.TrimSpace(option) {
		case "0":
			opts |= FailureAll
		case "1":
			opts |= FailureAny
		case "d":
			opts |= FailureDKIM
		case "s":
			opts |= FailureSPF
		default:
			return 0, fmt.Errorf("dmarc: invalid failure option in parameter 'fo'")
		}
	}
	return opts, nil
}

func parseUriList(value string) []string {
	uris := strings.Split(value, ",")
	for index, uri := range uris {
		uris[index] = strings.TrimSpace(uri)
	}
	return uris
}
