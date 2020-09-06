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
	Mutex sync.Mutex
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
	Cache.Mutex.Lock()
	ent := Cache.Cache[BuildCacheKey(name, width, height)]
	defer Cache.Mutex.Unlock()

	if ent != nil {
		Info(1, "Cache hit for: %s %d %d", name, width,height)
		ent.timestamp = time.Now()
		return ent.image
	}

	return nil
}

func addImageToCache(name string, width, height uint, image []byte) {

	Cache.Mutex.Lock()

	key := BuildCacheKey(name, width, height)

	ent := Cache.Cache[key]

	n:= len(image)
	imgCopy := make([]byte, n)

	copy(imgCopy, image)

	if ent != nil {
		Cache.CacheSize -= int64(len(ent.image))
	} else {
		ent = new(ImageCacheEntry)
		Cache.Cache[key] = ent
	}

	ent.timestamp = time.Now()
	ent.image = imgCopy

	Cache.CacheSize += int64(n)

	Info(1, "added image '%s' %d %d to cache, cache size %.3f MB", name, width, height, float32(Cache.CacheSize)/MB)

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
			Info(1, "cleanCache: cache size < 0")
			Cache.CacheSize = 0
		}
		Info(1, "cleanCache: removing cache entry: %s", cacheEntries[i].key)
		delete(Cache.Cache, cacheEntries[i].key)
	}

	Info(1, "cleanCache: final cache size %.3f MB", float32(Cache.CacheSize)/MB)
}



