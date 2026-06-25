package analyzer

import (
	"fmt"
	"hash/maphash"
	"os"
	"runtime"
	"sync"
)

var hashSeed = maphash.MakeSeed()

type entry[K comparable, V any] struct {
	key        K
	val        V
	prev, next *entry[K, V] // интрузивные указатели LRU-списка
}

type shard[K comparable, V any] struct {
	mu          sync.Mutex
	index       map[K]*entry[K, V]
	head, tail  *entry[K, V] // head = MRU, tail = LRU; nil-границы
	cap, length uint32
}

type ShardedCache[K comparable, V any] struct {
	shards []*shard[K, V]
	hashFn func(K) uint64
	mask   uint64
}

func NewShardedCache[K comparable, V any](totalCap uint32, numShards uint32, hashFn func(K) uint64) *ShardedCache[K, V] {
	if totalCap == 0 {
		shards := make([]*shard[K, V], 1)
		shards[0] = &shard[K, V]{
			cap:   0,
			index: make(map[K]*entry[K, V], 1),
		}
		return &ShardedCache[K, V]{shards: shards, hashFn: hashFn, mask: 0}
	}
	if numShards > uint32(runtime.NumCPU()) {
		_, _ = fmt.Fprintf(os.Stderr, "Warning: numShards bigger than avaliable threads\n")
	}
	if numShards == 0 || numShards&(numShards-1) != 0 {
		panic("numShards must be a power of two")
	}
	if numShards > totalCap {
		panic("numShards must be less or equal to totalCap")
	}
	perShardCap := totalCap / numShards
	shards := make([]*shard[K, V], numShards)
	for i := range shards {
		shards[i] = &shard[K, V]{
			cap:   perShardCap,
			index: make(map[K]*entry[K, V], perShardCap),
		}
	}
	return &ShardedCache[K, V]{shards: shards, hashFn: hashFn, mask: uint64(numShards - 1)}
}

func (c *ShardedCache[K, V]) shardFor(key K) *shard[K, V] {
	return c.shards[c.hashFn(key)&c.mask]
}

func (c *ShardedCache[K, V]) Get(key K) (V, bool) {
	s := c.shardFor(key)
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.index[key]
	if !ok {
		var zero V
		return zero, false
	}
	s.moveToFront(e)
	return e.val, true
}

func (c *ShardedCache[K, V]) Put(key K, val V) {
	s := c.shardFor(key)
	s.mu.Lock()
	defer s.mu.Unlock()
	//no-cap guard
	if s.cap == 0 {
		return
	}
	if e, ok := s.index[key]; ok {
		e.val = val
		s.moveToFront(e)
		return
	}

	var e *entry[K, V]
	if s.length == s.cap {
		// кэш заполнен: переиспользуем LRU-узел (tail) вместо аллокации
		e = s.tail
		delete(s.index, e.key)
		s.unlink(e)
	} else {
		e = &entry[K, V]{}
		s.length++
	}
	e.key, e.val = key, val
	s.pushFront(e)
	s.index[key] = e
}

// --- интрузивный двусвязный список; все методы вызываются под s.mu ---

func (s *shard[K, V]) pushFront(e *entry[K, V]) {
	e.prev = nil
	e.next = s.head
	if s.head != nil {
		s.head.prev = e
	} else {
		s.tail = e // список был пуст
	}
	s.head = e
}

func (s *shard[K, V]) unlink(e *entry[K, V]) {
	if e.prev != nil {
		e.prev.next = e.next
	} else {
		s.head = e.next // e был головой
	}
	if e.next != nil {
		e.next.prev = e.prev
	} else {
		s.tail = e.prev // e был хвостом
	}
	e.prev, e.next = nil, nil
}

func (s *shard[K, V]) moveToFront(e *entry[K, V]) {
	if s.head == e {
		return
	}
	s.unlink(e)
	s.pushFront(e)
}

func HashString(s string) uint64 {
	return maphash.String(hashSeed, s)
}
