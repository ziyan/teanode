package finance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Plaid's environments, and where each answers.
const (
	PlaidEnvironmentSandbox    = "sandbox"
	PlaidEnvironmentProduction = "production"

	plaidSandboxBaseUrl    = "https://sandbox.plaid.com"
	plaidProductionBaseUrl = "https://production.plaid.com"
)

// The API version every request names. Plaid answers a request that names
// none with whatever version the developer account defaults to, which the
// account holder can change on Plaid's dashboard; pinning it keeps the
// shape this client decodes the shape it gets.
const plaidApiVersion = "2020-09-14"

const (
	// How much history a new finance source asks for. It is fixed at link
	// time and 730 days is Plaid's ceiling; institutions give what they
	// have, which is often less.
	plaidDaysRequested = 730

	// Transactions per page of /transactions/sync, Plaid's maximum.
	plaidSyncPageTransactionCount = 500

	// How many times a sync starts over when Plaid reports the finance
	// source changed while it was paging. Plaid's advice is to restart
	// from the first cursor; a source that keeps changing faster than it
	// can be read is left for the next scheduled sync.
	plaidSyncAttemptLimit = 3

	// A sync that pages this many times is not converging, whatever
	// has_more says. At 500 a page it is a quarter of a million
	// transactions, more than 730 days of anybody's history.
	plaidSyncPageLimit = 500

	// The largest answer read from Plaid. A page of 500 transactions with
	// their accounts is a few megabytes at most.
	plaidResponseByteLimit = 32 << 20

	plaidTimeout = 60 * time.Second
)

// Plaid's error codes this client acts on.
const (
	plaidErrorCodeItemLoginRequired = "ITEM_LOGIN_REQUIRED"
	plaidErrorCodeSyncMutation      = "TRANSACTIONS_SYNC_MUTATION_DURING_PAGINATION"
)

// Plaid talks to Plaid's API for one operator's developer account.
//
// It is plain net/http with a struct per request and response rather than
// Plaid's SDK: the few calls used here do not justify a dependency that
// regenerates itself from an API description every week.
type Plaid struct {
	baseUrl      string
	clientId     string
	secret       string
	countryCodes []string
	products     []string
	http         *http.Client
}

// NewPlaid builds a client. It opens no connection; the first call does.
//
// Country codes default to the United States and products to transactions,
// which is what a Plaid account can use without asking Plaid for more.
func NewPlaid(environment, clientId, secret string, countryCodes []string, products []string) (*Plaid, error) {
	var baseUrl string
	switch environment {
	case PlaidEnvironmentSandbox:
		baseUrl = plaidSandboxBaseUrl
	case PlaidEnvironmentProduction:
		baseUrl = plaidProductionBaseUrl
	default:
		return nil, fmt.Errorf("finance: Plaid has no environment called %q", environment)
	}
	clientId = strings.TrimSpace(clientId)
	secret = strings.TrimSpace(secret)
	if clientId == "" || secret == "" {
		return nil, errors.New("finance: Plaid needs both a client id and a secret")
	}
	if len(countryCodes) == 0 {
		countryCodes = []string{"US"}
	}
	if len(products) == 0 {
		products = []string{"transactions"}
	}
	return &Plaid{
		baseUrl:      baseUrl,
		clientId:     clientId,
		secret:       secret,
		countryCodes: append([]string(nil), countryCodes...),
		products:     append([]string(nil), products...),
		http:         &http.Client{Timeout: plaidTimeout},
	}, nil
}

// Kind is ProviderKindPlaid.
func (self *Plaid) Kind() ProviderKind {
	return ProviderKindPlaid
}

