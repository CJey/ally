package main

import (
	"fmt"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/cjey/ally"
)

const (
	NS    = "test"
	TOTAL = 1280000
)

func main() {
	done := ally.NewEvent()
	ally.ExitHook = func() {
		defer done.Emit()
		os.Exit(1)
	}
	ally.Ready()

	Verify()

	threads := 1 * runtime.NumCPU()
	if ally.ID > 0 {
		threads = 8 * runtime.NumCPU()
	}
	loops := TOTAL / threads

	fmt.Printf("---\n")
	BenchSet(threads, loops)
	BenchGet(threads, loops)
	BenchDel(threads, loops)

	<-done.Yes
}

func Verify() {
	var key, value = "cjey", "hou"

	var cache = ally.Cache(NS)

	cache.Set(key, []byte(value))
	if val, ok := cache.Get(key); !ok || string(val) != value {
		panic(fmt.Errorf("set value dismatched with get"))
	}

	cache.Del(key)
	if _, ok := cache.Get(key); ok {
		panic(fmt.Errorf("del not workd"))
	}

	fmt.Printf("Verify Set/Get/Del ok\n")
}

func BenchSet(threads, loops int) {
	fmt.Printf("Set (threads %d, loops %d, ops %d): ", threads, loops, threads*loops)
	var wg sync.WaitGroup
	var cache = ally.Cache("test")
	ts := time.Now()
	for thread := 0; thread < threads; thread++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for loop := 0; loop < loops; loop++ {
				cache.Set(fmt.Sprintf("%d", thread*loops+loop), []byte(time.Now().String()))
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(ts)
	fmt.Printf("elapsed %s, %dops/ms\n", elapsed.Truncate(time.Millisecond), threads*loops/int(elapsed/time.Millisecond))
}

func BenchGet(threads, loops int) {
	fmt.Printf("Get (threads %d, loops %d, ops %d): ", threads, loops, threads*loops)
	var wg sync.WaitGroup
	var cache = ally.Cache("test")
	ts := time.Now()
	for thread := 0; thread < threads; thread++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for loop := 0; loop < loops; loop++ {
				cache.Get(fmt.Sprintf("%d", thread*loops+loop))
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(ts)
	fmt.Printf("elapsed %s, %dops/ms\n", elapsed.Truncate(time.Millisecond), threads*loops/int(elapsed/time.Millisecond))
}

func BenchDel(threads, loops int) {
	fmt.Printf("Del (threads %d, loops %d, ops %d): ", threads, loops, threads*loops)
	var wg sync.WaitGroup
	var cache = ally.Cache("test")
	ts := time.Now()
	for thread := 0; thread < threads; thread++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for loop := 0; loop < loops; loop++ {
				cache.Del(fmt.Sprintf("%d", thread*loops+loop))
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(ts)
	fmt.Printf("elapsed %s, %dops/ms\n", elapsed.Truncate(time.Millisecond), threads*loops/int(elapsed/time.Millisecond))
}
