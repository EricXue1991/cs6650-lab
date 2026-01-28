package main

import (
	"fmt"
	"sync"
)

func main() {
	m := make(map[int]int)

	var wg sync.WaitGroup
	wg.Add(50)

	for g := 0; g < 50; g++ {
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				m[g*1000+i] = i
			}
		}(g)
	}

	wg.Wait()
	fmt.Println("len(m) =", len(m))
}