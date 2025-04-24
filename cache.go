package ally

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/patrickmn/go-cache"
	"google.golang.org/protobuf/proto"

	pb "github.com/cjey/ally/proto"
)

var (
	_Cache      cacheBuckets = new(simCacheBuckets)
	_LocalCache cacheBuckets = new(simCacheBuckets)
)

// CacheInterface 提供了多实例共享内存kv缓存的功能
type CacheInterface interface {
	Set(key string, val []byte, ttl ...time.Duration)
	SetString(key string, val string, ttl ...time.Duration)
	SetJson(key string, raw any, ttl ...time.Duration) error
	SetMessage(key string, raw proto.Message, ttl ...time.Duration) error

	Get(key string) (val []byte, found bool)
	GetString(key string) (val string, found bool)
	GetJson(key string, raw any) (found bool, err error)
	GetMessage(key string, raw proto.Message) (found bool, err error)

	Del(key string)
	Keys() (keys []string)
	Count() (count uint64)
	Items() (items map[string][]byte)
	Flush()
	Exists(key string) (found bool)
}

// Cache 返回给定命名空间下的一个Cache Bucket，返回的对象对数据的操作都仅限于当前的命名空间内，
// 但在本App的全部实例间共享
func Cache(ns string) CacheInterface {
	return _Cache.Bucket(ns)
}

// LocalCache 相比于Cache，影响范围仅限定在本实例中
func LocalCache(ns string) CacheInterface {
	return _LocalCache.Bucket(ns)
}

type cacheBuckets interface {
	Bucket(ns string) CacheInterface
}

type simCacheBuckets struct {
	mu      sync.RWMutex
	buckets map[string]*cache.Cache
}

func (scb *simCacheBuckets) Bucket(ns string) CacheInterface {
	scb.mu.RLock()
	bucket := scb.buckets[ns]
	scb.mu.RUnlock()
	if bucket == nil {
		scb.mu.Lock()
		if bucket = scb.buckets[ns]; bucket == nil {
			if scb.buckets == nil {
				scb.buckets = make(map[string]*cache.Cache)
			}
			bucket = cache.New(cache.NoExpiration, 60*time.Second)
			scb.buckets[ns] = bucket
		}
		scb.mu.Unlock()
	}
	return &simCache{ns: ns, bucket: bucket}
}

type allyCacheBuckets struct {
	conns []pb.CacheClient
	seq   int64
}

func newAllyCacheBuckets(addr string) *allyCacheBuckets {
	max := 2
	acb := &allyCacheBuckets{
		conns: make([]pb.CacheClient, max),
		seq:   -1,
	}
	for i := 0; i < max; i++ {
		if conn, err := getGrpcConn(addr); err != nil {
			panic(fmt.Errorf("dial to ally [%s] for cache pool failed, %w", addr, err))
		} else {
			acb.conns[i] = pb.NewCacheClient(conn)
		}
	}
	return acb
}

func (acb *allyCacheBuckets) ally() pb.CacheClient {
	return acb.conns[int(atomic.AddInt64(&acb.seq, 1))%len(acb.conns)]
}

func (acb *allyCacheBuckets) Bucket(ns string) CacheInterface {
	return &allyCache{
		ns:   ns,
		ally: acb.ally,
	}
}

type simCache struct {
	ns     string
	bucket *cache.Cache
}

func (sc *simCache) Set(key string, val []byte, ttl ...time.Duration) {
	if len(ttl) == 0 {
		sc.bucket.Set(key, val, 0)
	} else {
		sc.bucket.Set(key, val, ttl[0])
	}
}
func (sc *simCache) SetString(key string, val string, ttl ...time.Duration) {
	sc.Set(key, []byte(val), ttl...)
}
func (sc *simCache) SetJson(key string, raw any, ttl ...time.Duration) error {
	if val, err := json.Marshal(raw); err != nil {
		return err
	} else {
		sc.Set(key, val, ttl...)
		return nil
	}
}
func (sc *simCache) SetMessage(key string, raw proto.Message, ttl ...time.Duration) error {
	if val, err := proto.Marshal(raw); err != nil {
		return err
	} else {
		sc.Set(key, val, ttl...)
		return nil
	}
}

func (sc *simCache) Get(key string) (val []byte, found bool) {
	if v, ok := sc.bucket.Get(key); ok {
		return v.([]byte), true
	}
	return nil, false
}
func (sc *simCache) GetString(key string) (val string, found bool) {
	if v, ok := sc.Get(key); ok {
		return string(v), true
	}
	return "", false
}
func (sc *simCache) GetJson(key string, raw any) (found bool, err error) {
	if val, found := sc.Get(key); !found {
		return false, nil
	} else {
		return true, json.Unmarshal(val, raw)
	}
}
func (sc *simCache) GetMessage(key string, raw proto.Message) (found bool, err error) {
	if val, found := sc.Get(key); !found {
		return false, nil
	} else {
		return true, proto.Unmarshal(val, raw)
	}
}

