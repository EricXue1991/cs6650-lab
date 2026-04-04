package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"time"
)

type SetRequest struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type GetResponse struct {
	Key     string `json:"key"`
	Value   string `json:"value"`
	Version int64  `json:"version"`
}

type ResultRow struct {
	Timestamp            string
	Operation            string
	Key                  string
	LatencyMs            float64
	Success              bool
	HTTPStatus           int
	ReturnedVersion      int64
	ExpectedVersion      int64
	StaleRead            bool
	TimeSinceLastWriteMs float64
}

func main() {
	baseURL := flag.String("base", "http://localhost:9101", "base URL of the database node")
	requests := flag.Int("requests", 200, "total number of requests")
	keyCount := flag.Int("keys", 20, "number of hot keys")
	writeRatio := flag.Int("write-ratio", 10, "write percentage, e.g. 10 means 10% writes and 90% reads")
	outFile := flag.String("out", "load_test_results.csv", "output CSV file")
	seed := flag.Int64("seed", time.Now().UnixNano(), "random seed")
	warmup := flag.Bool("warmup", true, "preload each key once before the main run")
	flag.Parse()

	if *writeRatio < 0 || *writeRatio > 100 {
		fmt.Println("write-ratio must be between 0 and 100")
		os.Exit(1)
	}

	rng := rand.New(rand.NewSource(*seed))

	keys := make([]string, 0, *keyCount)
	for i := 0; i < *keyCount; i++ {
		keys = append(keys, fmt.Sprintf("k%d", i))
	}

	expectedVersion := make(map[string]int64)
	lastWriteTime := make(map[string]time.Time)

	results := make([]ResultRow, 0, *requests)

	client := &http.Client{
		Timeout: 8 * time.Second,
	}

	// Warmup: write every key once so reads are meaningful.
	if *warmup {
		for i, key := range keys {
			row := doWrite(client, *baseURL, key, -100000-i, expectedVersion, lastWriteTime)
			if !row.Success {
				fmt.Printf("warmup write failed for key=%s status=%d\n", key, row.HTTPStatus)
			}
		}
	}

	for i := 0; i < *requests; i++ {
		key := keys[rng.Intn(len(keys))]
		isWrite := rng.Intn(100) < *writeRatio

		if isWrite {
			row := doWrite(client, *baseURL, key, i, expectedVersion, lastWriteTime)
			results = append(results, row)
		} else {
			row := doRead(client, *baseURL, key, expectedVersion, lastWriteTime)
			results = append(results, row)
		}
	}

	if err := writeCSV(*outFile, results); err != nil {
		fmt.Printf("failed to write CSV: %v\n", err)
		os.Exit(1)
	}

	totalReads := 0
	totalWrites := 0
	staleReads := 0
	successReads := 0
	successWrites := 0

	for _, r := range results {
		if r.Operation == "read" {
			totalReads++
			if r.Success {
				successReads++
			}
			if r.StaleRead {
				staleReads++
			}
		} else if r.Operation == "write" {
			totalWrites++
			if r.Success {
				successWrites++
			}
		}
	}

	fmt.Println("Load test finished.")
	fmt.Printf("Base URL: %s\n", *baseURL)
	fmt.Printf("Total requests: %d\n", *requests)
	fmt.Printf("Writes: %d (success %d)\n", totalWrites, successWrites)
	fmt.Printf("Reads: %d (success %d)\n", totalReads, successReads)
	fmt.Printf("Stale reads: %d\n", staleReads)
	fmt.Printf("Output CSV: %s\n", *outFile)
}

func doWrite(client *http.Client, baseURL, key string, seq int, expectedVersion map[string]int64, lastWriteTime map[string]time.Time) ResultRow {
	value := fmt.Sprintf("value-%d", seq)

	body, _ := json.Marshal(SetRequest{
		Key:   key,
		Value: value,
	})

	start := time.Now()
	resp, err := client.Post(baseURL+"/set", "application/json", bytes.NewBuffer(body))
	latency := time.Since(start)

	row := ResultRow{
		Timestamp:            time.Now().Format(time.RFC3339Nano),
		Operation:            "write",
		Key:                  key,
		LatencyMs:            float64(latency.Microseconds()) / 1000.0,
		Success:              false,
		HTTPStatus:           0,
		ReturnedVersion:      0,
		ExpectedVersion:      expectedVersion[key],
		StaleRead:            false,
		TimeSinceLastWriteMs: 0,
	}

	if t, ok := lastWriteTime[key]; ok {
		row.TimeSinceLastWriteMs = float64(time.Since(t).Microseconds()) / 1000.0
	}

	if err != nil {
		return row
	}
	defer resp.Body.Close()

	row.HTTPStatus = resp.StatusCode

	if resp.StatusCode != http.StatusCreated {
		return row
	}

	var result GetResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return row
	}

	row.Success = true
	row.ReturnedVersion = result.Version
	row.ExpectedVersion = result.Version

	expectedVersion[key] = result.Version
	lastWriteTime[key] = time.Now()

	return row
}

func doRead(client *http.Client, baseURL, key string, expectedVersion map[string]int64, lastWriteTime map[string]time.Time) ResultRow {
	start := time.Now()
	resp, err := client.Get(baseURL + "/get?key=" + key)
	latency := time.Since(start)

	row := ResultRow{
		Timestamp:            time.Now().Format(time.RFC3339Nano),
		Operation:            "read",
		Key:                  key,
		LatencyMs:            float64(latency.Microseconds()) / 1000.0,
		Success:              false,
		HTTPStatus:           0,
		ReturnedVersion:      0,
		ExpectedVersion:      expectedVersion[key],
		StaleRead:            false,
		TimeSinceLastWriteMs: -1,
	}

	if t, ok := lastWriteTime[key]; ok {
		row.TimeSinceLastWriteMs = float64(time.Since(t).Microseconds()) / 1000.0
	}

	if err != nil {
		return row
	}
	defer resp.Body.Close()

	row.HTTPStatus = resp.StatusCode

	if resp.StatusCode != http.StatusOK {
		if expectedVersion[key] > 0 {
			row.StaleRead = true
		}
		return row
	}

	var result GetResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return row
	}

	row.Success = true
	row.ReturnedVersion = result.Version

	if expectedVersion[key] > 0 && result.Version < expectedVersion[key] {
		row.StaleRead = true
	}

	return row
}

func writeCSV(filename string, rows []ResultRow) error {
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	header := []string{
		"timestamp",
		"operation",
		"key",
		"latency_ms",
		"success",
		"http_status",
		"returned_version",
		"expected_version",
		"stale_read",
		"time_since_last_write_ms",
	}
	if err := w.Write(header); err != nil {
		return err
	}

	for _, r := range rows {
		record := []string{
			r.Timestamp,
			r.Operation,
			r.Key,
			fmt.Sprintf("%.3f", r.LatencyMs),
			strconv.FormatBool(r.Success),
			strconv.Itoa(r.HTTPStatus),
			strconv.FormatInt(r.ReturnedVersion, 10),
			strconv.FormatInt(r.ExpectedVersion, 10),
			strconv.FormatBool(r.StaleRead),
			fmt.Sprintf("%.3f", r.TimeSinceLastWriteMs),
		}
		if err := w.Write(record); err != nil {
			return err
		}
	}

	return nil
}
