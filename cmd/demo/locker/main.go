package main

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cjey/ally"
)

func main() {
	main1()
	//main2()
}

func main1() {
	var cnt int64
	var last time.Time
	ally.ExitHook = func() {
		if time.Since(last) > 200*time.Millisecond {
			cnt = 1
		} else {
			cnt++
		}
		last = time.Now()
		if cnt == 3 {
			os.Exit(1)
		}
	}
	ally.Ready()

	//RW()
	//Try()
	Try2()

	//Concurrent2()
	//Bench()
	//Bench2()

	done := ally.NewEvent()
	<-done.Yes
}

func main2() {
	done := ally.NewEvent()
	ally.ExitHook = func() {
		defer done.Emit()
		os.Exit(1)
	}
	ally.Ready()

	//Simple()
	//Timeout()
	//Dirty()
	Concurrent()

	//for {
	//	runtime.GC()
	//	time.Sleep(time.Second)
	//}

	<-done.Yes
}

func RW() {
	mu := ally.Locker("test")
	if ally.ID%4 == 0 {
		fmt.Printf("%d getting lock\n", ally.ID)
		mu.Lock()
		fmt.Printf("%d gain lock\n", ally.ID)
	} else {
		fmt.Printf("%d getting rlock\n", ally.ID)
		mu.RLock()
		fmt.Printf("%d gain rlock\n", ally.ID)
		//time.Sleep(10 * time.Second)
		//runtime.GC()
	}
}

func Try() {
	mu := ally.Locker("test")
	mu.Lock()
	{
		r, w := mu.Queues()
		fmt.Printf("rq %d, wq %d\n", r, w)
	}
	{
		w := mu.TryLock()
		fmt.Printf("trylock %t\n", w)
	}
	mu.Unlock()
	{
		w := mu.TryLock()
		fmt.Printf("trylock %t\n", w)
		mu.Unlock()
	}
	{
		w := mu.TryRLock()
		fmt.Printf("tryrlock %t\n", w)
	}
	{
		w := mu.TryRLock()
		fmt.Printf("tryrlock %t\n", w)
	}
	{
		w := mu.TryRLock()
		fmt.Printf("tryrlock %t\n", w)
	}
	{
		w := mu.TryRLock()
		fmt.Printf("tryrlock %t\n", w)
	}
}

func Try2() {
	mu := ally.Locker("test")
	fmt.Printf("%s, outer locker locked\n", time.Now())
	mu.Lock()
	go func() {
		for {
			fmt.Printf("%s, trying lock\n", time.Now())
			if mu.TryLock(2 * time.Second) {
				fmt.Printf("%s, try lock got\n", time.Now())
				break
			} else {
				fmt.Printf("%s, try lock timeout\n", time.Now())
			}
		}
	}()
	time.Sleep(3 * time.Second)
	mu.Unlock()
	fmt.Printf("%s, outer locker unlocked\n", time.Now())
}

func Simple() {
	mu := ally.Locker("test")

	fmt.Printf("getting lock...\n")
	mu.Lock()
	fmt.Printf("lock got!\n")

	fmt.Printf("freeing lock...\n")
	mu.Unlock()
	fmt.Printf("lock freed!\n")
}

func Timeout() {
	mu := ally.Locker("test")
	fmt.Printf("%d, getting lock...\n", ally.ID)
	mu.Lock()
	fmt.Printf("%d, lock got!\n", ally.ID)

	fmt.Printf("%d, getting lock again...\n", ally.ID)
	mu.Lock()
	fmt.Printf("%d, lock got again???\n", ally.ID)
}

func Dirty() {
	mu1 := ally.Locker("test")
	mu2 := ally.Locker("test")
	mu1.Lock()
	mu2.Unlock()
}

