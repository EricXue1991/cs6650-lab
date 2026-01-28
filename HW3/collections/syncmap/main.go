package main

import (
	"fmt"
	"sync"
	"time"
)

func main() {
	var m sync.Map
	var wg sync.WaitGroup

	start := time.Now()

	wg.Add(50)
	for g := 0; g < 50; g++ {
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				m.Store(g*1000+i, i)
			}
		}(g)
	}

	wg.Wait()

	count := 0
	m.Range(func(_, _ any) bool {
		count++
		return true
	})

	elapsed := time.Since(start)

	fmt.Println("count =", count)
	fmt.Println("elapsed =", elapsed)
}
