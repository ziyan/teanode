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
	p, ok := parameters["p"]
	if !ok {
		return nil, fmt.Errorf("dmarc: record is missing a 'p' parameter")
	}
	record.Policy, err = parsePolicy(p, "p")
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
		i, err := strconv.Atoi(pct)
		if err != nil {
			return nil, fmt.Errorf("dmarc: invalid parameter 'pct': %w", err)
		}
		if i < 0 || i > 100 {
			return nil, fmt.Errorf("dmarc: invalid parameter 'pct': value %v out of bounds", i)
		}
		percent := uint64(i)
		record.Percent = &percent
	}

	if rf, ok := parameters["rf"]; ok {
		l := strings.Split(rf, ":")
		record.ReportFormat = make([]ReportFormat, len(l))
		for i, f := range l {
			switch f {
			case "afrf":
				record.ReportFormat[i] = ReportFormat(f)
			default:
				return nil, fmt.Errorf("dmarc: invalid parameter 'rf'")
			}
		}
	}

	if ri, ok := parameters["ri"]; ok {
		i, err := strconv.Atoi(ri)
		if err != nil {
			return nil, fmt.Errorf("dmarc: invalid parameter 'ri': %w", err)
		}
		if i <= 0 {
			return nil, fmt.Errorf("dmarc: invalid parameter 'ri': negative or zero duration")
		}
		record.ReportInterval = time.Duration(i) * time.Second
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

func parsePolicy(s, parameter string) (Policy, error) {
	switch s {
	case "none", "quarantine", "reject":
		return Policy(s), nil
	default:
		return "", fmt.Errorf("dmarc: invalid policy for parameter '%v'", parameter)
	}
}

func parseAlignmentMode(s, parameter string) (AlignmentMode, error) {
	switch s {
	case "r", "s":
		return AlignmentMode(s), nil
	default:
		return "", fmt.Errorf("dmarc: invalid alignment mode for parameter '%v'", parameter)
	}
}

func parseFailureOptions(s string) (FailureOptions, error) {
	l := strings.Split(s, ":")
	var opts FailureOptions
	for _, o := range l {
		switch strings.TrimSpace(o) {
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

func parseUriList(s string) []string {
	l := strings.Split(s, ",")
	for i, u := range l {
		l[i] = strings.TrimSpace(u)
	}
	return l
}
