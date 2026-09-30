package finance

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/util/safefetch"
)

const (
	// The bridge keeps about ninety days of history and caps one request
	// at ninety days, but warns above forty-five that it may start
	// refusing wider ones. So history is read in windows of forty-five
	// days, back to ninety.
	simpleFinWindowDuration  = 45 * 24 * time.Hour
	simpleFinHistoryDuration = 90 * 24 * time.Hour

	// How far before the newest posted transaction a later sync reaches
	// back. Pending transactions take days to post, and an institution
	// sometimes posts a transaction dated a week or more ago; fourteen days
	// catches both without refetching the whole history every time.
	simpleFinOverlapDuration = 14 * 24 * time.Hour

	// The largest answer read from the bridge. Forty-five days of every
	// account at one institution is well under a megabyte.
	simpleFinResponseByteLimit = 32 << 20

	// The bridge assembles an answer from its cache of every account at the
	// institution, and a wide window can take longer than safefetch's ten
	// seconds to start answering.
	simpleFinTimeout = 2 * time.Minute
)

// SimpleFIN talks to a SimpleFIN server, in practice the one public
// bridge.
//
// Every address it connects to came from the person: the setup token
// decodes to a claim URL of their choosing, and the credential is the
// address the claim returned. So every request goes through safefetch,
// which refuses anything but public addresses, and only over https,
// because the credential carries a password.
type SimpleFIN struct {
	http *http.Client
	now  func() time.Time

	// location is the person's time zone, which a pending transaction's
	// day is read in; UTC unless InLocation says otherwise.
	location *time.Location
}

// NewSimpleFIN builds a client. It opens no connection.
func NewSimpleFIN() *SimpleFIN {
	client := safefetch.Client()
	client.Timeout = simpleFinTimeout
	if transport, isTransport := client.Transport.(*http.Transport); isTransport {
		transport.ResponseHeaderTimeout = simpleFinTimeout
	}
	return &SimpleFIN{http: client, now: time.Now, location: time.UTC}
}

// InLocation reads the days of pending transactions in the person's time
// zone, and returns the client. A nil location is UTC.
func (self *SimpleFIN) InLocation(location *time.Location) *SimpleFIN {
	if location == nil {
		location = time.UTC
	}
	self.location = location
	return self
}

// Kind is ProviderKindSimpleFIN.
func (self *SimpleFIN) Kind() ProviderKind {
	return ProviderKindSimpleFIN
}

// Remove does nothing: SimpleFIN has no call that revokes a credential.
// The person can revoke it on the bridge's website, and the dashboard
// says so when they delete the finance source.
func (self *SimpleFIN) Remove(_ context.Context, _ string) error {
	return nil
}

// Claim trades a setup token for the credential, once. The token cannot
// be claimed twice, so a caller that fails after this returns must tell
// the person to make a new one.
//
// The token is Base64 of the claim URL. People paste it out of a web page,
// so line breaks, spaces and a missing trailing "=" are forgiven.
func (self *SimpleFIN) Claim(ctx context.Context, setupToken string) (credential string, err error) {
	claimUrl, err := decodeSetupToken(setupToken)
	if err != nil {
		return "", err
	}
	target, err := safefetch.ParseTarget(claimUrl)
	if err != nil {
		return "", fmt.Errorf("finance: the setup token does not hold a usable address: %w", err)
	}
	if target.Scheme != "https" {
		return "", errors.New("finance: the setup token's address is not https")
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), http.NoBody)
	if err != nil {
		return "", fmt.Errorf("finance: %w", err)
	}
	request.Header.Set("User-Agent", UserAgent())
	request.ContentLength = 0

	answer, statusCode, err := self.do(request)
	if err != nil {
		return "", err
	}
	if statusCode == http.StatusForbidden {
		// Also what the bridge answers a request without a user agent it
		// accepts, which does not use the token up; this client always
		// sends one, so a 403 here is the token itself.
		return "", errors.New("finance: SimpleFIN refused the setup token; it may have been claimed already, so make a new one")
	}
	if statusCode/100 != 2 {
		return "", fmt.Errorf("finance: SimpleFIN answered the claim with %d", statusCode)
	}
	credential = strings.TrimSpace(string(answer))
	if _, _, _, err := splitCredential(credential); err != nil {
		return "", errors.New("finance: SimpleFIN's answer to the claim was not a credential")
	}
	return credential, nil
}