// CreateLinkToken asks for the short-lived token Plaid Link opens with.
//
// With a credential for repair it is update mode: Link signs the person in
// again to the finance source they already have, rather than linking a
// new one. Update mode names no products, because the finance source keeps
// the ones it was linked with, and Plaid refuses a request that names them.
// Repairing matters beyond convenience: on some Plaid plans every finance
// source ever linked counts against a limit, deleted or not.
func (self *Plaid) CreateLinkToken(ctx context.Context, personReference string, credentialForRepair string) (string, error) {
	if strings.TrimSpace(personReference) == "" {
		return "", errors.New("finance: a link token needs a person reference")
	}
	body := map[string]any{
		"client_name":   "TeaNode",
		"language":      "en",
		"country_codes": self.countryCodes,
		"user":          map[string]any{"client_user_id": personReference},
	}
	if credentialForRepair != "" {
		body["access_token"] = credentialForRepair
	} else {
		body["products"] = self.products
		body["transactions"] = map[string]any{"days_requested": plaidDaysRequested}
	}
	var answered struct {
		LinkToken string `json:"link_token"`
	}
	if err := self.post(ctx, "/link/token/create", body, &answered); err != nil {
		return "", err
	}
	if answered.LinkToken == "" {
		return "", errors.New("finance: Plaid answered without a link token")
	}
	return answered.LinkToken, nil
}

// ExchangePublicToken trades the token Plaid Link hands the browser for
// the finance source's credential and Plaid's id for it. The public token
// is single use and expires in minutes, so this runs as soon as the
// browser sends it.
func (self *Plaid) ExchangePublicToken(ctx context.Context, publicToken string) (credential, providerReference string, err error) {
	if strings.TrimSpace(publicToken) == "" {
		return "", "", errors.New("finance: no public token to exchange")
	}
	var answered struct {
		AccessToken string `json:"access_token"`
		ItemID      string `json:"item_id"`
	}
	if err := self.post(ctx, "/item/public_token/exchange", map[string]any{"public_token": publicToken}, &answered); err != nil {
		return "", "", err
	}
	if answered.AccessToken == "" || answered.ItemID == "" {
		return "", "", errors.New("finance: Plaid answered the exchange without a credential")
	}
	return answered.AccessToken, answered.ItemID, nil
}

// Remove ends the finance source at Plaid, which stops Plaid charging the
// operator for it every month.
func (self *Plaid) Remove(ctx context.Context, credential string) error {
	return self.post(ctx, "/item/remove", map[string]any{"access_token": credential}, nil)
}

// InstitutionName asks Plaid what an institution is called. Link's
// metadata already names it, so this is for a finance source whose name
// was lost or never recorded.
func (self *Plaid) InstitutionName(ctx context.Context, institutionId string) (string, error) {
	var answered struct {
		Institution struct {
			Name string `json:"name"`
		} `json:"institution"`
	}
	body := map[string]any{"institution_id": institutionId, "country_codes": self.countryCodes}
	if err := self.post(ctx, "/institutions/get_by_id", body, &answered); err != nil {
		return "", err
	}
	return answered.Institution.Name, nil
}

// plaidSyncPage is one answer from /transactions/sync. The transactions
// and accounts stay raw here so each can be kept whole as provider
// metadata before it is read into the fields this program uses.
type plaidSyncPage struct {
	Added      []json.RawMessage `json:"added"`
	Modified   []json.RawMessage `json:"modified"`
	Removed    []plaidRemoved    `json:"removed"`
	Accounts   []json.RawMessage `json:"accounts"`
	NextCursor string            `json:"next_cursor"`
	HasMore    bool              `json:"has_more"`
}

type plaidRemoved struct {
	TransactionID string `json:"transaction_id"`
}

type plaidTransaction struct {
	TransactionID          string      `json:"transaction_id"`
	AccountID              string      `json:"account_id"`
	Amount                 json.Number `json:"amount"`
	CurrencyCode           string      `json:"iso_currency_code"`
	UnofficialCurrencyCode string      `json:"unofficial_currency_code"`
	Date                   string      `json:"date"`
	AuthorizedDatetime     string      `json:"authorized_datetime"`
	Name                   string      `json:"name"`
	MerchantName           string      `json:"merchant_name"`
	IsPending              bool        `json:"pending"`
	PendingTransactionID   string      `json:"pending_transaction_id"`
	Category               *struct {
		Primary  string `json:"primary"`
		Detailed string `json:"detailed"`
	} `json:"personal_finance_category"`
}

