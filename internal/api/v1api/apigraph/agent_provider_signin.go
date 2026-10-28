package apigraph

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// Signing a provider in from the dashboard, for one that runs on a
// person's plan rather than a key.
//
// The server asks the service for a one-time code and the operator types it
// on the service's own page, from any device: the server never sees their
// password, and needs no browser of its own. See llm.DeviceSignIn.

// AgentProviderSignInMutation signs a provider in.
type AgentProviderSignInMutation interface {
	// Start signing a provider in: the code to type and the page to type
	// it on. The provider is the one named, created when the sign-in
	// finishes if there is none yet. Needs server:manage.
	BeginAgentProviderSignIn(ctx context.Context, arguments BeginAgentProviderSignInArguments) (*AgentProviderSignInView, error)

	// Wait a little for the code to be entered. Until it is, the answer
	// says so and the page asks again; once it is, the provider is saved
	// signed in and the answer names the account. Needs server:manage.
	FinishAgentProviderSignIn(ctx context.Context, arguments FinishAgentProviderSignInArguments) (*AgentProviderSignInView, error)
}

// BeginAgentProviderSignInArguments name the provider to sign in.
type BeginAgentProviderSignInArguments struct {
	Provider string `json:"provider"`
}

// FinishAgentProviderSignInArguments name the sign-in to wait on.
type FinishAgentProviderSignInArguments struct {
	SignInID string `json:"signInId"`
}

// AgentProviderSignInView is a sign-in as the page shows it.
type AgentProviderSignInView struct {
	SignInID            string    `json:"signInId"`
	Provider            string    `json:"provider"`
	UserCode            string    `json:"userCode"`
	VerificationAddress string    `json:"verificationAddress"`
	ExpiresAt           time.Time `json:"expiresAt"`

	// IsSignedIn says the code was entered and the provider saved.
	IsSignedIn bool `json:"isSignedIn"`
	// Account and Plan are what it signed in as, once it has.
	Account string `json:"account"`
	Plan    string `json:"plan"`
}

// finishWait is how long one finish waits before answering that the code
// has not been entered yet: short enough to sit inside a request.
const finishWait = 25 * time.Second

// deviceSignInEndpointsFor is where a provider kind signs in. A variable so
// that a test can sign in at a service of its own.
var deviceSignInEndpointsFor = llm.DeviceSignInEndpointsFor

// finishing holds the sign-ins this instance is waiting on, so that a second
// finish of the same one waits for the first rather than redeeming its code
// again: a code is redeemed once.
var finishing = struct {
	sync.Mutex
	byID map[string]*finishingSignIn
}{byID: map[string]*finishingSignIn{}}

type finishingSignIn struct {
	mutex sync.Mutex

	// callerCount is how many finishes hold or wait for the mutex, so that
	// the entry goes when the last of them is done.
	callerCount int
}

func lockFinishing(signInId string) func() {
	finishing.Lock()
	entry := finishing.byID[signInId]
	if entry == nil {
		entry = &finishingSignIn{}
		finishing.byID[signInId] = entry
	}
	entry.callerCount++
	finishing.Unlock()
	entry.mutex.Lock()
	return func() {
		entry.mutex.Unlock()
		finishing.Lock()
		entry.callerCount--
		if entry.callerCount == 0 {
			delete(finishing.byID, signInId)
		}
		finishing.Unlock()
	}
}

func (self *graph) BeginAgentProviderSignIn(ctx context.Context, arguments BeginAgentProviderSignInArguments) (*AgentProviderSignInView, error) {
	if _, err := self.requirePermission(ctx, models.PermissionServerManage); err != nil {
		return nil, err
	}
	provider := strings.TrimSpace(arguments.Provider)
	if provider == "" || strings.Contains(provider, ":") {
		return nil, errors.New("name the provider to sign in, without a colon")
	}
	if declared := self.config.Current().Agent.Provider(provider); declared != nil && declared.Kind != config.AgentProviderKindCodex {
		return nil, fmt.Errorf("the provider %q takes a key, not a sign-in", provider)
	}
	endpoints, err := deviceSignInEndpointsFor(config.AgentProviderKindCodex)
	if err != nil {
		return nil, err
	}
	started, err := llm.BeginDeviceSignInAt(ctx, endpoints)
	if err != nil {
		return nil, err
	}
	identifier := make([]byte, 16)
	if _, err := rand.Read(identifier); err != nil {
		return nil, err
	}
	signInId := hex.EncodeToString(identifier)
	// Kept in the database rather than in this process, because the page
	// asks again on whichever instance its next request reaches.
	record := started.Record()
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) error {
		return tx.CreateAgentProviderSignIn(&db.AgentProviderSignIn{
			ID: signInId, ProviderName: provider, UserCode: record.UserCode,
			VerificationAddress: record.VerificationAddress, DeviceAuthID: record.DeviceAuthID,
			PollInterval: record.Interval, ExpiresAt: record.ExpiresAt,
		}, time.Now())
	}); err != nil {
		return nil, err
	}
	return &AgentProviderSignInView{
		SignInID: signInId, Provider: provider, UserCode: started.UserCode,
		VerificationAddress: started.VerificationAddress, ExpiresAt: started.ExpiresAt,
	}, nil
}