// decodeSetupToken reads the claim URL out of a setup token.
func decodeSetupToken(setupToken string) (string, error) {
	compact := strings.Join(strings.Fields(setupToken), "")
	compact = strings.TrimRight(compact, "=")
	if compact == "" {
		return "", errors.New("finance: no setup token")
	}
	for _, encoding := range []*base64.Encoding{base64.RawStdEncoding, base64.RawURLEncoding} {
		if decoded, err := encoding.DecodeString(compact); err == nil {
			return strings.TrimSpace(string(decoded)), nil
		}
	}
	return "", errors.New("finance: the setup token is not Base64")
}

// splitCredential takes the credential apart into the address to fetch
// from, without its user information, and the user name and password to
// send in a header instead. Sending them in the header rather than the
// address keeps them out of every error that quotes an address.
func splitCredential(credential string) (address *url.URL, username, password string, err error) {
	parsed, err := url.Parse(strings.TrimSpace(credential))
	if err != nil {
		return nil, "", "", errors.New("finance: the SimpleFIN credential is not an address")
	}
	if parsed.User == nil {
		return nil, "", "", errors.New("finance: the SimpleFIN credential has no user name")
	}
	username = parsed.User.Username()
	password, hasPassword := parsed.User.Password()
	if username == "" || !hasPassword {
		return nil, "", "", errors.New("finance: the SimpleFIN credential has no password")
	}
	parsed.User = nil
	address, err = safefetch.ParseTarget(parsed.String())
	if err != nil {
		return nil, "", "", fmt.Errorf("finance: the SimpleFIN credential is not a usable address: %w", err)
	}
	if address.Scheme != "https" {
		return nil, "", "", errors.New("finance: the SimpleFIN credential is not https")
	}
	return address, username, password, nil
}

// CheckSimpleFINCredential says whether a credential claimed elsewhere
// has the shape of one: an https address with a user name and a password,
// naming a host safefetch would connect to. It sends nothing.
func CheckSimpleFINCredential(credential string) error {
	_, _, _, err := splitCredential(credential)
	return err
}

// DescribeCredential proves a credential claimed elsewhere works by
// reading the accounts once, balances only, and names the institution
// when every account names the same one. One credential can reach every
// institution the person connected on the bridge, and naming the finance
// source after the first of several would name all its accounts wrongly.
func (self *SimpleFIN) DescribeCredential(ctx context.Context, credential string) (*CredentialDescription, error) {
	address, username, password, err := splitCredential(credential)
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	query.Set("balances-only", "1")
	accountSet, err := self.fetchAccounts(ctx, address, username, password, query)
	if err != nil {
		return nil, err
	}
	institutionNames := map[string]bool{}
	for _, rawAccount := range accountSet.Accounts {
		var decoded simpleFinAccount
		if err := json.Unmarshal(rawAccount, &decoded); err != nil {
			return nil, fmt.Errorf("finance: a SimpleFIN account was not readable: %w", err)
		}
		if institutionName := strings.TrimSpace(decoded.Organization.Name); institutionName != "" {
			institutionNames[institutionName] = true
		}
	}
	description := &CredentialDescription{}
	if len(institutionNames) == 1 {
		for institutionName := range institutionNames {
			description.InstitutionName = institutionName
		}
	}
	return description, nil
}

