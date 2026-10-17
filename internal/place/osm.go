package place

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/config"
)

// requestInterval is the least time between two requests to a provider. The
// OpenStreetMap Foundation's public server allows one a second from an
// application, and a Photon server an operator runs does not mind being asked
// no faster than that either.
const requestInterval = time.Second

// requestTimeout is how long one request to a provider may take.
const requestTimeout = 10 * time.Second

// maximumRequestWait is the longest a caller waits for its turn. A caller whose
// turn is further away is refused: a search a person is waiting for gives up
// and says so, and the place-naming job tries again on its next run, where a
// queue that grows without end would keep every other caller waiting behind it.
const maximumRequestWait = 5 * time.Second

// maximumResponseBytes bounds what is read from a provider, since the
// address is the operator's to set and the answer is not ours to trust.
const maximumResponseBytes = 4 << 20

// Found is a named place on the map: what a provider answered for a point or
// for a search. DistanceMeters is its distance from the point that was asked
// about, for the answers that have one.
type Found struct {
	PlaceName        string
	OSMType          string
	OSMID            int64
	OSMCategory      string
	AddressText      string
	LatitudeDegrees  float64
	LongitudeDegrees float64
	DistanceMeters   float64
}

// Provider names points and finds places from OpenStreetMap data. Every call
// waits its turn behind the one limiter all providers in the process share.
type Provider interface {
	// Name answers the named place at a point, or the address there. It is nil
	// with no error when the provider knows nothing at that point.
	Name(ctx context.Context, latitudeDegrees, longitudeDegrees float64) (*Found, error)

	// Search finds places matching words near a point, nearest first, within
	// the radius. An empty query asks what is near the point instead of
	// matching words, which is what a check-in list wants.
	Search(ctx context.Context, query string, latitudeDegrees, longitudeDegrees, radiusMeters float64, limitCount int) ([]*Found, error)
}

// NewProvider makes the provider the configuration names, or nil with no error
// when it names none: places are then named after their city, offline. The
// user agent names this server, since the public server's policy asks every
// application to identify itself.
func NewProvider(places config.Places, userAgent string) (Provider, error) {
	if places.ProviderKind == "" {
		return nil, nil
	}
	baseUrl := strings.TrimRight(strings.TrimSpace(places.ProviderURL), "/")
	if baseUrl == "" {
		return nil, fmt.Errorf("place: the %s provider has no address", places.ProviderKind)
	}
	providersRequester := &requester{
		baseUrl:   baseUrl,
		userAgent: userAgent,
		client:    &http.Client{Timeout: requestTimeout},
		limiter:   sharedLimiter,
	}
	switch places.ProviderKind {
	case config.PlacesProviderNominatim:
		return &nominatimProvider{requester: providersRequester}, nil
	case config.PlacesProviderPhoton:
		return &photonProvider{requester: providersRequester}, nil
	}
	return nil, fmt.Errorf("place: %q is not a provider kind this server knows", places.ProviderKind)
}

// The ways asking a provider fails. An error from a Provider wraps one of
// these (or is a ProviderStatusError), and carries the detail for the log,
// which can hold the provider's address and the coordinates asked about. What a
// person is shown, and what is stored on a place, is FailureSentence.
var (
	// ErrProviderBusy is a request that was not made because the turn it
	// would have waited for was more than maximumRequestWait away.
	ErrProviderBusy = errors.New("place: the place provider is busy")

	// ErrProviderUnreachable is a request that got no answer.
	ErrProviderUnreachable = errors.New("place: could not reach the place provider")

	// ErrProviderAnswerUnreadable is an answer that was not what was expected.
	ErrProviderAnswerUnreadable = errors.New("place: the place provider's answer could not be read")
)

// ProviderStatusError is an answer with a status other than 200.
type ProviderStatusError struct {
	StatusCode int
}

func (self *ProviderStatusError) Error() string {
	return fmt.Sprintf("place: the place provider answered %d", self.StatusCode)
}

// FailureSentence is why asking a provider failed, in a sentence that names no
// address and no coordinate, for the person who sees the place and for the
// place's record: "could not reach the place provider", "the place provider
// answered 503", "the place provider's answer could not be read" or "the place
// provider is busy". The detail of the error goes to the log.
func FailureSentence(err error) string {
	var statusError *ProviderStatusError
	switch {
	case errors.Is(err, ErrProviderBusy):
		return "the place provider is busy"
	case errors.As(err, &statusError):
		return fmt.Sprintf("the place provider answered %d", statusError.StatusCode)
	case errors.Is(err, ErrProviderUnreachable):
		return "could not reach the place provider"
	case errors.Is(err, ErrProviderAnswerUnreadable):
		return "the place provider's answer could not be read"
	}
	return "could not ask the place provider"
}

// limiter spaces requests out. Both providers use the one in sharedLimiter, so
// that the limit holds for the process whichever of them is asked and however
// many people are being named at once. The limit is per server instance: each
// instance of a multi-instance deployment has its own limiter, so n instances
// send up to n requests a second to the provider.
type limiter struct {
	mutex         sync.Mutex
	interval      time.Duration
	lastRequestAt time.Time

	// maximumWait is the longest a caller waits for its turn; zero is
	// maximumRequestWait.
	maximumWait time.Duration
}