func (self *graph) FinishAgentProviderSignIn(ctx context.Context, arguments FinishAgentProviderSignInArguments) (*AgentProviderSignInView, error) {
	if _, err := self.requirePermission(ctx, models.PermissionServerManage); err != nil {
		return nil, err
	}
	signInId := strings.TrimSpace(arguments.SignInID)
	unlock := lockFinishing(signInId)
	defer unlock()

	var waiting *db.AgentProviderSignIn
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		waiting, err = tx.GetAgentProviderSignIn(signInId)
		return err
	}); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil, errors.New("that sign-in is not waiting any more; start it again")
		}
		return nil, err
	}
	endpoints, err := deviceSignInEndpointsFor(config.AgentProviderKindCodex)
	if err != nil {
		return nil, err
	}
	started, err := llm.ResumeDeviceSignIn(endpoints, llm.DeviceSignInRecord{
		UserCode: waiting.UserCode, VerificationAddress: waiting.VerificationAddress,
		DeviceAuthID: waiting.DeviceAuthID, Interval: waiting.PollInterval, ExpiresAt: waiting.ExpiresAt,
	})
	if err != nil {
		return nil, err
	}
	view := &AgentProviderSignInView{
		SignInID: signInId, Provider: waiting.ProviderName, UserCode: started.UserCode,
		VerificationAddress: started.VerificationAddress, ExpiresAt: started.ExpiresAt,
	}
	waitContext, cancel := context.WithTimeout(ctx, finishWait)
	defer cancel()
	result, err := started.Wait(waitContext)
	if errors.Is(err, llm.ErrSignInPending) {
		return view, nil
	}
	// Finished, or expired, or refused: either way it is not waiting any more.
	if deleteErr := self.database.TransactionContext(ctx, func(tx db.Transaction) error {
		return tx.DeleteAgentProviderSignIn(signInId)
	}); deleteErr != nil {
		log.Warningf("the finished sign-in of the %s provider could not be forgotten: %s", waiting.ProviderName, deleteErr)
	}
	if err != nil {
		return nil, err
	}
	if err := self.config.Update(func(configuration *config.Configuration) error {
		return keepSignIn(configuration, waiting.ProviderName, result)
	}); err != nil {
		return nil, err
	}
	log.Noticef("%s signed the %s provider in (plan %q)", api.ContextAuthenticatedUsername(ctx), waiting.ProviderName, result.Plan)
	view.IsSignedIn, view.Account, view.Plan = true, result.Account, result.Plan
	return view, nil
}

// keepSignIn saves a finished sign-in to the provider it was for, creating
// the provider when there is none of that name yet.
func keepSignIn(configuration *config.Configuration, provider string, result *llm.SignInResult) error {
	for index := range configuration.Agent.Providers {
		declared := &configuration.Agent.Providers[index]
		if declared.Name != provider {
			continue
		}
		if declared.Kind != config.AgentProviderKindCodex {
			return fmt.Errorf("the provider %q takes a key, not a sign-in", provider)
		}
		declared.RefreshToken, declared.Account = result.RefreshToken, result.Account
		return nil
	}
	isEnabled := true
	configuration.Agent.Providers = append(configuration.Agent.Providers, config.AgentProvider{
		Name: provider, Kind: config.AgentProviderKindCodex, Enabled: &isEnabled,
		RefreshToken: result.RefreshToken, Account: result.Account,
	})
	return nil
}