// simpleFinAccountSet is the answer to GET /accounts. The protocol names
// the warning list errlist, a list of objects; the bridge sends errors, a
// list of strings. Both are read.
type simpleFinAccountSet struct {
	Errors    []json.RawMessage `json:"errors"`
	ErrorList []json.RawMessage `json:"errlist"`
	Accounts  []json.RawMessage `json:"accounts"`
}

type simpleFinAccount struct {
	ID               string      `json:"id"`
	Name             string      `json:"name"`
	Currency         string      `json:"currency"`
	CurrentBalance   string      `json:"balance"`
	AvailableBalance string      `json:"available-balance"`
	BalanceDate      json.Number `json:"balance-date"`
	Organization     struct {
		Name   string `json:"name"`
		Domain string `json:"domain"`
	} `json:"org"`
	Holdings     []json.RawMessage `json:"holdings"`
	Transactions []json.RawMessage `json:"transactions"`
}

type simpleFinTransaction struct {
	ID           string          `json:"id"`
	Posted       json.Number     `json:"posted"`
	Amount       string          `json:"amount"`
	Description  string          `json:"description"`
	Payee        string          `json:"payee"`
	MerchantCode json.RawMessage `json:"mcc"`
	IsPending    bool            `json:"pending"`
	TransactedAt json.Number     `json:"transacted_at"`
}

// Sync reads the accounts and the transactions of the window the cursor
// implies. The cursor holds, for each account, the Unix time in seconds up
// to which its transactions have been read: the newest posted one, or the
// time of a sync that returned none and warned of nothing. It is empty
// before the first sync; a single number is the cursor of an older release,
// which kept one time for the whole connection.
//
// The window starts at the account furthest behind, less an overlap for
// transactions that post late. An institution that fails for a while
// keeps its account's time, so the next sync reaches back over the gap,
// and an account seen for the first time is read from as far back as the
// bridge keeps.
//
// SimpleFIN does not report removals. A pending transaction that posts,
// or is dropped, may come back under a different id or not at all, so a
// sync says instead that every stored pending transaction from the start
// of its window that it did not return is gone (PendingReplacedFrom). That
// is set on the first sync too: a first sync that is run again, because
// the one before died before its cursor was saved, then clears what the
// first attempt stored and has since posted.
func (self *SimpleFIN) Sync(ctx context.Context, credential string, cursor string) (*SyncResult, error) {
	address, username, password, err := splitCredential(credential)
	if err != nil {
		return nil, err
	}
	now := self.now().UTC()
	earliest := now.Add(-simpleFinHistoryDuration)
	readUntil, isEveryAccountKnown, err := parseSimpleFinCursor(cursor)
	if err != nil {
		return nil, err
	}
	start := earliest
	if len(readUntil) > 0 {
		furthestBehind := int64(0)
		for _, seconds := range readUntil {
			if furthestBehind == 0 || seconds < furthestBehind {
				furthestBehind = seconds
			}
		}
		start = time.Unix(furthestBehind, 0).UTC().Add(-simpleFinOverlapDuration)
		// A finance source switched off for months reaches back only as
		// far as the bridge keeps.
		if start.Before(earliest) {
			start = earliest
		}
		if start.After(now) {
			start = now.Add(-simpleFinOverlapDuration)
		}
	}

	accumulated := newSimpleFinAccumulator(self.location)
	if err := self.readWindows(ctx, address, username, password, accumulated, start, now); err != nil {
		return nil, err
	}
	// An account the cursor does not know joined the connection since the
	// last sync: its history before the window is read too.
	if !isEveryAccountKnown && start.After(earliest) {
		for _, account := range accumulated.accounts {
			if _, isKnown := readUntil[account.ProviderAccountID]; !isKnown {
				if err := self.readWindows(ctx, address, username, password, accumulated, earliest, start); err != nil {
					return nil, err
				}
				start = earliest
				break
			}
		}
	}

	syncResult := &SyncResult{NextCursor: cursor, PendingReplacedFrom: &start}
	syncResult.Accounts = accumulated.accounts
	syncResult.Added = accumulated.transactions
	syncResult.InstitutionName = accumulated.institutionName
	syncResult.ProviderWarnings = accumulated.warnings

	// Each account's time moves to its newest posted transaction. One with
	// none new moves to now only when the bridge warned of nothing: a
	// warning may be about that account's institution, whose silence is
	// then not a quiet month.
	nextReadUntil := map[string]int64{}
	for accountId, seconds := range readUntil {
		nextReadUntil[accountId] = seconds
	}
	for _, account := range accumulated.accounts {
		accountId := account.ProviderAccountID
		newest := accumulated.newestPostedSecondsByAccount[accountId]
		if len(accumulated.warnings) == 0 && now.Unix() > newest {
			newest = now.Unix()
		}
		if isEveryAccountKnown {
			// An older release's single time stands for every account.
			if legacy, isSet := readUntil[""]; isSet && legacy > nextReadUntil[accountId] {
				nextReadUntil[accountId] = legacy
			}
		}
		if newest > nextReadUntil[accountId] {
			nextReadUntil[accountId] = newest
		}
	}
	delete(nextReadUntil, "")
	if len(nextReadUntil) > 0 {
		encoded, err := json.Marshal(nextReadUntil)
		if err != nil {
			return nil, err
		}
		syncResult.NextCursor = string(encoded)
	}
	return syncResult, nil
}

