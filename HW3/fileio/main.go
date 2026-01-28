package main

import (
	"bufio"
	"fmt"
	"os"
	"time"
)

func unbufferedWrite(path string, n int) time.Duration {
	f, err := os.Create(path)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	start := time.Now()
	for i := 0; i < n; i++ {
		_, err := f.Write([]byte("hello\n"))
		if err != nil {
			panic(err)
		}
	}
	return time.Since(start)
}

func bufferedWrite(path string, n int) time.Duration {
	f, err := os.Create(path)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	writer := bufio.NewWriter(f)

	start := time.Now()
	for i := 0; i < n; i++ {
		_, err := writer.WriteString("hello\n")
		if err != nil {
			panic(err)
		}
	}
	writer.Flush()
	return time.Since(start)
}

func main() {
	n := 100000

	d1 := unbufferedWrite("unbuffered.txt", n)
	d2 := bufferedWrite("buffered.txt", n)

	fmt.Println("unbuffered duration =", d1)
	fmt.Println("buffered   duration =", d2)
}