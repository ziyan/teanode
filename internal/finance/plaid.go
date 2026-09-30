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
	plaidErrorCodeInvalidProduct    = "INVALID_PRODUCT"
)

// The one product every finance source is linked with, and the one that
// reads holdings and investment transactions. The others a link may ask
// for are optional: Plaid adds each where the institution and the accounts
// the person chose support it.
const (
	plaidProductTransactions = "transactions"
	plaidProductInvestments  = "investments"
)

const (
	// How far back investment transactions are read the first time, Plaid's
	// ceiling; after that, from this many days before the last read, so a
	// trade the institution reports late is still picked up.
	plaidInvestmentsDaysRequested = 730
	plaidInvestmentsOverlapDays   = 30

	// Investment transactions per page of /investments/transactions/get,
	// Plaid's maximum, and the most pages read in one sync.
	plaidInvestmentsPageCount = 500
	plaidInvestmentsPageLimit = 100
)

// plaidInvestmentsUnavailableCodes are Plaid's codes for a finance source
// whose holdings cannot be read this time, or at all: the data is not ready
// yet after a link, it has no investment accounts, or the product was never
// added to it. None of them fails the sync.
var plaidInvestmentsUnavailableCodes = map[string]bool{
	"PRODUCT_NOT_READY":           true,
	"NO_INVESTMENT_ACCOUNTS":      true,
	"NO_INVESTMENT_AUTH_ACCOUNTS": true,
	"PRODUCTS_NOT_SUPPORTED":      true,
	"ADDITIONAL_CONSENT_REQUIRED": true,
}

// plaidCredentialRefusedCodes are Plaid's codes for a credential that no
// sign-in can mend: the link was removed or never existed, the credential
// is not one Plaid issued to this client, or the person withdrew their
// consent at Plaid or at the institution. Asking again fails the same way
// every time.
var plaidCredentialRefusedCodes = map[string]bool{
	"ITEM_NOT_FOUND":          true,
	"INVALID_ACCESS_TOKEN":    true,
	"ACCESS_NOT_GRANTED":      true,
	"USER_PERMISSION_REVOKED": true,
	"USER_ACCOUNT_REVOKED":    true,
}

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
	optionalProducts := self.optionalProducts()
	if credentialForRepair != "" {
		body["access_token"] = credentialForRepair
	} else {
		body["products"] = []string{plaidProductTransactions}
		if len(optionalProducts) > 0 {
			body["optional_products"] = optionalProducts
		}
		body["transactions"] = map[string]any{"days_requested": plaidDaysRequested}
	}
	var answered struct {
		LinkToken string `json:"link_token"`
	}
	err := self.post(ctx, "/link/token/create", body, &answered)
	var plaidError *PlaidError
	if errors.As(err, &plaidError) && plaidError.ErrorCode == plaidErrorCodeInvalidProduct && body["optional_products"] != nil {
		// The operator turned on a product their Plaid account is not
		// enabled for. Linking without it beats not linking at all.
		log.Warningf("Plaid refused the optional products %v (%s); linking with transactions only", optionalProducts, plaidError.ErrorMessage)
		delete(body, "optional_products")
		err = self.post(ctx, "/link/token/create", body, &answered)
	}
	if err != nil {
		return "", err
	}
	if answered.LinkToken == "" {
		return "", errors.New("finance: Plaid answered without a link token")
	}
	return answered.LinkToken, nil
}

