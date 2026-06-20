// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package zone

// CacheNameLen is the per-entry tag-name length. tyrquake:
// CACHE_NAMELEN.
const CacheNameLen = 32

// CacheUser is the handle through which the renderer + model loader
// reference a cached resource. Lifetime: the User outlives the cache
// entry; Data is nil after a Flush or LRU eviction. tyrquake:
// cache_user_t.
type CacheUser struct {
	Destructor func(*CacheUser) // optional; called on Free
	Data       []byte           // nil when not currently resident
	Pad        int              // extra bytes before Data for container_of()
}

// cacheSystem mirrors tyrquake's cache_system_t. It is the in-arena
// per-entry header that links into both the spatial (prev/next) and
// LRU (lruPrev/lruNext) doubly-linked lists. Stored here for layout
// parity with upstream; the full LRU logic is not yet ported (see
// package doc).
type cacheSystem struct {
	size             int32
	user             *CacheUser
	name             [CacheNameLen]byte
	prev, next       *cacheSystem
	lruPrev, lruNext *cacheSystem
}

// Cache is the LRU cache layered on top of a Hunk. The full
// implementation is deferred until its first consumer (the model
// loader) lands; see the package doc for the rationale. The struct
// shape is preserved so renderer code can declare *Cache fields and
// CacheUser variables today.
type Cache struct {
	hunk *Hunk
	head cacheSystem // sentinel cap for both linked lists
}

// NewCache constructs a Cache layered on h. tyrquake: Cache_Init
// (struct shape only; methods still panic).
func NewCache(h *Hunk) *Cache {
	c := &Cache{hunk: h}
	c.head.prev = &c.head
	c.head.next = &c.head
	c.head.lruPrev = &c.head
	c.head.lruNext = &c.head
	return c
}

// Alloc reserves size bytes of cache memory for u, tagged with name.
// tyrquake: Cache_Alloc. NOT YET PORTED.
func (c *Cache) Alloc(u *CacheUser, size int, name string) []byte {
	panic("zone: cache not yet ported (Cache.Alloc)")
}

// Check returns u.Data if u is still resident, else nil. Touches the
// LRU. tyrquake: Cache_Check. NOT YET PORTED.
func (c *Cache) Check(u *CacheUser) []byte {
	panic("zone: cache not yet ported (Cache.Check)")
}

// Free evicts u from the cache, running its destructor if any.
// tyrquake: Cache_Free. NOT YET PORTED.
func (c *Cache) Free(u *CacheUser) {
	panic("zone: cache not yet ported (Cache.Free)")
}

// Flush evicts every cache entry. tyrquake: Cache_Flush. NOT YET
// PORTED.
func (c *Cache) Flush() {
	panic("zone: cache not yet ported (Cache.Flush)")
}