// readWindows reads every window from start to end into the accumulator.
// The newest window, the one ending now, is left open, so a transaction
// posted while the request is in flight is not cut off.
func (self *SimpleFIN) readWindows(ctx context.Context, address *url.URL, username, password string, accumulated *simpleFinAccumulator, start, end time.Time) error {
	now := self.now().UTC()
	for windowStart := start; windowStart.Before(end); windowStart = windowStart.Add(simpleFinWindowDuration) {
		windowEnd := windowStart.Add(simpleFinWindowDuration)
		if windowEnd.After(end) {
			windowEnd = end
		}
		query := url.Values{}
		query.Set("start-date", strconv.FormatInt(windowStart.Unix(), 10))
		query.Set("pending", "1")
		if windowEnd.Before(now) {
			query.Set("end-date", strconv.FormatInt(windowEnd.Unix(), 10))
		}
		accountSet, err := self.fetchAccounts(ctx, address, username, password, query)
		if err != nil {
			return err
		}
		if err := accumulated.add(accountSet, now); err != nil {
			return err
		}
	}
	return nil
}

// parseSimpleFinCursor reads a cursor into each account's time. An older
// release's cursor, one number for the connection, comes back under the
// empty account id and says every account the connection has is known.
func parseSimpleFinCursor(cursor string) (map[string]int64, bool, error) {
	cursor = strings.TrimSpace(cursor)
	if cursor == "" {
		return map[string]int64{}, false, nil
	}
	if seconds, err := strconv.ParseInt(cursor, 10, 64); err == nil {
		return map[string]int64{"": seconds}, true, nil
	}
	readUntil := map[string]int64{}
	if err := json.Unmarshal([]byte(cursor), &readUntil); err != nil {
		return nil, false, fmt.Errorf("finance: the SimpleFIN cursor is not readable")
	}
	return readUntil, false, nil
}

func (self *SimpleFIN) fetchAccounts(ctx context.Context, address *url.URL, username, password string, query url.Values) (*simpleFinAccountSet, error) {
	accountsUrl := *address
	accountsUrl.Path = strings.TrimRight(accountsUrl.Path, "/") + "/accounts"
	accountsUrl.RawPath = ""
	accountsUrl.RawQuery = query.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, accountsUrl.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("finance: %w", err)
	}
	request.SetBasicAuth(username, password)
	request.Header.Set("User-Agent", UserAgent())
	request.Header.Set("Accept", "application/json")

	answer, statusCode, err := self.do(request)
	if err != nil {
		return nil, err
	}
	if statusCode == http.StatusForbidden {
		return nil, fmt.Errorf("finance: SimpleFIN answered 403: %w", ErrCredentialRefused)
	}
	if statusCode/100 != 2 {
		return nil, fmt.Errorf("finance: SimpleFIN answered %d", statusCode)
	}
	var accountSet simpleFinAccountSet
	if err := json.Unmarshal(answer, &accountSet); err != nil {
		return nil, fmt.Errorf("finance: SimpleFIN's answer was not readable: %w", err)
	}
	return &accountSet, nil
}