// optionalProducts are the configured products other than transactions,
// which a link asks for only where the institution has them.
func (self *Plaid) optionalProducts() []string {
	var optional []string
	for _, product := range self.products {
		if product != plaidProductTransactions {
			optional = append(optional, product)
		}
	}
	return optional
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

// DescribeCredential asks Plaid about a credential made elsewhere with
// this operator's keys: Plaid's id for the link, and the institution it
// reaches. A credential Plaid does not know, or issued to another client
// id, answers an error matching ErrCredentialRefused. The institution's
// name is InstitutionName's to find.
func (self *Plaid) DescribeCredential(ctx context.Context, credential string) (*CredentialDescription, error) {
	if strings.TrimSpace(credential) == "" {
		return nil, errors.New("finance: no credential to describe")
	}
	var answered struct {
		Item struct {
			ItemID        string `json:"item_id"`
			InstitutionID string `json:"institution_id"`
		} `json:"item"`
	}
	if err := self.post(ctx, "/item/get", map[string]any{"access_token": credential}, &answered); err != nil {
		return nil, err
	}
	if answered.Item.ItemID == "" {
		return nil, errors.New("finance: Plaid described the credential without an id for its link")
	}
	return &CredentialDescription{ProviderReference: answered.Item.ItemID, InstitutionID: answered.Item.InstitutionID}, nil
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

	// TransactionsUpdateStatus is how far Plaid has got gathering the
	// finance source's history: NOT_READY and INITIAL_UPDATE_COMPLETE
	// for a while after a link, then HISTORICAL_UPDATE_COMPLETE. Empty
	// from a Plaid that does not say.
	TransactionsUpdateStatus string `json:"transactions_update_status"`
}

// plaidHistoryCompleteStatuses are the update statuses that mean nothing
// more of the history is on its way, or that Plaid does not know.
var plaidHistoryCompleteStatuses = map[string]bool{
	"":                                   true,
	"HISTORICAL_UPDATE_COMPLETE":         true,
	"TRANSACTIONS_UPDATE_STATUS_UNKNOWN": true,
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
//
// Then, for a finance source linked with investments, it reads the
// holdings and the investment transactions: trades land in Trades, and
// the investment transactions that move cash in or out (dividends,
// interest, fees, deposits, withdrawals) in Added, with a personal finance
// category so they are treated like any other transaction.
func (self *Plaid) Sync(ctx context.Context, credential string, cursor string) (*SyncResult, error) {
	parsedCursor := parsePlaidCursor(cursor)
	var syncResult *SyncResult
	for attempt := 1; ; attempt++ {
		var err error
		syncResult, err = self.syncFrom(ctx, credential, parsedCursor.TransactionsCursor)
		var plaidError *PlaidError
		if errors.As(err, &plaidError) && plaidError.ErrorCode == plaidErrorCodeSyncMutation && attempt < plaidSyncAttemptLimit {
			// The pages read so far may describe a state that no longer
			// exists. Plaid's instruction is to throw them away and start
			// again from the cursor the sync began with.
			continue
		}
		if err != nil {
			return nil, err
		}
		break
	}
	parsedCursor.TransactionsCursor = syncResult.NextCursor
	if err := self.readInvestments(ctx, credential, &parsedCursor, syncResult, time.Now().UTC()); err != nil {
		return nil, err
	}
	fillTransactionCurrencies(syncResult)
	syncResult.NextCursor = parsedCursor.encode()
	return syncResult, nil
}

// plaidCursor is what a Plaid finance source keeps between syncs: Plaid's
// cursor for /transactions/sync, and the last day investment transactions
// were read through, which Plaid keeps no cursor for.
type plaidCursor struct {
	TransactionsCursor     string `json:"transactionsCursor"`
	InvestmentsReadThrough string `json:"investmentsReadThrough,omitempty"`
}

// parsePlaidCursor reads a finance source's cursor: the JSON object, or a
// plain string, which is Plaid's transactions cursor as it was kept before
// investments were read.
func parsePlaidCursor(cursor string) plaidCursor {
	if strings.HasPrefix(cursor, "{") {
		var parsed plaidCursor
		if err := json.Unmarshal([]byte(cursor), &parsed); err == nil {
			return parsed
		}
	}
	return plaidCursor{TransactionsCursor: cursor}
}

// encode writes the cursor back. A finance source that has never read
// investments keeps the plain string, which a server without this code
// still reads.
func (self plaidCursor) encode() string {
	if self.InvestmentsReadThrough == "" {
		return self.TransactionsCursor
	}
	encoded, _ := json.Marshal(self)
	return string(encoded)
}

func (self *Plaid) syncFrom(ctx context.Context, credential string, startingCursor string) (*SyncResult, error) {
	syncResult := &SyncResult{NextCursor: startingCursor}
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
				syncResult.Accounts[index] = account
			} else {
				accountIndexByID[account.ProviderAccountID] = len(syncResult.Accounts)
				syncResult.Accounts = append(syncResult.Accounts, account)
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
				syncResult.Added[index] = transaction
			} else {
				transactionIndexByID[transaction.ProviderTransactionID] = len(syncResult.Added)
				syncResult.Added = append(syncResult.Added, transaction)
			}
		}
		for _, removed := range page.Removed {
			if removed.TransactionID != "" {
				syncResult.RemovedProviderTransactionIDs = append(syncResult.RemovedProviderTransactionIDs, removed.TransactionID)
			}
		}

		if page.NextCursor != "" {
			syncResult.NextCursor = page.NextCursor
		}
		if !page.HasMore {
			syncResult.IsHistoryIncomplete = !plaidHistoryCompleteStatuses[page.TransactionsUpdateStatus]
			break
		}
		if page.NextCursor == "" || page.NextCursor == pageCursor {
			return nil, errors.New("finance: Plaid said there was more without moving the cursor")
		}
		pageCursor = page.NextCursor
	}
	return syncResult, nil
}

