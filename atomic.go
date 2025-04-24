package ally

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	pb "github.com/cjey/ally/proto"
)

var (
	_Atomic      atomicBuckets = new(simAtomicBuckets)
	_LocalAtomic atomicBuckets = new(simAtomicBuckets)
)

// AtomicInterface 提供了一个简单的原子操作数据的功能
type AtomicInterface interface {
	Add(delta int64) (new int64)
	CompareAndSwap(old, new int64) (swapped bool)
	Load() (val int64)
	Store(val int64)
	Swap(new int64) (old int64)
}

// Atomic 返回指定名称的操作对象，返回的对象对数据的操作仅限于此名称空间，
// 但在App全部实例间共享
func Atomic(name string) AtomicInterface {
	return _Atomic.Bucket(name)
}

// LocalAtomic 相比于Atomic，影响范围限定在本实例中
func LocalAtomic(name string) AtomicInterface {
	return _LocalAtomic.Bucket(name)
}

type atomicBuckets interface {
	Bucket(name string) AtomicInterface
}

type simAtomicBuckets struct {
	mu      sync.RWMutex
	buckets map[string]*int64
}

func (sab *simAtomicBuckets) Bucket(name string) AtomicInterface {
	sab.mu.RLock()
	bucket := sab.buckets[name]
	sab.mu.RUnlock()
	if bucket == nil {
		sab.mu.Lock()
		if bucket = sab.buckets[name]; bucket == nil {
			if sab.buckets == nil {
				sab.buckets = make(map[string]*int64)
			}
			var n int64
			bucket = &n
			sab.buckets[name] = bucket
		}
		sab.mu.Unlock()
	}
	return &simAtomic{name: name, bucket: bucket}
}

type allyAtomicBuckets struct {
	conns []pb.AtomicClient
	seq   int64
}

func newAllyAtomicBuckets(addr string) *allyAtomicBuckets {
	max := 8
	aab := &allyAtomicBuckets{
		conns: make([]pb.AtomicClient, max),
		seq:   -1,
	}
	for i := 0; i < max; i++ {
		if conn, err := getGrpcConn(addr); err != nil {
			panic(fmt.Errorf("dial to ally [%s] for atomic pool failed, %w", addr, err))
		} else {
			aab.conns[i] = pb.NewAtomicClient(conn)
		}
	}
	return aab
}

func (aab *allyAtomicBuckets) ally() pb.AtomicClient {
	return aab.conns[int(atomic.AddInt64(&aab.seq, 1))%len(aab.conns)]
}

func (aab *allyAtomicBuckets) Bucket(name string) AtomicInterface {
	return &allyAtomic{
		name: name,
		ally: aab.ally,
	}
}

type simAtomic struct {
	name   string
	bucket *int64
}

func (sa *simAtomic) Add(delta int64) (new int64) {
	return atomic.AddInt64(sa.bucket, delta)
}

func (sa *simAtomic) CompareAndSwap(old, new int64) (swapped bool) {
	return atomic.CompareAndSwapInt64(sa.bucket, old, new)
}

func (sa *simAtomic) Load() (val int64) {
	return atomic.LoadInt64(sa.bucket)
}

func (sa *simAtomic) Store(val int64) {
	atomic.StoreInt64(sa.bucket, val)
}

func (sa *simAtomic) Swap(new int64) (old int64) {
	return atomic.SwapInt64(sa.bucket, new)
}

type allyAtomic struct {
	ally func() pb.AtomicClient
	name string
}

func (aa *allyAtomic) Add(delta int64) (new int64) {
	req := &pb.AtomicAddRequest{Name: aa.name, Delta: delta}
	if rep, err := aa.ally().AtomicAdd(context.Background(), req); err != nil {
		panic(runtimeError(err))
	} else {
		return rep.Val
	}
}

func (aa *allyAtomic) Load() (val int64) {
	req := &pb.AtomicLoadRequest{Name: aa.name}
	if rep, err := aa.ally().AtomicLoad(context.Background(), req); err != nil {
		panic(runtimeError(err))
	} else {
		return rep.Val
	}
}

func (aa *allyAtomic) Store(val int64) {
	req := &pb.AtomicStoreRequest{Name: aa.name, Val: val}
	if _, err := aa.ally().AtomicStore(context.Background(), req); err != nil {
		panic(runtimeError(err))
	}
}

func (aa *allyAtomic) Swap(new int64) (old int64) {
	req := &pb.AtomicSwapRequest{Name: aa.name, New: new}
	if rep, err := aa.ally().AtomicSwap(context.Background(), req); err != nil {
		panic(runtimeError(err))
	} else {
		return rep.Old
	}
}

func (aa *allyAtomic) CompareAndSwap(old, new int64) (swapped bool) {
	req := &pb.AtomicCompareAndSwapRequest{Name: aa.name, Old: old, New: new}
	if rep, err := aa.ally().AtomicCompareAndSwap(context.Background(), req); err != nil {
		panic(runtimeError(err))
	} else {
		return rep.Swapped
	}
}
