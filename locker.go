package ally

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	pb "github.com/cjey/ally/proto"
)

var (
	_Locker      lockerBuckets = new(simLockerBuckets)
	_LocalLocker lockerBuckets = new(simLockerBuckets)
)

// RLock has higher priority than Lock
// if in rlocking, new RLock always be acquired, and all Lock must wait until all RLock done
type LockerInterface interface {
	// Lock获得互斥锁
	Lock()
	// RLock获得共享锁
	RLock()
	// TryLock尝试获得互斥锁，返回获取结果，如果提供了timeout，则会尝试等待指定的时间
	TryLock(timeout ...time.Duration) bool
	// TryLock尝试获得共享锁，返回获取结果，如果提供了timeout，则会尝试等待指定的时间
	TryRLock(timeout ...time.Duration) bool

	// Unlock释放互斥锁
	Unlock()
	// RUnlock释放共享锁
	RUnlock()

	// <= 0 means not locked; > 0 means lock acquired; |num| means queues length
	// r=8,w=-3: 8 rlock using, 3 lock waiting
	// r=-8,w=3: 1 lock using, 2 lock waiting, 8 rlock waiting
	// Queues 返回目前互斥锁、共享锁的等待队列情况
	Queues() (r, w int64)
}

// Locker 获得一把具名的锁，相同名称的锁争抢同一份资源，
// 但在本App的所有实例间共享
func Locker(name string) LockerInterface {
	return _Locker.Bucket(name)
}

// LocalLocker 相比于Locker，影响范围仅限定在本实例中
func LocalLocker(name string) LockerInterface {
	return _LocalLocker.Bucket(name)
}

type lockerBuckets interface {
	Bucket(name string) LockerInterface
}

type simLockerBuckets struct {
	mu      sync.RWMutex
	lockers map[string]*simLocker
}

func (slb *simLockerBuckets) Bucket(name string) LockerInterface {
	slb.mu.RLock()
	locker := slb.lockers[name]
	slb.mu.RUnlock()
	if locker == nil {
		slb.mu.Lock()
		if locker = slb.lockers[name]; locker == nil {
			if slb.lockers == nil {
				slb.lockers = make(map[string]*simLocker)
			}
			locker = &simLocker{name: name}
			slb.lockers[name] = locker
		}
		slb.mu.Unlock()
	}
	return locker
}

type allyLockerBuckets struct {
	conns []pb.LockerClient
	seq   int64
}

func newAllyLockerBuckets(addr string) *allyLockerBuckets {
	max := 8
	alb := &allyLockerBuckets{
		conns: make([]pb.LockerClient, max),
		seq:   -1,
	}
	for i := 0; i < max; i++ {
		if conn, err := getGrpcConn(addr); err != nil {
			panic(fmt.Errorf("dial to ally [%s] for locker pool failed, %w", addr, err))
		} else {
			alb.conns[i] = pb.NewLockerClient(conn)
		}
	}
	return alb
}

func (alb *allyLockerBuckets) ally() pb.LockerClient {
	return alb.conns[int(atomic.AddInt64(&alb.seq, 1))%len(alb.conns)]
}

func (alb *allyLockerBuckets) Bucket(name string) LockerInterface {
	return &allyLocker{
		name: name,
		ally: alb.ally,
	}
}

type simLocker struct {
	mu sync.RWMutex

	name string

	cnt  int64
	head *simLockerQueue
	tail *simLockerQueue

	rcnt  int64
	rhead *simLockerQueue
	rtail *simLockerQueue
}

type simLockerQueue struct {
	acquired bool
	wait     chan struct{}
	next     *simLockerQueue
}

func (sm *simLocker) Lock() {
	var me = new(simLockerQueue)

	sm.mu.Lock()
	if sm.head == nil && sm.rhead == nil {
		me.acquired = true
	} else {
		me.wait = make(chan struct{})
	}
	if sm.head == nil {
		sm.head = me
	} else {
		sm.tail.next = me
	}
	sm.tail = me
	sm.cnt++
	sm.mu.Unlock()

	if !me.acquired {
		<-me.wait
	}
}

func (sm *simLocker) RLock() {
	var me = new(simLockerQueue)

	sm.mu.Lock()
	if sm.head == nil || !sm.head.acquired {
		me.acquired = true
	} else {
		me.wait = make(chan struct{})
	}
	if sm.rhead == nil {
		sm.rhead = me
	} else {
		sm.rtail.next = me
	}
	sm.rtail = me
	sm.rcnt++
	sm.mu.Unlock()

	if !me.acquired {
		<-me.wait
	}
}

func (sm *simLocker) TryLock(timeout ...time.Duration) bool {
	var me = new(simLockerQueue)
	sm.mu.Lock()
	if sm.head == nil && sm.rhead == nil {
		me.acquired = true
		sm.head = me
		sm.tail = me
		sm.cnt++
	} else if len(timeout) > 0 && timeout[0] > 0 {
		me.wait = make(chan struct{})
		if sm.head == nil {
			sm.head = me
		} else {
			sm.tail.next = me
		}
		sm.tail = me
		sm.cnt++
	}
	sm.mu.Unlock()

	if me.wait != nil {
		select {
		case <-me.wait:
		case <-time.After(timeout[0]):
			sm.mu.Lock()
			if !me.acquired {
				fake := &simLockerQueue{next: sm.head}
				pre, cur := fake, fake.next
				for cur != nil {
					if cur == me {
						pre.next = me.next
						if pre == fake {
							sm.head = pre.next
						}
						if sm.head == nil {
							sm.tail = nil
						} else if sm.tail == me {
							sm.tail = pre
						}
						sm.cnt--
						break
					}
					pre = cur
					cur = cur.next
				}
			}
			sm.mu.Unlock()
		}
	}
	return me.acquired
}