// readInvestments adds a finance source's investments to its sync, when
// the source was linked with them: the investment accounts and their
// holdings, the securities, the trades, and the investment transactions
// that move cash. Holdings Plaid cannot give this time leave the result as
// it was, with a warning, and the read-through day unmoved.
func (self *Plaid) readInvestments(ctx context.Context, credential string, parsedCursor *plaidCursor, syncResult *SyncResult, now time.Time) error {
	isLinkedWithInvestments, err := self.isLinkedWithInvestments(ctx, credential)
	if err != nil || !isLinkedWithInvestments {
		return err
	}

	var holdingsAnswer struct {
		Accounts   []json.RawMessage `json:"accounts"`
		Holdings   []json.RawMessage `json:"holdings"`
		Securities []json.RawMessage `json:"securities"`
	}
	err = self.post(ctx, "/investments/holdings/get", map[string]any{"access_token": credential}, &holdingsAnswer)
	var plaidError *PlaidError
	if errors.As(err, &plaidError) && plaidInvestmentsUnavailableCodes[plaidError.ErrorCode] {
		syncResult.ProviderWarnings = append(syncResult.ProviderWarnings, "Plaid could not give the holdings this time ("+plaidError.ErrorCode+")")
		return nil
	}
	if err != nil {
		return err
	}

	securityByID := map[string]Security{}
	addSecurities := func(raws []json.RawMessage) error {
		for _, raw := range raws {
			security, err := plaidSecurityFrom(raw)
			if err != nil {
				return err
			}
			if _, isSeen := securityByID[security.ProviderSecurityID]; !isSeen {
				syncResult.Securities = append(syncResult.Securities, security)
			}
			securityByID[security.ProviderSecurityID] = security
		}
		return nil
	}
	if err := addSecurities(holdingsAnswer.Securities); err != nil {
		return err
	}

	isAccountSeen := make(map[string]bool, len(syncResult.Accounts))
	for _, account := range syncResult.Accounts {
		isAccountSeen[account.ProviderAccountID] = true
	}
	for _, raw := range holdingsAnswer.Accounts {
		account, err := plaidAccountFrom(raw)
		if err != nil {
			return err
		}
		if account.AccountKind != AccountKindInvestment {
			continue
		}
		// Every investment account in the answer had its holdings read,
		// including one that holds nothing.
		syncResult.HoldingsReadAccountIDs = append(syncResult.HoldingsReadAccountIDs, account.ProviderAccountID)
		if !isAccountSeen[account.ProviderAccountID] {
			isAccountSeen[account.ProviderAccountID] = true
			syncResult.Accounts = append(syncResult.Accounts, account)
		}
	}
	for _, raw := range holdingsAnswer.Holdings {
		holding, err := plaidHoldingFrom(raw)
		if err != nil {
			return err
		}
		// Cash is held as a security of kind cash; it is the account's
		// own cash, valued on the account's asset, not a holding of it.
		if securityByID[holding.ProviderSecurityID].SecurityKind == SecurityKindCash {
			continue
		}
		syncResult.Holdings = append(syncResult.Holdings, holding)
	}

	startDay := now.AddDate(0, 0, -plaidInvestmentsDaysRequested)
	if readThrough, err := time.Parse(time.DateOnly, parsedCursor.InvestmentsReadThrough); err == nil {
		startDay = readThrough.AddDate(0, 0, -plaidInvestmentsOverlapDays)
	}
	endDay := now.Format(time.DateOnly)
	for offset, pageCount := 0, 0; ; pageCount++ {
		if pageCount >= plaidInvestmentsPageLimit {
			return errors.New("finance: Plaid kept saying there were more investment transactions after too many pages")
		}
		var page struct {
			InvestmentTransactions []json.RawMessage `json:"investment_transactions"`
			Securities             []json.RawMessage `json:"securities"`
			TotalCount             int               `json:"total_investment_transactions"`
		}
		body := map[string]any{
			"access_token": credential,
			"start_date":   startDay.Format(time.DateOnly),
			"end_date":     endDay,
			"options":      map[string]any{"count": plaidInvestmentsPageCount, "offset": offset},
		}
		err := self.post(ctx, "/investments/transactions/get", body, &page)
		if errors.As(err, &plaidError) && plaidInvestmentsUnavailableCodes[plaidError.ErrorCode] {
			syncResult.ProviderWarnings = append(syncResult.ProviderWarnings, "Plaid could not give the investment transactions this time ("+plaidError.ErrorCode+")")
			return nil
		}
		if err != nil {
			return err
		}
		if err := addSecurities(page.Securities); err != nil {
			return err
		}
		for _, raw := range page.InvestmentTransactions {
			if err := addPlaidInvestmentTransaction(syncResult, raw); err != nil {
				return err
			}
		}
		offset += len(page.InvestmentTransactions)
		if len(page.InvestmentTransactions) == 0 || offset >= page.TotalCount {
			break
		}
	}
	parsedCursor.InvestmentsReadThrough = endDay
	return nil
}