// do sends a request and reads the answer, bounded. A transport error is
// reported without the address, which for a claim is the secret itself.
func (self *SimpleFIN) do(request *http.Request) ([]byte, int, error) {
	response, err := self.http.Do(request)
	if err != nil {
		var urlError *url.Error
		if errors.As(err, &urlError) {
			err = urlError.Err
		}
		return nil, 0, fmt.Errorf("finance: cannot reach SimpleFIN: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	answer, err := io.ReadAll(io.LimitReader(response.Body, simpleFinResponseByteLimit+1))
	if err != nil {
		return nil, 0, fmt.Errorf("finance: cannot read SimpleFIN's answer: %w", err)
	}
	if len(answer) > simpleFinResponseByteLimit {
		return nil, 0, errors.New("finance: SimpleFIN's answer was too large")
	}
	return answer, response.StatusCode, nil
}

// simpleFinAccumulator merges the windows of one sync. Each window names
// every account again, and a transaction near a window's edge, or one
// still pending, can appear in two; the later window's copy wins, being
// the newer answer.
type simpleFinAccumulator struct {
	accounts             []Account
	accountIndexByID     map[string]int
	transactions         []Transaction
	transactionIndexByID map[string]int
	institutionName      string
	warnings             []string
	isWarningSeen        map[string]bool
	location             *time.Location

	// newestPostedSecondsByAccount is each account's newest posted
	// transaction, by provider account id.
	newestPostedSecondsByAccount map[string]int64
}

func newSimpleFinAccumulator(location *time.Location) *simpleFinAccumulator {
	if location == nil {
		location = time.UTC
	}
	return &simpleFinAccumulator{
		accountIndexByID:     map[string]int{},
		transactionIndexByID: map[string]int{},
		isWarningSeen:        map[string]bool{},
		location:             location,

		newestPostedSecondsByAccount: map[string]int64{},
	}
}

func (self *simpleFinAccumulator) warn(warning string) {
	warning = strings.TrimSpace(warning)
	// Each window repeats the bridge's warnings about the connection.
	if warning == "" || self.isWarningSeen[warning] {
		return
	}
	self.isWarningSeen[warning] = true
	self.warnings = append(self.warnings, warning)
}

func (self *simpleFinAccumulator) add(accountSet *simpleFinAccountSet, now time.Time) error {
	for _, raw := range append(accountSet.Errors, accountSet.ErrorList...) {
		self.warn(simpleFinWarningText(raw))
	}
	for _, rawAccount := range accountSet.Accounts {
		var decoded simpleFinAccount
		if err := json.Unmarshal(rawAccount, &decoded); err != nil {
			return fmt.Errorf("finance: a SimpleFIN account was not readable: %w", err)
		}
		if decoded.ID == "" {
			return errors.New("finance: a SimpleFIN account has no id")
		}
		metadata, err := withoutKey(rawAccount, "transactions")
		if err != nil {
			return err
		}
		currencyCode := simpleFinCurrencyCode(decoded.Currency)
		account := Account{
			ProviderAccountID: decoded.ID,
			AccountName:       decoded.Name,
			AccountKind:       simpleFinAccountKind(decoded.Name, len(decoded.Holdings) > 0),
			CurrencyCode:      currencyCode,
			CurrentBalance:    self.balance(decoded.ID, "balance", decoded.CurrentBalance),
			AvailableBalance:  self.balance(decoded.ID, "available-balance", decoded.AvailableBalance),
			BalanceAt:         now,
			ProviderMetadata:  metadata,
		}
		if balanceSeconds, err := decoded.BalanceDate.Int64(); err == nil && balanceSeconds > 0 {
			account.BalanceAt = time.Unix(balanceSeconds, 0).UTC()
		}
		if index, isSeen := self.accountIndexByID[account.ProviderAccountID]; isSeen {
			self.accounts[index] = account
		} else {
			self.accountIndexByID[account.ProviderAccountID] = len(self.accounts)
			self.accounts = append(self.accounts, account)
		}
		if decoded.Organization.Name != "" {
			self.institutionName = decoded.Organization.Name
		}

		for _, rawTransaction := range decoded.Transactions {
			transaction, postedSeconds, err := simpleFinTransactionFrom(rawTransaction, decoded.ID, currencyCode, now, self.location)
			if err != nil {
				// One unreadable transaction must not stop the finance
				// source from syncing forever; it is reported and the
				// rest are kept.
				self.warn(err.Error())
				continue
			}
			if !transaction.IsPending && postedSeconds > self.newestPostedSecondsByAccount[decoded.ID] {
				self.newestPostedSecondsByAccount[decoded.ID] = postedSeconds
			}
			key := transaction.ProviderAccountID + "\x00" + transaction.ProviderTransactionID
			if index, isSeen := self.transactionIndexByID[key]; isSeen {
				self.transactions[index] = transaction
			} else {
				self.transactionIndexByID[key] = len(self.transactions)
				self.transactions = append(self.transactions, transaction)
			}
		}
	}
	return nil
}

// balance reads one of an account's balances, or reports it and leaves it
// unknown when it is not a number.
func (self *simpleFinAccumulator) balance(accountId, balanceFieldName, text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	if _, err := parseDecimal(text); err != nil {
		self.warn(fmt.Sprintf("SimpleFIN account %s has a %s that is not a number", accountId, balanceFieldName))
		return ""
	}
	return strings.TrimSpace(text)
}

func simpleFinTransactionFrom(raw json.RawMessage, accountId, currencyCode string, now time.Time, location *time.Location) (Transaction, int64, error) {
	var decoded simpleFinTransaction
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return Transaction{}, 0, fmt.Errorf("a SimpleFIN transaction in account %s was not readable", accountId)
	}
	if decoded.ID == "" {
		return Transaction{}, 0, fmt.Errorf("a SimpleFIN transaction in account %s has no id", accountId)
	}
	amount := strings.TrimSpace(decoded.Amount)
	if _, err := parseDecimal(amount); err != nil {
		return Transaction{}, 0, fmt.Errorf("SimpleFIN transaction %s has an amount that is not a number", decoded.ID)
	}
	transaction := Transaction{
		ProviderTransactionID: decoded.ID,
		ProviderAccountID:     accountId,
		Amount:                amount,
		CurrencyCode:          currencyCode,
		Description:           decoded.Description,
		MerchantName:          firstNonEmpty(strings.TrimSpace(decoded.Payee), strings.TrimSpace(decoded.Description)),
		IsPending:             decoded.IsPending,
		ProviderMetadata:      raw,
	}
	if merchantCode := simpleFinMerchantCode(decoded.MerchantCode); merchantCode != "" {
		transaction.ProviderCategoryDetailed = "mcc:" + merchantCode
	}
	if transactedSeconds, err := decoded.TransactedAt.Int64(); err == nil && transactedSeconds > 0 {
		transactedAt := time.Unix(transactedSeconds, 0).UTC()
		transaction.TransactedAt = &transactedAt
	}
	postedSeconds, err := decoded.Posted.Int64()
	if err != nil {
		postedSeconds = 0
	}
	// A posted time is read as a UTC day: the bridge stands for a posting
	// day with that day's midnight UTC, which in any zone west of
	// Greenwich is the evening before.
	//
	// A pending transaction has no posted time yet (the protocol says 0).
	// The table needs a day, so it is the day it happened, or failing
	// that the day it was seen; it is replaced when it posts. Both are
	// real moments rather than days, so they are read in the person's
	// time zone: a card used late in the evening west of Greenwich is that
	// day's spending, not the next day's.
	switch {
	case postedSeconds > 0:
		transaction.PostedOn = time.Unix(postedSeconds, 0).UTC().Format(time.DateOnly)
	case transaction.TransactedAt != nil:
		transaction.PostedOn = transaction.TransactedAt.In(location).Format(time.DateOnly)
	default:
		transaction.PostedOn = now.In(location).Format(time.DateOnly)
	}
	return transaction, postedSeconds, nil
}

// simpleFinMerchantCode reads the merchant category code, which the bridge
// sends as a string and could as well send as a number. Anything that is
// not four digits is ignored rather than stored as a category.
func simpleFinMerchantCode(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		var number json.Number
		if err := json.Unmarshal(raw, &number); err != nil {
			return ""
		}
		text = number.String()
	}
	text = strings.TrimSpace(text)
	if len(text) != 4 || !isAllDigits(text) {
		return ""
	}
	return text
}

