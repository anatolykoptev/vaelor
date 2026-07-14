package forge

import (
	"context"
	"encoding/json"
	"time"

	kitcache "github.com/anatolykoptev/go-kit/cache"
)

// cacheGetOrLoadJSONWithTTL retrieves a typed value from c, loading it via
// loader on cache miss and storing the marshaled JSON with the given TTL.
// If c is nil, the loader is called directly and caching is skipped.
func cacheGetOrLoadJSONWithTTL[T any](
	c *kitcache.Cache,
	ctx context.Context,
	key string,
	ttl time.Duration,
	loader func(context.Context) (T, error),
) (T, error) {
	var zero T
	if c == nil {
		return loader(ctx)
	}

	data, err := c.GetOrLoadWithTTL(ctx, key, ttl, func(ctx context.Context) ([]byte, error) {
		val, err := loader(ctx)
		if err != nil {
			return nil, err
		}
		return json.Marshal(val)
	})
	if err != nil {
		return zero, err
	}

	var val T
	if err := json.Unmarshal(data, &val); err != nil {
		return zero, err
	}
	return val, nil
}