func (sc *simCache) Del(key string) {
	sc.bucket.Delete(key)
}

func (sc *simCache) Keys() (keys []string) {
	items := sc.bucket.Items()
	keys = make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	return keys
}

func (sc *simCache) Count() (count uint64) {
	return uint64(sc.bucket.ItemCount())
}

func (sc *simCache) Items() (items map[string][]byte) {
	_items := sc.bucket.Items()
	items = make(map[string][]byte, len(_items))
	for key, _item := range _items {
		items[key] = _item.Object.([]byte)
	}
	return items
}

func (sc *simCache) Exists(key string) (found bool) {
	if _, ok := sc.bucket.Get(key); ok {
		return true
	}
	return false
}

func (sc *simCache) Flush() {
	sc.bucket.Flush()
}

type allyCache struct {
	ns   string
	ally func() pb.CacheClient
}

func (ac *allyCache) Set(key string, val []byte, ttl ...time.Duration) {
	req := &pb.CacheSetRequest{Ns: ac.ns, Key: key, Val: val}
	if len(ttl) > 0 && ttl[0] > 0 {
		req.Ttl = uint64(ttl[0].Truncate(time.Millisecond))
	}
	if _, err := ac.ally().CacheSet(context.Background(), req); err != nil {
		panic(runtimeError(err))
	}
}
func (ac *allyCache) SetString(key string, val string, ttl ...time.Duration) {
	ac.Set(key, []byte(val), ttl...)
}
func (ac *allyCache) SetJson(key string, raw any, ttl ...time.Duration) error {
	if val, err := json.Marshal(raw); err != nil {
		return err
	} else {
		ac.Set(key, val, ttl...)
		return nil
	}
}
func (ac *allyCache) SetMessage(key string, raw proto.Message, ttl ...time.Duration) error {
	if val, err := proto.Marshal(raw); err != nil {
		return err
	} else {
		ac.Set(key, val, ttl...)
		return nil
	}
}

func (ac *allyCache) Get(key string) (val []byte, found bool) {
	req := &pb.CacheGetRequest{Ns: ac.ns, Key: key}
	if rep, err := ac.ally().CacheGet(context.Background(), req); err != nil {
		panic(runtimeError(err))
	} else {
		return rep.Val, rep.Found
	}
}
func (ac *allyCache) GetString(key string) (val string, found bool) {
	if v, ok := ac.Get(key); ok {
		return string(v), true
	}
	return "", false
}
func (ac *allyCache) GetJson(key string, raw any) (found bool, err error) {
	if val, found := ac.Get(key); !found {
		return false, nil
	} else {
		return true, json.Unmarshal(val, raw)
	}
}
func (ac *allyCache) GetMessage(key string, raw proto.Message) (found bool, err error) {
	if val, found := ac.Get(key); !found {
		return false, nil
	} else {
		return true, proto.Unmarshal(val, raw)
	}
}

func (ac *allyCache) Del(key string) {
	req := &pb.CacheDelRequest{Ns: ac.ns, Key: key}
	if _, err := ac.ally().CacheDel(context.Background(), req); err != nil {
		panic(runtimeError(err))
	}
}

func (ac *allyCache) Keys() (keys []string) {
	req := &pb.CacheKeysRequest{Ns: ac.ns}
	if rep, err := ac.ally().CacheKeys(context.Background(), req); err != nil {
		panic(runtimeError(err))
	} else {
		return rep.Keys
	}
}

func (ac *allyCache) Count() (count uint64) {
	req := &pb.CacheCountRequest{Ns: ac.ns}
	if rep, err := ac.ally().CacheCount(context.Background(), req); err != nil {
		panic(runtimeError(err))
	} else {
		return rep.Count
	}
}

func (ac *allyCache) Items() (items map[string][]byte) {
	req := &pb.CacheItemsRequest{Ns: ac.ns}
	if rep, err := ac.ally().CacheItems(context.Background(), req); err != nil {
		panic(runtimeError(err))
	} else {
		return rep.Items
	}
}

func (ac *allyCache) Flush() {
	req := &pb.CacheFlushRequest{Ns: ac.ns}
	if _, err := ac.ally().CacheFlush(context.Background(), req); err != nil {
		panic(runtimeError(err))
	}
}

func (ac *allyCache) Exists(key string) (found bool) {
	req := &pb.CacheExistsRequest{Ns: ac.ns, Key: key}
	if rep, err := ac.ally().CacheExists(context.Background(), req); err != nil {
		panic(runtimeError(err))
	} else {
		return rep.Found
	}
}