func (sm *simLocker) TryRLock(timeout ...time.Duration) bool {
	var me = new(simLockerQueue)
	sm.mu.Lock()
	if sm.head == nil || !sm.head.acquired {
		me.acquired = true
		if sm.rhead == nil {
			sm.rhead = me
		} else {
			sm.rtail.next = me
		}
		sm.rtail = me
		sm.rcnt++
	} else if len(timeout) > 0 && timeout[0] > 0 {
		me.wait = make(chan struct{})
		if sm.rhead == nil {
			sm.rhead = me
		} else {
			sm.rtail.next = me
		}
		sm.rtail = me
		sm.rcnt++
	}
	sm.mu.Unlock()
	if me.wait != nil {
		select {
		case <-me.wait:
		case <-time.After(timeout[0]):
			sm.mu.Lock()
			if !me.acquired {
				fake := &simLockerQueue{next: sm.rhead}
				pre, cur := fake, fake.next
				for cur != nil {
					if cur == me {
						pre.next = me.next
						if pre == fake {
							sm.rhead = pre.next
						}
						if sm.rhead == nil {
							sm.tail = nil
						} else if sm.rtail == me {
							sm.rtail = pre
						}
						sm.rcnt--
						break
					}
					pre = cur
					cur = cur.next
				}
			}
			sm.mu.Unlock()
		}
	}
	return me.acquired
}

func (sm *simLocker) Unlock() {
	sm.mu.Lock()
	if sm.head == nil || !sm.head.acquired {
		panic(fmt.Errorf("runtime: invalid ally.Locker.Unlock call, locker[%s]", sm.name))
	}
	sm.head = sm.head.next
	sm.cnt--
	if sm.head == nil {
		sm.tail = nil
	}
	if sm.rhead != nil {
		for cur := sm.rhead; cur != nil; cur = cur.next {
			cur.acquired = true
			close(cur.wait)
		}
	} else if sm.head != nil {
		sm.head.acquired = true
		close(sm.head.wait)
	}
	sm.mu.Unlock()
}

func (sm *simLocker) RUnlock() {
	sm.mu.Lock()
	if sm.rhead == nil || !sm.rhead.acquired {
		panic(fmt.Errorf("runtime: invalid ally.Locker.RUnlock call, locker[%s]", sm.name))
	}
	sm.rhead = sm.rhead.next
	sm.rcnt--
	if sm.rhead == nil {
		sm.rtail = nil
	}
	if sm.rhead == nil && sm.head != nil {
		sm.head.acquired = true
		close(sm.head.wait)
	}
	sm.mu.Unlock()
}

func (sm *simLocker) Queues() (r, w int64) {
	sm.mu.RLock()
	if sm.rhead != nil && sm.rhead.acquired {
		r = sm.rcnt
	} else {
		r = -1 * sm.rcnt
	}
	if sm.head != nil && sm.head.acquired {
		w = sm.cnt
	} else {
		w = -1 * sm.cnt
	}
	sm.mu.RUnlock()
	return
}

type allyLocker struct {
	name string
	ally func() pb.LockerClient
}

func (am *allyLocker) Lock() {
	req := &pb.LockerLockRequest{Name: am.name}
	if _, err := am.ally().LockerLock(context.Background(), req); err != nil {
		panic(runtimeError(err))
	}
}

func (am *allyLocker) RLock() {
	req := &pb.LockerRLockRequest{Name: am.name}
	if _, err := am.ally().LockerRLock(context.Background(), req); err != nil {
		panic(runtimeError(err))
	}
}

func (am *allyLocker) TryLock(timeout ...time.Duration) bool {
	req := &pb.LockerTryLockRequest{Name: am.name}
	if len(timeout) > 0 && timeout[0] > 0 {
		req.Timeout = uint64(timeout[0] / time.Millisecond)
	}
	if rep, err := am.ally().LockerTryLock(context.Background(), req); err != nil {
		panic(runtimeError(err))
	} else {
		return rep.Acquired
	}
}

func (am *allyLocker) TryRLock(timeout ...time.Duration) bool {
	req := &pb.LockerTryRLockRequest{Name: am.name}
	if len(timeout) > 0 && timeout[0] > 0 {
		req.Timeout = uint64(timeout[0] / time.Millisecond)
	}
	if rep, err := am.ally().LockerTryRLock(context.Background(), req); err != nil {
		panic(runtimeError(err))
	} else {
		return rep.Acquired
	}
}

func (am *allyLocker) Unlock() {
	req := &pb.LockerUnlockRequest{Name: am.name}
	if _, err := am.ally().LockerUnlock(context.Background(), req); err != nil {
		panic(runtimeError(err))
	}
}

func (am *allyLocker) RUnlock() {
	req := &pb.LockerRUnlockRequest{Name: am.name}
	if _, err := am.ally().LockerRUnlock(context.Background(), req); err != nil {
		panic(runtimeError(err))
	}
}

func (am *allyLocker) Queues() (r, w int64) {
	req := &pb.LockerQueuesRequest{Name: am.name}
	if rep, err := am.ally().LockerQueues(context.Background(), req); err != nil {
		panic(runtimeError(err))
	} else {
		return rep.R, rep.W
	}
}