type plaidAccount struct {
	AccountID    string `json:"account_id"`
	Name         string `json:"name"`
	OfficialName string `json:"official_name"`
	Mask         string `json:"mask"`
	AccountType  string `json:"type"`
	Balances     struct {
		CurrentBalance         json.Number `json:"current"`
		AvailableBalance       json.Number `json:"available"`
		CurrencyCode           string      `json:"iso_currency_code"`
		UnofficialCurrencyCode string      `json:"unofficial_currency_code"`
		LastUpdatedDatetime    string      `json:"last_updated_datetime"`
	} `json:"balances"`
}

// Sync reads everything that changed since the cursor, paging until Plaid
// says there is no more.
//
// Added and modified transactions both land in Added, because the reader
// upserts by provider transaction id either way. When a pending
// transaction posts, Plaid removes the pending one and adds a posted one
// naming it in PendingProviderTransactionID, so nothing here has to match
// them up.
func (self *Plaid) Sync(ctx context.Context, credential string, cursor string) (*SyncResult, error) {
	for attempt := 1; ; attempt++ {
		result, err := self.syncFrom(ctx, credential, cursor)
		var plaidError *PlaidError
		if errors.As(err, &plaidError) && plaidError.ErrorCode == plaidErrorCodeSyncMutation && attempt < plaidSyncAttemptLimit {
			// The pages read so far may describe a state that no longer
			// exists. Plaid's instruction is to throw them away and start
			// again from the cursor the sync began with.
			continue
		}
		return result, err
	}
}

func (self *Plaid) syncFrom(ctx context.Context, credential string, startingCursor string) (*SyncResult, error) {
	result := &SyncResult{NextCursor: startingCursor}
	transactionIndexByID := map[string]int{}
	accountIndexByID := map[string]int{}
	pageCursor := startingCursor

	for pageCount := 0; ; pageCount++ {
		if pageCount >= plaidSyncPageLimit {
			return nil, errors.New("finance: Plaid kept saying there was more after too many pages")
		}
		body := map[string]any{
			"access_token": credential,
			"count":        plaidSyncPageTransactionCount,
			"options":      map[string]any{"include_personal_finance_category": true},
		}
		// An empty cursor is how Plaid is asked for the whole history.
		if pageCursor != "" {
			body["cursor"] = pageCursor
		}
		var page plaidSyncPage
		if err := self.post(ctx, "/transactions/sync", body, &page); err != nil {
			return nil, err
		}

		for _, raw := range page.Accounts {
			account, err := plaidAccountFrom(raw)
			if err != nil {
				return nil, err
			}
			// Every page carries the accounts; the last page's balances
			// are the newest.
			if index, isSeen := accountIndexByID[account.ProviderAccountID]; isSeen {
				result.Accounts[index] = account
			} else {
				accountIndexByID[account.ProviderAccountID] = len(result.Accounts)
				result.Accounts = append(result.Accounts, account)
			}
		}
		for _, raw := range append(page.Added, page.Modified...) {
			transaction, err := plaidTransactionFrom(raw)
			if err != nil {
				return nil, err
			}
			// A transaction modified on a later page than it was added on
			// is kept once, as it was last described.
			if index, isSeen := transactionIndexByID[transaction.ProviderTransactionID]; isSeen {
				result.Added[index] = transaction
			} else {
				transactionIndexByID[transaction.ProviderTransactionID] = len(result.Added)
				result.Added = append(result.Added, transaction)
			}
		}
		for _, removed := range page.Removed {
			if removed.TransactionID != "" {
				result.RemovedProviderTransactionIDs = append(result.RemovedProviderTransactionIDs, removed.TransactionID)
			}
		}

		if page.NextCursor != "" {
			result.NextCursor = page.NextCursor
		}
		if !page.HasMore {
			break
		}
		if page.NextCursor == "" || page.NextCursor == pageCursor {
			return nil, errors.New("finance: Plaid said there was more without moving the cursor")
		}
		pageCursor = page.NextCursor
	}

	fillTransactionCurrencies(result)
	return result, nil
}

