package passwords

import (
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
	"time"
)

// The secrets of RFC 6238 appendix B: the ASCII digits 1234567890 repeated to
// the length of each hash.
const (
	rfcSecretSHA1   = "12345678901234567890"
	rfcSecretSHA256 = "12345678901234567890123456789012"
	rfcSecretSHA512 = "1234567890123456789012345678901234567890123456789012345678901234"
)

func rfcAddress(secret, algorithm string, digits int) string {
	return "otpauth://totp/Example:ada@example.com?secret=" + base32.StdEncoding.EncodeToString([]byte(secret)) +
		"&issuer=Example&algorithm=" + algorithm + "&digits=" + string(rune('0'+digits))
}

func TestRFC6238AppendixB(test *testing.T) {
	cases := []struct {
		algorithm string
		secret    string
		codes     map[int64]string
	}{
		{"SHA1", rfcSecretSHA1, map[int64]string{59: "94287082", 1111111109: "07081804", 1111111111: "14050471", 1234567890: "89005924", 2000000000: "69279037", 20000000000: "65353130"}},
		{"SHA256", rfcSecretSHA256, map[int64]string{59: "46119246", 1111111109: "68084774", 1111111111: "67062674", 1234567890: "91819424", 2000000000: "90698825", 20000000000: "77737706"}},
		{"SHA512", rfcSecretSHA512, map[int64]string{59: "90693936", 1111111109: "25091201", 1111111111: "99943326", 1234567890: "93441116", 2000000000: "38618901", 20000000000: "47863826"}},
	}
	for _, testCase := range cases {
		oneTimeCode, err := ParseOneTimeCodeURI(rfcAddress(testCase.secret, testCase.algorithm, 8))
		if err != nil {
			test.Fatal(err)
		}
		for unixSeconds, want := range testCase.codes {
			got, err := oneTimeCode.CodeAt(time.Unix(unixSeconds, 0))
			if err != nil || got != want {
				test.Errorf("%s at %d = %q, %v, want %q", testCase.algorithm, unixSeconds, got, err, want)
			}
		}
	}
}

func TestOneTimeCodeDefaults(test *testing.T) {
	oneTimeCode, err := ParseOneTimeCodeURI("otpauth://totp/Example:ada@example.com?secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ&issuer=Example")
	if err != nil {
		test.Fatal(err)
	}
	if oneTimeCode.Algorithm != "SHA1" || oneTimeCode.DigitCount != 6 || oneTimeCode.PeriodSeconds != 30 {
		test.Fatalf("defaults = %+v", oneTimeCode)
	}
	if oneTimeCode.Issuer != "Example" || oneTimeCode.AccountName != "ada@example.com" {
		test.Fatalf("label = %q, %q", oneTimeCode.Issuer, oneTimeCode.AccountName)
	}
	// The six digit code is the last six digits of the eight digit one.
	got, _ := oneTimeCode.CodeAt(time.Unix(59, 0))
	if got != "287082" {
		test.Fatalf("code = %q", got)
	}
}

func TestOneTimeCodeSecretSpellings(test *testing.T) {
	want, _ := ParseOneTimeCodeURI("otpauth://totp/a?secret=JBSWY3DPEHPK3PXP")
	wantCode, _ := want.CodeAt(time.Unix(1000, 0))
	for _, secret := range []string{"jbswy3dpehpk3pxp", "JBSWY3DP EHPK3PXP", "jbsw-y3dp-ehpk-3pxp", "JBSWY3DPEHPK3PXP===="} {
		oneTimeCode, err := ParseOneTimeCodeURI("otpauth://totp/a?secret=" + secret)
		if err != nil {
			test.Errorf("%q: %v", secret, err)
			continue
		}
		if code, _ := oneTimeCode.CodeAt(time.Unix(1000, 0)); code != wantCode {
			test.Errorf("%q gives %q, want %q", secret, code, wantCode)
		}
	}
}

func TestOneTimeCodePeriod(test *testing.T) {
	oneTimeCode, err := ParseOneTimeCodeURI("otpauth://totp/a?secret=JBSWY3DPEHPK3PXP&period=60")
	if err != nil {
		test.Fatal(err)
	}
	first, _ := oneTimeCode.CodeAt(time.Unix(60, 0))
	second, _ := oneTimeCode.CodeAt(time.Unix(119, 0))
	third, _ := oneTimeCode.CodeAt(time.Unix(120, 0))
	if first != second || first == third {
		test.Fatalf("codes = %s %s %s", first, second, third)
	}
	if remaining := oneTimeCode.SecondsRemainingAt(time.Unix(119, 0)); remaining != 1 {
		test.Fatalf("remaining = %d", remaining)
	}
	if remaining := oneTimeCode.SecondsRemainingAt(time.Unix(120, 0)); remaining != 60 {
		test.Fatalf("remaining = %d", remaining)
	}
}

