package main

import (
	"fmt"
	"runtime"
	"time"
)

func pingPong(rounds int) time.Duration {
	ch1 := make(chan struct{})
	ch2 := make(chan struct{})
	done := make(chan struct{})

	// goroutine A: 
	go func() {
		for i := 0; i < rounds; i++ {
			<-ch1
			ch2 <- struct{}{}
		}
		done <- struct{}{}
	}()

	start := time.Now()

	// goroutine B: 
	go func() {
		for i := 0; i < rounds; i++ {
			ch1 <- struct{}{}
			<-ch2
		}
		done <- struct{}{}
	}()

	<-done
	<-done
	return time.Since(start)
}

func main() {
	const rounds = 1_000_000

	runtime.GOMAXPROCS(1)
	d1 := pingPong(rounds)
	avg1 := d1 / time.Duration(2*rounds)
	fmt.Println("GOMAXPROCS=1")
	fmt.Println("  total =", d1)
	fmt.Println("  avg handoff =", avg1)

	runtime.GOMAXPROCS(runtime.NumCPU())
	d2 := pingPong(rounds)
	avg2 := d2 / time.Duration(2*rounds)
	fmt.Println("GOMAXPROCS=NumCPU")
	fmt.Println("  total =", d2)
	fmt.Println("  avg handoff =", avg2)
}