// sharedLimiter is the process-wide limiter.
var sharedLimiter = &limiter{interval: requestInterval}

// wait returns when the caller may make a request, with ErrProviderBusy at once
// if its turn is more than the longest wait away, or with the context's error
// if the context ends first. Each caller reserves the next free time before
// sleeping, so waiting callers go in turn and none of them is let through early
// by another's cancellation. A canceled caller gives its turn back when it is
// the last one in line; a caller in the middle leaves a gap, which is the safe
// side of a limit.
func (self *limiter) wait(ctx context.Context) error {
	self.mutex.Lock()
	now := time.Now()
	allowedAt := self.lastRequestAt.Add(self.interval)
	if allowedAt.Before(now) {
		allowedAt = now
	}
	maximumWait := self.maximumWait
	if maximumWait == 0 {
		maximumWait = maximumRequestWait
	}
	if allowedAt.Sub(now) > maximumWait {
		self.mutex.Unlock()
		return ErrProviderBusy
	}
	previousRequestAt := self.lastRequestAt
	self.lastRequestAt = allowedAt
	self.mutex.Unlock()

	giveBack := func() {
		self.mutex.Lock()
		defer self.mutex.Unlock()
		if self.lastRequestAt.Equal(allowedAt) {
			self.lastRequestAt = previousRequestAt
		}
	}
	delay := allowedAt.Sub(now)
	if delay <= 0 {
		if err := ctx.Err(); err != nil {
			giveBack()
			return err
		}
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		giveBack()
		return ctx.Err()
	}
}

// requester is what both providers share: the address, the identity the
// provider is told, the limiter and the plumbing of one request.
type requester struct {
	baseUrl   string
	userAgent string
	client    *http.Client
	limiter   *limiter
}

// get asks for a path with parameters and reads the JSON answer into target.
func (self *requester) get(ctx context.Context, path string, parameters url.Values, target any) error {
	if err := self.limiter.wait(ctx); err != nil {
		if errors.Is(err, ErrProviderBusy) {
			return err
		}
		return fmt.Errorf("place: stopped waiting to ask the place provider: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, self.baseUrl+path+"?"+parameters.Encode(), nil)
	if err != nil {
		return fmt.Errorf("%w: the provider's address is not usable: %s", ErrProviderUnreachable, err)
	}
	request.Header.Set("User-Agent", self.userAgent)
	request.Header.Set("Accept", "application/json")
	response, err := self.client.Do(request)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrProviderUnreachable, err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<16))
		return &ProviderStatusError{StatusCode: response.StatusCode}
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, maximumResponseBytes)).Decode(target); err != nil {
		return fmt.Errorf("%w: %s", ErrProviderAnswerUnreadable, err)
	}
	return nil
}

// metersPerDegree is the length of a degree of latitude, and of longitude at
// the equator.
const metersPerDegree = 111320.0

// boundingBox is the square around a point that holds the circle of a radius,
// for a provider that searches inside a box. The east and west edges stop at
// the date line rather than wrapping, and a box at a pole spans every
// longitude.
func boundingBox(latitudeDegrees, longitudeDegrees, radiusMeters float64) (west, north, east, south float64) {
	latitudeChange := radiusMeters / metersPerDegree
	longitudeChange := 180.0
	if cosine := math.Cos(latitudeDegrees * math.Pi / 180); cosine > 1e-6 {
		longitudeChange = math.Min(180, radiusMeters/(metersPerDegree*cosine))
	}
	return math.Max(-180, longitudeDegrees-longitudeChange),
		math.Min(90, latitudeDegrees+latitudeChange),
		math.Min(180, longitudeDegrees+longitudeChange),
		math.Max(-90, latitudeDegrees-latitudeChange)
}

// nearest keeps the results within the radius of the point, nearest first, at
// most limitCount of them, with their distances filled in. A provider answers
// in its own order (by how well the words match) and a box holds more than the
// circle, so the person's radius is applied here, the same way for both.
func nearest(found []*Found, latitudeDegrees, longitudeDegrees, radiusMeters float64, limitCount int) []*Found {
	kept := make([]*Found, 0, len(found))
	for _, candidate := range found {
		candidate.DistanceMeters = DistanceMeters(latitudeDegrees, longitudeDegrees, candidate.LatitudeDegrees, candidate.LongitudeDegrees)
		if candidate.DistanceMeters <= radiusMeters {
			kept = append(kept, candidate)
		}
	}
	sort.SliceStable(kept, func(first, second int) bool { return kept[first].DistanceMeters < kept[second].DistanceMeters })
	if limitCount > 0 && len(kept) > limitCount {
		kept = kept[:limitCount]
	}
	return kept
}

// searchFetchCount is how many results to ask a provider for when the person
// wants limitCount, since the ones nearest are not the ones it lists first.
func searchFetchCount(limitCount int) int {
	return min(max(limitCount, 1)*3, 40)
}
