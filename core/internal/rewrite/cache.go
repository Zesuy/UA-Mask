package rewrite

import lru "github.com/hashicorp/golang-lru/v2"

type UACache struct {
	cache *lru.Cache[string, string]
}

func NewUACache(size int) (*UACache, error) {
	if size == 0 {
		return &UACache{}, nil
	}

	cache, err := lru.New[string, string](size)
	if err != nil {
		return nil, err
	}

	return &UACache{cache: cache}, nil
}

func (c *UACache) Get(originUA string) (string, bool) {
	if c == nil || c.cache == nil {
		return "", false
	}
	return c.cache.Get(originUA)
}

func (c *UACache) Add(originUA, finalUA string) {
	if c == nil || c.cache == nil {
		return
	}
	c.cache.Add(originUA, finalUA)
}