// isLinkedWithInvestments asks Plaid whether the finance source has the
// investments product, which Plaid adds at the link where the institution
// and the accounts the person chose support it. Reading the holdings of
// one without it would start billing for a product it does not use.
func (self *Plaid) isLinkedWithInvestments(ctx context.Context, credential string) (bool, error) {
	var answered struct {
		Item struct {
			Products       []string `json:"products"`
			BilledProducts []string `json:"billed_products"`
		} `json:"item"`
	}
	if err := self.post(ctx, "/item/get", map[string]any{"access_token": credential}, &answered); err != nil {
		return false, err
	}
	for _, product := range append(answered.Item.Products, answered.Item.BilledProducts...) {
		if product == plaidProductInvestments {
			return true, nil
		}
	}
	return false, nil
}

// fillTransactionCurrencies gives a transaction without a currency its
// account's. Plaid names one on nearly every transaction; the rare one
// without would otherwise be an amount in nothing.
func fillTransactionCurrencies(syncResult *SyncResult) {
	currencyByAccountID := make(map[string]string, len(syncResult.Accounts))
	for _, account := range syncResult.Accounts {
		currencyByAccountID[account.ProviderAccountID] = account.CurrencyCode
	}
	for index := range syncResult.Added {
		if syncResult.Added[index].CurrencyCode == "" {
			syncResult.Added[index].CurrencyCode = currencyByAccountID[syncResult.Added[index].ProviderAccountID]
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
		// Plaid reports a card's or a loan's balance as the amount owed.
		IsOwedBalancePositive: true,
		ProviderMetadata:      raw,
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

type plaidSecurity struct {
	SecurityID             string      `json:"security_id"`
	Name                   string      `json:"name"`
	TickerSymbol           string      `json:"ticker_symbol"`
	SecurityType           string      `json:"type"`
	ClosePrice             json.Number `json:"close_price"`
	ClosePriceAsOf         string      `json:"close_price_as_of"`
	CurrencyCode           string      `json:"iso_currency_code"`
	UnofficialCurrencyCode string      `json:"unofficial_currency_code"`
}

func plaidSecurityFrom(raw json.RawMessage) (Security, error) {
	var decoded plaidSecurity
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return Security{}, fmt.Errorf("finance: a Plaid security was not readable: %w", err)
	}
	if decoded.SecurityID == "" {
		return Security{}, errors.New("finance: a Plaid security has no id")
	}
	closePrice, err := canonicalJsonQuantity(decoded.ClosePrice)
	if err != nil {
		return Security{}, fmt.Errorf("finance: Plaid security %s has no usable close price: %w", decoded.SecurityID, err)
	}
	security := Security{
		ProviderSecurityID: decoded.SecurityID,
		TickerSymbol:       decoded.TickerSymbol,
		SecurityName:       firstNonEmpty(decoded.Name, decoded.TickerSymbol, decoded.SecurityID),
		SecurityKind:       plaidSecurityKind(decoded.SecurityType),
		CurrencyCode:       firstNonEmpty(decoded.CurrencyCode, decoded.UnofficialCurrencyCode),
		ClosePrice:         closePrice,
		ProviderMetadata:   raw,
	}
	if _, err := time.Parse(time.DateOnly, decoded.ClosePriceAsOf); err == nil && closePrice != "" {
		security.ClosePriceOn = decoded.ClosePriceAsOf
	}
	return security, nil
}

// plaidSecurityKind maps Plaid's security type, which writes two words with
// a space, onto this program's vocabulary.
func plaidSecurityKind(securityType string) string {
	securityKind := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(securityType)), " ", "_")
	switch securityKind {
	case SecurityKindCash, SecurityKindCryptocurrency, SecurityKindDerivative, SecurityKindEquity, SecurityKindETF,
		SecurityKindFixedIncome, SecurityKindLoan, SecurityKindMutualFund:
		return securityKind
	}
	return SecurityKindOther
}

