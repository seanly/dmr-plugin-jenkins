package jenkins

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// cachedJenkinsClient wraps a JenkinsClient with caching capability
type cachedJenkinsClient struct {
	inner JenkinsClient
	cache *inventoryCache
}

// cacheEntry holds cached data with timestamp
type cacheEntry struct {
	data      []byte
	timestamp time.Time
}

// inventoryCache provides thread-safe caching for inventory data
type inventoryCache struct {
	mu   sync.RWMutex
	data map[string]cacheEntry
	ttl  time.Duration
}

func newInventoryCache(ttl time.Duration) *inventoryCache {
	return &inventoryCache{
		data: make(map[string]cacheEntry),
		ttl:  ttl,
	}
}

func (c *inventoryCache) get(key string) ([]byte, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.data[key]
	if !ok || time.Since(entry.timestamp) > c.ttl {
		return nil, false
	}
	return entry.data, true
}

func (c *inventoryCache) set(key string, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[key] = cacheEntry{
		data:      data,
		timestamp: time.Now(),
	}
}

func (c *inventoryCache) makeCacheKey(parts ...string) string {
	return strings.Join(parts, ":")
}

// NewCachedClient wraps a JenkinsClient with caching
func NewCachedClient(inner JenkinsClient, ttl time.Duration) JenkinsClient {
	return &cachedJenkinsClient{
		inner: inner,
		cache: newInventoryCache(ttl),
	}
}

func (c *cachedJenkinsClient) GetJob(ctx context.Context, job string, tree string) ([]byte, error) {
	key := c.cache.makeCacheKey("GetJob", job, tree)

	if data, ok := c.cache.get(key); ok {
		return data, nil
	}

	data, err := c.inner.GetJob(ctx, job, tree)
	if err != nil {
		return nil, err
	}

	c.cache.set(key, data)
	return data, nil
}

func (c *cachedJenkinsClient) ListBuilds(ctx context.Context, job string, limit int) ([]byte, error) {
	// Don't cache ListBuilds as it changes frequently
	return c.inner.ListBuilds(ctx, job, limit)
}

func (c *cachedJenkinsClient) GetBuild(ctx context.Context, job string, buildNumber int) ([]byte, error) {
	// Don't cache GetBuild as it changes frequently
	return c.inner.GetBuild(ctx, job, buildNumber)
}

func (c *cachedJenkinsClient) TriggerBuild(ctx context.Context, job string, params map[string]string) error {
	// Clear job cache after trigger
	defer c.cache.clear()
	return c.inner.TriggerBuild(ctx, job, params)
}

func (c *cachedJenkinsClient) GetConsoleText(ctx context.Context, job string, buildNumber int) (string, error) {
	// Don't cache console text
	return c.inner.GetConsoleText(ctx, job, buildNumber)
}

func (c *inventoryCache) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data = make(map[string]cacheEntry)
}

func (c *cachedJenkinsClient) GetComputers(ctx context.Context) ([]byte, error) {
	key := c.cache.makeCacheKey("GetComputers")

	if data, ok := c.cache.get(key); ok {
		return data, nil
	}

	data, err := c.inner.GetComputers(ctx)
	if err != nil {
		return nil, err
	}

	c.cache.set(key, data)
	return data, nil
}

func (c *cachedJenkinsClient) GetQueue(ctx context.Context) ([]byte, error) {
	key := c.cache.makeCacheKey("GetQueue")

	if data, ok := c.cache.get(key); ok {
		return data, nil
	}

	data, err := c.inner.GetQueue(ctx)
	if err != nil {
		return nil, err
	}

	c.cache.set(key, data)
	return data, nil
}

func (c *cachedJenkinsClient) SearchSuggest(ctx context.Context, folder string, query string) ([]byte, error) {
	return c.inner.SearchSuggest(ctx, folder, query)
}

func (c *cachedJenkinsClient) Close() error {
	return c.inner.Close()
}

// Ensure cachedJenkinsClient implements JenkinsClient
var _ JenkinsClient = (*cachedJenkinsClient)(nil)

// Helper for formatting cache key with integers
func formatCacheKey(parts ...interface{}) string {
	var strs []string
	for _, p := range parts {
		strs = append(strs, fmt.Sprintf("%v", p))
	}
	return strings.Join(strs, ":")
}