func Concurrent() {
	var wg sync.WaitGroup
	wg.Add(16 * 16)
	for i := 0; i < 16; i++ {
		go func() {
			mu := ally.Locker("test")
			for j := 0; j < 16; j++ {
				go func() {
					defer wg.Done()

					id := fmt.Sprintf("id=%d, i=%d, j=%d", ally.ID, i, j)
					fmt.Printf("%s, getting lock...\n", id)
					mu.Lock()
					fmt.Printf("***************** %s, lock got! *****************\n", id)

					fmt.Printf("%s, freeing lock\n", id)
					mu.Unlock()
					fmt.Printf("%s, lock freed\n", id)
				}()
			}
		}()
	}
	wg.Wait()
}

func Concurrent2() {
	var wg sync.WaitGroup
	wg.Add(16 * 16)
	for i := 0; i < 16; i++ {
		go func() {
			mu := ally.Locker("test")
			for j := 0; j < 16; j++ {
				go func() {
					defer wg.Done()

					for k := 0; true; k++ {
						id := fmt.Sprintf("id=%d, i=%d, j=%d, k=%d", ally.ID, i, j, k)
						fmt.Printf("%s, getting lock...\n", id)
						mu.Lock()
						fmt.Printf("***************** %s, lock got! *****************\n", id)

						//time.Sleep(1000 * time.Millisecond)

						fmt.Printf("%s, freeing lock\n", id)
						mu.Unlock()
						fmt.Printf("%s, lock freed\n", id)
					}
				}()
			}
		}()
	}
	wg.Wait()
}

func Bench() {
	var wg sync.WaitGroup

	threads := 128
	loops := 128000 / threads

	fmt.Printf("(threads %d, loops %d, ops %d): ", threads, loops, threads*loops)

	var ts = time.Now()
	var elapsed_lock int64
	var elapsed_unlock int64
	for thread := 0; thread < threads; thread++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for loop := 0; loop < loops; loop++ {
				name := fmt.Sprintf("test-%d", thread*loops+loop)
				mu := ally.Locker(name)
				{
					ts := time.Now()
					mu.Lock()
					atomic.AddInt64(&elapsed_lock, int64(time.Since(ts)))
				}
				{
					ts := time.Now()
					mu.Unlock()
					atomic.AddInt64(&elapsed_unlock, int64(time.Since(ts)))
				}
			}
		}()
	}

	wg.Wait()

	fmt.Printf("elapsed %s(%dops/ms), lock %s(%s/op), unlock %s(%s/op)\n",
		time.Since(ts), time.Duration(threads*loops*2)/(time.Since(ts)/time.Millisecond),
		time.Duration(elapsed_lock), time.Duration(elapsed_lock)/time.Duration(threads*loops),
		time.Duration(elapsed_unlock), time.Duration(elapsed_unlock)/time.Duration(threads*loops),
	)
}

func Bench2() {
	var wg sync.WaitGroup

	threads := 12800
	loops := 1280000 / threads

	fmt.Printf("(threads %d, loops %d, ops %d): ", threads, loops, threads*loops)

	var ts = time.Now()
	var elapsed_lock int64
	var elapsed_unlock int64
	for thread := 0; thread < threads; thread++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mu := ally.Locker(fmt.Sprintf("test-%d", thread))
			for loop := 0; loop < loops; loop++ {
				{
					ts := time.Now()
					mu.Lock()
					atomic.AddInt64(&elapsed_lock, int64(time.Since(ts)))
				}
				{
					ts := time.Now()
					mu.Unlock()
					atomic.AddInt64(&elapsed_unlock, int64(time.Since(ts)))
				}
			}
		}()
	}

	wg.Wait()

	fmt.Printf("elapsed %s(%dops/ms), lock %s(%s/op), unlock %s(%s/op)\n",
		time.Since(ts), time.Duration(threads*loops*2)/(time.Since(ts)/time.Millisecond),
		time.Duration(elapsed_lock), time.Duration(elapsed_lock)/time.Duration(threads*loops),
		time.Duration(elapsed_unlock), time.Duration(elapsed_unlock)/time.Duration(threads*loops),
	)
}
