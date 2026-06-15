package server

import "testing"

func TestLRUCacheEviction(t *testing.T) {
	cache := newLRUCache(2)

	cache.Add("feature-a", FeatureFlag{Name: "feature-a"})
	cache.Add("feature-b", FeatureFlag{Name: "feature-b"})

	// Access feature-a to make it most recently used.
	if _, ok := cache.Get("feature-a"); !ok {
		t.Fatal("expected feature-a to be in cache")
	}

	cache.Add("feature-c", FeatureFlag{Name: "feature-c"})

	if _, ok := cache.Get("feature-b"); ok {
		t.Fatal("expected feature-b to be evicted from cache")
	}

	if _, ok := cache.Get("feature-a"); !ok {
		t.Fatal("expected feature-a to remain in cache")
	}

	if _, ok := cache.Get("feature-c"); !ok {
		t.Fatal("expected feature-c to be in cache")
	}
}

func TestLRUCacheRemove(t *testing.T) {
	cache := newLRUCache(2)

	cache.Add("feature-x", FeatureFlag{Name: "feature-x"})
	cache.Add("feature-y", FeatureFlag{Name: "feature-y"})

	cache.Remove("feature-x")

	if _, ok := cache.Get("feature-x"); ok {
		t.Fatal("expected feature-x to be removed from cache")
	}

	if _, ok := cache.Get("feature-y"); !ok {
		t.Fatal("expected feature-y to remain in cache")
	}
}

func TestLRUCacheUpdateMovesToFront(t *testing.T) {
	cache := newLRUCache(2)

	cache.Add("feature-a", FeatureFlag{Name: "feature-a"})
	cache.Add("feature-b", FeatureFlag{Name: "feature-b"})
	cache.Add("feature-a", FeatureFlag{Name: "feature-a-updated"})
	cache.Add("feature-c", FeatureFlag{Name: "feature-c"})

	if _, ok := cache.Get("feature-b"); ok {
		t.Fatal("expected feature-b to be evicted after feature-a was refreshed")
	}

	flag, ok := cache.Get("feature-a")
	if !ok {
		t.Fatal("expected feature-a to remain in cache")
	}
	if flag.Name != "feature-a-updated" {
		t.Fatalf("expected feature-a to be updated in cache, got %q", flag.Name)
	}
}