type plaidHolding struct {
	AccountID              string      `json:"account_id"`
	SecurityID             string      `json:"security_id"`
	Quantity               json.Number `json:"quantity"`
	InstitutionPrice       json.Number `json:"institution_price"`
	InstitutionValue       json.Number `json:"institution_value"`
	CostBasis              json.Number `json:"cost_basis"`
	CurrencyCode           string      `json:"iso_currency_code"`
	UnofficialCurrencyCode string      `json:"unofficial_currency_code"`
}

func plaidHoldingFrom(raw json.RawMessage) (Holding, error) {
	var decoded plaidHolding
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return Holding{}, fmt.Errorf("finance: a Plaid holding was not readable: %w", err)
	}
	if decoded.AccountID == "" || decoded.SecurityID == "" {
		return Holding{}, errors.New("finance: a Plaid holding names no account or security")
	}
	heldQuantity, err := canonicalJsonQuantity(decoded.Quantity)
	if err != nil || heldQuantity == "" {
		return Holding{}, fmt.Errorf("finance: a Plaid holding of %s has no usable quantity", decoded.SecurityID)
	}
	unitPrice, err := canonicalJsonQuantity(decoded.InstitutionPrice)
	if err != nil {
		return Holding{}, fmt.Errorf("finance: a Plaid holding of %s has no usable price: %w", decoded.SecurityID, err)
	}
	holdingValue, err := canonicalJsonAmount(decoded.InstitutionValue)
	if err != nil || holdingValue == "" {
		return Holding{}, fmt.Errorf("finance: a Plaid holding of %s has no usable value", decoded.SecurityID)
	}
	costBasis, err := canonicalJsonAmount(decoded.CostBasis)
	if err != nil {
		return Holding{}, fmt.Errorf("finance: a Plaid holding of %s has no usable cost basis: %w", decoded.SecurityID, err)
	}
	return Holding{
		ProviderAccountID:  decoded.AccountID,
		ProviderSecurityID: decoded.SecurityID,
		HeldQuantity:       heldQuantity,
		UnitPrice:          unitPrice,
		HoldingValue:       holdingValue,
		CostBasis:          costBasis,
		CurrencyCode:       firstNonEmpty(decoded.CurrencyCode, decoded.UnofficialCurrencyCode),
		ProviderMetadata:   raw,
	}, nil
}

