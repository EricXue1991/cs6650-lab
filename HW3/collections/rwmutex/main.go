package main

import (
	"fmt"
	"sync"
	"time"
)

type SafeMap struct {
	mu sync.RWMutex
	m  map[int]int
}

func main() {
	sm := &SafeMap{m: make(map[int]int)}
	var wg sync.WaitGroup

	start := time.Now()

	wg.Add(50)
	for g := 0; g < 50; g++ {
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				key := g*1000 + i
				sm.mu.Lock()   
				sm.m[key] = i
				sm.mu.Unlock()
			}
		}(g)
	}

	wg.Wait()
	elapsed := time.Since(start)

	fmt.Println("len(m) =", len(sm.m))
	fmt.Println("elapsed =", elapsed)
}