// fillTransactionCurrencies gives a transaction without a currency its
// account's. Plaid names one on nearly every transaction; the rare one
// without would otherwise be an amount in nothing.
func fillTransactionCurrencies(result *SyncResult) {
	currencyByAccountID := make(map[string]string, len(result.Accounts))
	for _, account := range result.Accounts {
		currencyByAccountID[account.ProviderAccountID] = account.CurrencyCode
	}
	for index := range result.Added {
		if result.Added[index].CurrencyCode == "" {
			result.Added[index].CurrencyCode = currencyByAccountID[result.Added[index].ProviderAccountID]
		}
	}
}

func plaidTransactionFrom(raw json.RawMessage) (Transaction, error) {
	var decoded plaidTransaction
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return Transaction{}, fmt.Errorf("finance: a Plaid transaction was not readable: %w", err)
	}
	if decoded.TransactionID == "" || decoded.AccountID == "" {
		return Transaction{}, errors.New("finance: a Plaid transaction has no id")
	}
	if _, err := time.Parse(time.DateOnly, decoded.Date); err != nil {
		return Transaction{}, fmt.Errorf("finance: Plaid transaction %s has no usable date", decoded.TransactionID)
	}
	amount, err := negatedJsonAmount(decoded.Amount)
	if err != nil {
		return Transaction{}, fmt.Errorf("finance: Plaid transaction %s has no usable amount: %w", decoded.TransactionID, err)
	}
	transaction := Transaction{
		ProviderTransactionID:        decoded.TransactionID,
		ProviderAccountID:            decoded.AccountID,
		PostedOn:                     decoded.Date,
		Amount:                       amount,
		CurrencyCode:                 firstNonEmpty(decoded.CurrencyCode, decoded.UnofficialCurrencyCode),
		Description:                  decoded.Name,
		MerchantName:                 decoded.MerchantName,
		IsPending:                    decoded.IsPending,
		PendingProviderTransactionID: decoded.PendingTransactionID,
		ProviderMetadata:             raw,
	}
	// Only a real moment. Most institutions give Plaid an authorized date
	// and no time, and a date turned into midnight UTC shows as the day
	// before for anybody west of Greenwich. The date stays in the
	// provider metadata.
	if decoded.AuthorizedDatetime != "" {
		if transactedAt, err := time.Parse(time.RFC3339, decoded.AuthorizedDatetime); err == nil {
			transaction.TransactedAt = &transactedAt
		}
	}
	if decoded.Category != nil {
		transaction.ProviderCategoryPrimary = decoded.Category.Primary
		transaction.ProviderCategoryDetailed = decoded.Category.Detailed
	}
	return transaction, nil
}

func plaidAccountFrom(raw json.RawMessage) (Account, error) {
	var decoded plaidAccount
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return Account{}, fmt.Errorf("finance: a Plaid account was not readable: %w", err)
	}
	if decoded.AccountID == "" {
		return Account{}, errors.New("finance: a Plaid account has no id")
	}
	currentBalance, err := canonicalJsonAmount(decoded.Balances.CurrentBalance)
	if err != nil {
		return Account{}, fmt.Errorf("finance: Plaid account %s has no usable balance: %w", decoded.AccountID, err)
	}
	availableBalance, err := canonicalJsonAmount(decoded.Balances.AvailableBalance)
	if err != nil {
		return Account{}, fmt.Errorf("finance: Plaid account %s has no usable available balance: %w", decoded.AccountID, err)
	}
	account := Account{
		ProviderAccountID: decoded.AccountID,
		AccountName:       firstNonEmpty(decoded.Name, decoded.OfficialName),
		AccountMask:       decoded.Mask,
		AccountKind:       plaidAccountKind(decoded.AccountType),
		CurrencyCode:      firstNonEmpty(decoded.Balances.CurrencyCode, decoded.Balances.UnofficialCurrencyCode),
		CurrentBalance:    currentBalance,
		AvailableBalance:  availableBalance,
		ProviderMetadata:  raw,
	}
	// Plaid names the moment only for some institutions; otherwise the
	// balance is as of this sync, which is when Plaid last refreshed it
	// for the answer it just gave.
	account.BalanceAt = time.Now().UTC()
	if decoded.Balances.LastUpdatedDatetime != "" {
		if balanceAt, err := time.Parse(time.RFC3339, decoded.Balances.LastUpdatedDatetime); err == nil {
			account.BalanceAt = balanceAt
		}
	}
	return account, nil
}