type plaidInvestmentTransaction struct {
	InvestmentTransactionID string      `json:"investment_transaction_id"`
	AccountID               string      `json:"account_id"`
	SecurityID              string      `json:"security_id"`
	Date                    string      `json:"date"`
	Name                    string      `json:"name"`
	Quantity                json.Number `json:"quantity"`
	Amount                  json.Number `json:"amount"`
	Price                   json.Number `json:"price"`
	Fees                    json.Number `json:"fees"`
	TransactionType         string      `json:"type"`
	TransactionSubtype      string      `json:"subtype"`
	CurrencyCode            string      `json:"iso_currency_code"`
	UnofficialCurrencyCode  string      `json:"unofficial_currency_code"`
}

// addPlaidInvestmentTransaction files one of Plaid's investment
// transactions: a buy, sell, cancel or security transfer as a trade, and
// one that moves cash as a finance transaction.
func addPlaidInvestmentTransaction(syncResult *SyncResult, raw json.RawMessage) error {
	var decoded plaidInvestmentTransaction
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return fmt.Errorf("finance: a Plaid investment transaction was not readable: %w", err)
	}
	if decoded.InvestmentTransactionID == "" || decoded.AccountID == "" {
		return errors.New("finance: a Plaid investment transaction has no id")
	}
	if _, err := time.Parse(time.DateOnly, decoded.Date); err != nil {
		return fmt.Errorf("finance: Plaid investment transaction %s has no usable date", decoded.InvestmentTransactionID)
	}
	// Plaid's amount is positive when cash leaves the account.
	amount, err := negatedJsonAmount(decoded.Amount)
	if err != nil {
		return fmt.Errorf("finance: Plaid investment transaction %s has no usable amount: %w", decoded.InvestmentTransactionID, err)
	}
	currencyCode := firstNonEmpty(decoded.CurrencyCode, decoded.UnofficialCurrencyCode)

	transactionType := strings.ToLower(strings.TrimSpace(decoded.TransactionType))
	switch transactionType {
	case TradeKindBuy, TradeKindSell, TradeKindCancel, TradeKindTransfer:
		tradedQuantity, err := canonicalJsonQuantity(decoded.Quantity)
		if err != nil {
			return fmt.Errorf("finance: Plaid trade %s has no usable quantity: %w", decoded.InvestmentTransactionID, err)
		}
		unitPrice, err := canonicalJsonQuantity(decoded.Price)
		if err != nil {
			return fmt.Errorf("finance: Plaid trade %s has no usable price: %w", decoded.InvestmentTransactionID, err)
		}
		feeAmount, err := canonicalJsonAmount(decoded.Fees)
		if err != nil {
			return fmt.Errorf("finance: Plaid trade %s has no usable fees: %w", decoded.InvestmentTransactionID, err)
		}
		syncResult.Trades = append(syncResult.Trades, Trade{
			ProviderTradeID:    decoded.InvestmentTransactionID,
			ProviderAccountID:  decoded.AccountID,
			ProviderSecurityID: decoded.SecurityID,
			TradedOn:           decoded.Date,
			TradeKind:          transactionType,
			TradeSubkind:       decoded.TransactionSubtype,
			TradedQuantity:     tradedQuantity,
			UnitPrice:          unitPrice,
			TradeAmount:        amount,
			FeeAmount:          feeAmount,
			CurrencyCode:       currencyCode,
			Description:        decoded.Name,
			ProviderMetadata:   raw,
		})
		return nil
	}

	primary, detailed := plaidInvestmentCashCategory(transactionType, decoded.TransactionSubtype, strings.HasPrefix(amount, "-"))
	syncResult.Added = append(syncResult.Added, Transaction{
		ProviderTransactionID:    decoded.InvestmentTransactionID,
		ProviderAccountID:        decoded.AccountID,
		PostedOn:                 decoded.Date,
		Amount:                   amount,
		CurrencyCode:             currencyCode,
		Description:              decoded.Name,
		ProviderCategoryPrimary:  primary,
		ProviderCategoryDetailed: detailed,
		ProviderMetadata:         raw,
	})
	return nil
}

