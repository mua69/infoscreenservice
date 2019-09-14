package main

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

type ImageCacheEntry struct {
	timestamp time.Time
	image []byte
}

type ImageCache struct {
	Cache map[string]*ImageCacheEntry
	CacheSize int64
	CacheSizeLimit int64
	Mutex sync.RWMutex
}

const MB = 1024*1024
var Cache ImageCache


func InitImageCache() {
	Cache.Cache = make(map[string]*ImageCacheEntry)
	Cache.CacheSize = 0
	Cache.CacheSizeLimit = int64(g_config.CacheSize*MB)

	Info(0, "Cache size: %d MB", g_config.CacheSize)
}

func BuildCacheKey(name string, width, height uint) string {
	return fmt.Sprintf("%s/%d/%d", name, width, height)
}

func GetImageFromCache(name string, width, height uint) []byte {
	Cache.Mutex.RLock()
	ent := Cache.Cache[BuildCacheKey(name, width, height)]
	Cache.Mutex.RUnlock()

	if ent != nil {
		Info(0, "Cache hit for: %s %d %d", name, width,height)
		return ent.image
	}

	return nil
}

func addImageToCache(name string, width, height uint, image []byte) {

	Cache.Mutex.Lock()

	key := BuildCacheKey(name, width, height)

	ent := Cache.Cache[key]

	if ent != nil {
		Cache.CacheSize -= int64(len(ent.image))
	} else {
		ent = new(ImageCacheEntry)
		Cache.Cache[key] = ent
	}

	ent.timestamp = time.Now()
	ent.image = image

	Cache.CacheSize += int64(len(image))

	Info(0, "added image '%s' %d %d to cache, cache size %.3f MB", name, width, height, float32(Cache.CacheSize)/MB)

	if Cache.CacheSize > Cache.CacheSizeLimit {
		cleanCache()
	}

	Cache.Mutex.Unlock()
}

func cleanCache() {
	type SortEntry struct {
		key string
		ent *ImageCacheEntry
	}

	n := len(Cache.Cache)
	cacheEntries := make([]SortEntry, n)

	i := 0

	for key, ent := range Cache.Cache {
		cacheEntries[i].key = key
		cacheEntries[i].ent = ent
		i++
	}

	sort.Slice(cacheEntries, func(a, b int) bool { return cacheEntries[b].ent.timestamp.After(cacheEntries[a].ent.timestamp)})

	/*
	for i = 0; i <n; i++ {
		Info(0, "Sorted Cache Entry: %s: %s", cacheEntries[i].ent.timestamp.Format(time.RFC3339), cacheEntries[i].key)
	}
*/

	for i = 0; i < n && Cache.CacheSize > Cache.CacheSizeLimit; i++ {
		Cache.CacheSize -= int64(len(cacheEntries[i].ent.image))
		if Cache.CacheSize < 0 {
			Info(0, "cleanCache: cache size < 0")
			Cache.CacheSize = 0
		}
		Info(0, "cleanCache: removing cache entry: %s", cacheEntries[i].key)
		delete(Cache.Cache, cacheEntries[i].key)
	}

	Info(0, "cleanCache: final cache size %.3f MB", float32(Cache.CacheSize)/MB)
}