// plaidAccountKind maps Plaid's account type onto this program's, which
// is the same vocabulary with anything unexpected folded into other.
func plaidAccountKind(accountType string) string {
	switch accountType {
	case AccountKindDepository, AccountKindCredit, AccountKindLoan, AccountKindInvestment:
		return accountType
	case "brokerage":
		// Plaid's older name for investment accounts, still sent by some.
		return AccountKindInvestment
	}
	return AccountKindOther
}

// PlaidError is Plaid refusing a request, with the code and explanation
// it gave. It never carries the request, which holds the secret and the
// credential.
type PlaidError struct {
	StatusCode   int
	ErrorType    string
	ErrorCode    string
	ErrorMessage string
	RequestID    string
}

func (self *PlaidError) Error() string {
	text := fmt.Sprintf("finance: Plaid answered %d", self.StatusCode)
	if self.ErrorCode != "" {
		text += " " + self.ErrorCode
	}
	if self.ErrorMessage != "" {
		text += ": " + self.ErrorMessage
	}
	// Plaid's support asks for the request id first.
	if self.RequestID != "" {
		text += " (request " + self.RequestID + ")"
	}
	return text
}

// Unwrap makes a refused sign-in match ErrSignInRequired, so the reader
// can ask errors.Is without knowing Plaid's codes.
func (self *PlaidError) Unwrap() error {
	if self.ErrorCode == plaidErrorCodeItemLoginRequired {
		return ErrSignInRequired
	}
	return nil
}

// post sends one call with the operator's keys and decodes the answer into
// result, which may be nil for a call whose answer says nothing useful.
func (self *Plaid) post(ctx context.Context, path string, body map[string]any, result any) error {
	sent := make(map[string]any, len(body)+2)
	for key, value := range body {
		sent[key] = value
	}
	sent["client_id"] = self.clientId
	sent["secret"] = self.secret
	encoded, err := json.Marshal(sent)
	if err != nil {
		return fmt.Errorf("finance: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, self.baseUrl+path, bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("finance: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", UserAgent())
	request.Header.Set("Plaid-Version", plaidApiVersion)

	response, err := self.http.Do(request)
	if err != nil {
		return fmt.Errorf("finance: cannot reach Plaid: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	answer, err := io.ReadAll(io.LimitReader(response.Body, plaidResponseByteLimit+1))
	if err != nil {
		return fmt.Errorf("finance: cannot read Plaid's answer: %w", err)
	}
	if len(answer) > plaidResponseByteLimit {
		return errors.New("finance: Plaid's answer was too large")
	}

	if response.StatusCode/100 != 2 {
		var refused struct {
			ErrorType    string `json:"error_type"`
			ErrorCode    string `json:"error_code"`
			ErrorMessage string `json:"error_message"`
			RequestID    string `json:"request_id"`
		}
		// Not every refusal is Plaid's JSON (a proxy's error page is not),
		// and the status alone is still worth reporting.
		_ = json.Unmarshal(answer, &refused)
		return &PlaidError{
			StatusCode:   response.StatusCode,
			ErrorType:    refused.ErrorType,
			ErrorCode:    refused.ErrorCode,
			ErrorMessage: refused.ErrorMessage,
			RequestID:    refused.RequestID,
		}
	}
	if result == nil {
		return nil
	}
	if err := json.Unmarshal(answer, result); err != nil {
		return fmt.Errorf("finance: Plaid's answer to %s was not readable: %w", path, err)
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