// simpleFinWarningText is one entry of errors (a string) or errlist (an
// object whose msg says what went wrong).
func simpleFinWarningText(raw json.RawMessage) string {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var described struct {
		ErrorCode        string `json:"code"`
		ErrorText        string `json:"msg"`
		ErrorDescription string `json:"message"`
	}
	if err := json.Unmarshal(raw, &described); err != nil {
		return strings.TrimSpace(string(raw))
	}
	text = firstNonEmpty(described.ErrorText, described.ErrorDescription)
	if described.ErrorCode != "" && text != "" {
		return described.ErrorCode + ": " + text
	}
	return firstNonEmpty(text, described.ErrorCode)
}

// simpleFinCurrencyCode is the account's currency. The protocol allows a
// URL naming a custom currency instead of an ISO code; that is kept as
// sent, and simply has no exchange rate.
func simpleFinCurrencyCode(currency string) string {
	currency = strings.TrimSpace(currency)
	if len(currency) == 3 {
		return strings.ToUpper(currency)
	}
	return currency
}

// simpleFinAccountKind guesses the kind from what SimpleFIN offers, which
// is only the account's name and whether it holds investments. A guess
// that is wrong costs more than no guess, so only unmistakable names
// count, and everything else is other.
func simpleFinAccountKind(accountName string, hasHoldings bool) string {
	if hasHoldings {
		return AccountKindInvestment
	}
	lowered := " " + strings.ToLower(accountName) + " "
	for _, candidate := range []struct {
		words       []string
		accountKind string
	}{
		{[]string{"credit card", " visa ", " mastercard ", " amex "}, AccountKindCredit},
		{[]string{"checking", "savings", "money market", "current account"}, AccountKindDepository},
		{[]string{"mortgage", " loan "}, AccountKindLoan},
		{[]string{"brokerage", " ira ", "401(k)", " 401k ", "investment"}, AccountKindInvestment},
	} {
		for _, word := range candidate.words {
			if strings.Contains(lowered, word) {
				return candidate.accountKind
			}
		}
	}
	return AccountKindOther
}

// withoutKey is a JSON object with one key taken out, the rest exactly as
// it arrived: the account's transactions are stored as rows of their own,
// and keeping them in the account's metadata too would store each twice.
func withoutKey(raw json.RawMessage, key string) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, fmt.Errorf("finance: a SimpleFIN account is not an object: %w", err)
	}
	delete(fields, key)
	// Marshalled with its keys sorted, so the same account stored twice is
	// the same text.
	encoded, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("finance: %w", err)
	}
	return encoded, nil
}
