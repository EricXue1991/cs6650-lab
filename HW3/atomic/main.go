package main

import (
	"fmt"
	"sync"
	"sync/atomic"
)

func main() {
	const goroutines = 50
	const perG = 100000

	
	var counter int64
	var wg sync.WaitGroup

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < perG; j++ {
				counter++
			}
		}()
	}
	wg.Wait()

	fmt.Println("Non-atomic counter =", counter)

	
	counter = 0
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < perG; j++ {
				atomic.AddInt64(&counter, 1)
			}
		}()
	}
	wg.Wait()

	fmt.Println("Atomic counter =", counter)
}