func TestParseOneTimeCodeURIRefuses(test *testing.T) {
	for _, address := range []string{
		"",
		"https://example.com",
		"otpauth://hotp/a?secret=JBSWY3DPEHPK3PXP&counter=1",
		"otpauth://totp/a",
		"otpauth://totp/a?secret=!!!!",
		"otpauth://totp/a?secret=JBSWY3DPEHPK3PXP&digits=7",
		"otpauth://totp/a?secret=JBSWY3DPEHPK3PXP&algorithm=MD5",
		"otpauth://totp/a?secret=JBSWY3DPEHPK3PXP&period=0",
	} {
		if _, err := ParseOneTimeCodeURI(address); err == nil {
			test.Errorf("%q was accepted", address)
		}
	}
}

// testdata/onetimecode.json holds the addresses the reading must answer the
// same way here and in the dashboard and the extension (web/src/passwords/
// oneTimeCode.ts, which reads the same file).
type sharedOneTimeCodeCases struct {
	UnixSeconds []int64 `json:"unixSeconds"`
	Cases       []struct {
		Name          string   `json:"name"`
		Address       string   `json:"address"`
		IsValid       bool     `json:"isValid"`
		SecretHex     string   `json:"secretHex"`
		Algorithm     string   `json:"algorithm"`
		DigitCount    int      `json:"digitCount"`
		PeriodSeconds int      `json:"periodSeconds"`
		Issuer        string   `json:"issuer"`
		AccountName   string   `json:"accountName"`
		ExpectedCodes []string `json:"expectedCodes"`
	} `json:"cases"`
}

func TestSharedOneTimeCodeCases(test *testing.T) {
	content, err := os.ReadFile("testdata/onetimecode.json")
	if err != nil {
		test.Fatal(err)
	}
	var shared sharedOneTimeCodeCases
	if err := json.Unmarshal(content, &shared); err != nil {
		test.Fatal(err)
	}
	if len(shared.Cases) < 50 || len(shared.UnixSeconds) == 0 {
		test.Fatal("testdata/onetimecode.json has too few cases")
	}
	for _, each := range shared.Cases {
		oneTimeCode, err := ParseOneTimeCodeURI(each.Address)
		if !each.IsValid {
			if err == nil {
				test.Errorf("%s: %q was accepted", each.Name, each.Address)
			}
			continue
		}
		if err != nil {
			test.Errorf("%s: %q was refused: %v", each.Name, each.Address, err)
			continue
		}
		if hex.EncodeToString(oneTimeCode.Secret) != each.SecretHex || oneTimeCode.Algorithm != each.Algorithm ||
			oneTimeCode.DigitCount != each.DigitCount || oneTimeCode.PeriodSeconds != each.PeriodSeconds ||
			oneTimeCode.Issuer != each.Issuer || oneTimeCode.AccountName != each.AccountName {
			test.Errorf("%s: %q read as %+v", each.Name, each.Address, oneTimeCode)
			continue
		}
		for index, unixSeconds := range shared.UnixSeconds {
			if got, err := oneTimeCode.CodeAt(time.Unix(unixSeconds, 0)); err != nil || got != each.ExpectedCodes[index] {
				test.Errorf("%s: code at %d = %q, %v, wanted %q", each.Name, unixSeconds, got, err, each.ExpectedCodes[index])
			}
		}
	}
}

func TestParseOneTimeCodeURIRefusesWhatTheOtherReadingsRefuse(test *testing.T) {
	for _, address := range []string{
		"otpauth://totp/a?secret=JBSW\tY3DPEHPK3PXP",
		"otpauth://totp/a?secret=JBSWY3DP=EHPK3PXP",
		"otpauth://totp/a?secret=JBSWY3DPEHPK3PXP&digits=%2B8",
		"otpauth://totp/a?secret=JBSWY3DPEHPK3PXP&period=%2B5",
		"otpauth://totp/a?secret=JBSWY3DPEHPK3PXP&period=99999999999999999999",
		"otpauth://totp:80/a?secret=JBSWY3DPEHPK3PXP",
	} {
		if _, err := ParseOneTimeCodeURI(address); err == nil {
			test.Errorf("%q was accepted", address)
		}
	}
}
