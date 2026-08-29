package ceremony

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/redis/go-redis/v9"

	"github.com/ziyan/teanode/internal/util/security"
)

// NewRedisStore parks ceremonies where every instance can reach them.
//
// For a deployment behind a load balancer: WebAuthn is two requests, and the
// browser has no reason to come back to the instance it started with. The
// in-process store would then fail every other sign-in, which looks like a
// broken passkey rather than a missing Redis.
//
// A key with a TTL is exactly the shape of the thing being stored, and GETDEL
// is "return it and delete it" as one step — the single-use rule enforced by
// the store rather than by every caller remembering to delete.
func NewRedisStore(client redis.UniversalClient) Store {
	return &redisStore{client: client}
}

type redisStore struct {
	client redis.UniversalClient
}

func key(ceremonyId string) string { return "teanode:ceremony:" + ceremonyId }

func (self *redisStore) Park(ctx context.Context, ceremony *Ceremony) (string, error) {
	encoded, err := json.Marshal(ceremony)
	if err != nil {
		return "", err
	}
	ceremonyId := security.NewULID()
	if err := self.client.Set(ctx, key(ceremonyId), encoded, Lifetime).Err(); err != nil {
		return "", fmt.Errorf("ceremony: failed to park: %w", err)
	}
	return ceremonyId, nil
}

func (self *redisStore) Take(ctx context.Context, ceremonyId string) (*Ceremony, error) {
	if ceremonyId == "" {
		return nil, ErrNoCeremonyInProgress
	}
	raw, err := self.client.GetDel(ctx, key(ceremonyId)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrNoCeremonyInProgress
	}
	if err != nil {
		return nil, fmt.Errorf("ceremony: failed to take: %w", err)
	}
	var parked Ceremony
	if err := json.Unmarshal(raw, &parked); err != nil {
		return nil, err
	}
	return &parked, nil
}