// plaidInvestmentCashCategory gives an investment transaction that moves
// cash the personal finance category Plaid gives ordinary transactions, so
// the provider category mapping, transfer detection and the categorize
// model treat it like one: dividends, capital gain payouts and interest
// are income; fees are fees; taxes are taxes; money put in or taken out is
// a transfer, told by its sign when the subtype says nothing.
func plaidInvestmentCashCategory(transactionType, transactionSubtype string, isCashOut bool) (primary, detailed string) {
	subtype := strings.ToLower(strings.TrimSpace(transactionSubtype))
	switch {
	case transactionType == "fee" || strings.HasSuffix(subtype, "fee") || subtype == "margin expense":
		return "BANK_FEES", "BANK_FEES_OTHER_BANK_FEES"
	case strings.Contains(subtype, "tax"):
		return "GOVERNMENT_AND_NON_PROFIT", "GOVERNMENT_AND_NON_PROFIT_TAX_PAYMENT"
	case strings.Contains(subtype, "dividend") || strings.Contains(subtype, "capital gain") || subtype == "unqualified gain":
		return "INCOME", "INCOME_DIVIDENDS"
	case subtype == "interest":
		return "INCOME", "INCOME_INTEREST_EARNED"
	case subtype == "deposit" || subtype == "contribution":
		return "TRANSFER_IN", "TRANSFER_IN_INVESTMENT_AND_RETIREMENT_FUNDS"
	case subtype == "withdrawal" || subtype == "distribution":
		return "TRANSFER_OUT", "TRANSFER_OUT_INVESTMENT_AND_RETIREMENT_FUNDS"
	case isCashOut:
		return "TRANSFER_OUT", "TRANSFER_OUT_INVESTMENT_AND_RETIREMENT_FUNDS"
	}
	return "TRANSFER_IN", "TRANSFER_IN_INVESTMENT_AND_RETIREMENT_FUNDS"
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

// Unwrap makes a refused sign-in match ErrSignInRequired, and a refused
// credential ErrCredentialRefused, so the reader can ask errors.Is without
// knowing Plaid's codes.
func (self *PlaidError) Unwrap() error {
	if self.ErrorCode == plaidErrorCodeItemLoginRequired {
		return ErrSignInRequired
	}
	if plaidCredentialRefusedCodes[self.ErrorCode] {
		return ErrCredentialRefused
	}
	return nil
}

// post sends one call with the operator's keys and decodes the answer into
// answered, which may be nil for a call whose answer says nothing useful.
func (self *Plaid) post(ctx context.Context, path string, body map[string]any, answered any) error {
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
	if answered == nil {
		return nil
	}
	if err := json.Unmarshal(answer, answered); err != nil {
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
