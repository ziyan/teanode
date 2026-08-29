package mailparse

import (
	"encoding/base64"
	"fmt"
	"hash"
	"io"
	"mime"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html/charset"
)

func MergeHeaders(headers ...[]string) []string {
	length := 0
	for _, h := range headers {
		length += len(h)
	}
	hh := make([]string, 0, length)
	for _, h := range headers {
		hh = append(hh, h...)
	}
	return hh
}

func SplitHeader(header string) (string, string) {
	parts := strings.SplitN(header, ":", 2)
	key := strings.TrimSpace(parts[0])
	if len(parts) > 1 {
		return key, strings.TrimSpace(parts[1])
	}
	return key, ""
}

func UnsplitHeader(key, value string) string {
	return fmt.Sprintf("%s: %s%s", key, value, crlf)
}

func ParseParameters(value string) (map[string]string, error) {
	pairs := strings.Split(value, ";")
	parameters := make(map[string]string)
	for _, pair := range pairs {
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 {
			if trimmed := strings.TrimSpace(pair); trimmed != "" {
				parameters[""] = trimmed
			}
			continue
		}
		parameters[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	return parameters, nil
}

func UnparseParameters(parameters map[string]string, prefixes, suffixes []string) string {
	sortedKeys := make([]string, 0, len(parameters))
	for key := range parameters {
		if !inStringSlice(key, prefixes) && !inStringSlice(key, suffixes) {
			sortedKeys = append(sortedKeys, key)
		}
	}
	sort.Strings(sortedKeys)
	keys := make([]string, 0, len(parameters))
	keys = append(keys, prefixes...)
	keys = append(keys, sortedKeys...)
	keys = append(keys, suffixes...)

	var value string
	var index int
	for _, key := range keys {
		if _, ok := parameters[key]; !ok {
			continue
		}
		if index > 0 {
			value += "; "
		}
		value += key + "=" + parameters[key]
		index++
	}
	return value
}

func ParseTagList(value string) []string {
	tags := strings.Split(value, ":")
	for i, tag := range tags {
		tags[i] = StripWhitespace(tag)
	}
	return tags
}

func UnparseTagList(values []string) string {
	return strings.Join(values, ":")
}

func ParseTime(value string) (time.Time, error) {
	seconds, err := strconv.ParseInt(StripWhitespace(value), 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(seconds, 0), nil
}

func UnparseTime(value time.Time) string {
	return fmt.Sprintf("%d", value.Unix())
}

type headerPicker struct {
	headers []string
	picked  map[string]int
}

func (self *headerPicker) pick(key string) string {
	at := self.picked[key]
	for i := len(self.headers) - 1; i >= 0; i-- {
		header := self.headers[i]
		k, _ := SplitHeader(header)
		if !strings.EqualFold(k, key) {
			continue
		}
		if at == 0 {
			self.picked[key]++
			return header
		}
		at--
	}
	return ""
}

func HashHeaders(headers []string, keys []string, canonicalizer Canonicalizer, hasher hash.Hash) error {
	self := &headerPicker{
		headers: headers,
		picked:  make(map[string]int),
	}
	for _, key := range keys {
		if header := self.pick(key); header != "" {
			if _, err := hasher.Write([]byte(canonicalizer.CanonicalizeHeader(header))); err != nil {
				return err
			}
		}
	}
	return nil
}

func IsHeaderOfInterest(key string, interestedHeaderKeys []string) bool {
	for _, interestedHeaderKey := range interestedHeaderKeys {
		if strings.EqualFold(key, interestedHeaderKey) {
			return true
		}
	}
	return false
}

var removeSignaturePattern = regexp.MustCompile(`(b\s*=)[^;]+`)

func RemoveSignature(value string) string {
	return removeSignaturePattern.ReplaceAllString(value, "$1")
}

func DecodeBase64String(value string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(StripWhitespace(value))
}

func EncodeBase64String(value []byte) string {
	return base64.StdEncoding.EncodeToString(value)
}

func inStringSlice(needle string, haystack []string) bool {
	for _, n := range haystack {
		if n == needle {
			return true
		}
	}
	return false
}

func FindHeaderValue(headers []string, key string) string {
	for i := len(headers) - 1; i >= 0; i-- {
		k, v := SplitHeader(headers[i])
		if strings.EqualFold(k, key) {
			return v
		}
	}
	return ""
}

var wordDecoder = &mime.WordDecoder{
	CharsetReader: func(label string, input io.Reader) (io.Reader, error) {
		if encoding, _ := charset.Lookup(label); encoding != nil {
			return encoding.NewDecoder().Reader(input), nil
		}
		log.Errorf("failed to lookup charset %q", label)
		return nil, fmt.Errorf("mailparse: failed to decode charset %q", label)
	},
}

func DecodeHeaderValue(value string) string {
	if decoded, err := wordDecoder.DecodeHeader(strings.Join(strings.Split(value, crlf), "")); err == nil {
		return decoded
	}
	return value
}

func EncodeHeaderValue(value string) string {
	return mime.BEncoding.Encode("UTF-8", value)
}
