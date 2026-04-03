package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
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

func main() {
	coordinator := flag.String("coordinator", "http://localhost:9303", "node that receives the write")
	othersRaw := flag.String("others", "http://localhost:9301,http://localhost:9302,http://localhost:9304,http://localhost:9305", "comma-separated other nodes")
	flag.Parse()

	others := splitCSV(*othersRaw)

	fmt.Println("=== Leaderless Tests ===")
	fmt.Printf("coordinator = %s\n", *coordinator)
	fmt.Printf("others      = %v\n", others)

	if err := testFinalConsistency(*coordinator, others); err != nil {
		fmt.Printf("Final consistency test FAILED: %v\n", err)
	} else {
		fmt.Println("Final consistency test PASSED")
	}

	fmt.Println()

	if err := testWindow(*coordinator, others); err != nil {
		fmt.Printf("Window test FAILED: %v\n", err)
	} else {
		fmt.Println("Window test FINISHED")
	}
}

func testFinalConsistency(coordinator string, others []string) error {
	fmt.Println("Running final consistency test...")

	key := fmt.Sprintf("leaderless-final-%d", time.Now().UnixNano())
	value := "stable-value"

	resp, err := doSet(coordinator, key, value)
	if err != nil {
		return fmt.Errorf("set failed: %w", err)
	}

	if resp.Value != value {
		return fmt.Errorf("set returned wrong value: got=%s want=%s", resp.Value, value)
	}

	coordRead, err := doGet(coordinator, key)
	if err != nil {
		return fmt.Errorf("coordinator get failed after ack: %w", err)
	}
	if coordRead.Value != value {
		return fmt.Errorf("coordinator value mismatch: got=%s want=%s", coordRead.Value, value)
	}

	for _, node := range others {
		r, err := doGet(node, key)
		if err != nil {
			return fmt.Errorf("other node %s get failed after ack: %w", node, err)
		}
		if r.Value != value {
			return fmt.Errorf("other node %s mismatch: got=%s want=%s", node, r.Value, value)
		}
	}

	fmt.Println("  - write acked by coordinator")
	fmt.Println("  - coordinator read is consistent")
	fmt.Println("  - all other nodes are consistent after ack")

	return nil
}

func testWindow(coordinator string, others []string) error {
	fmt.Println("Running write-window inconsistency test...")
	fmt.Println("This test reads other nodes while the coordinator write is still in progress.")

	if len(others) == 0 {
		return fmt.Errorf("no other nodes provided")
	}

	iterations := 20
	target := others[len(others)-1]

	totalFresh := 0
	totalNotFound := 0
	totalOther := 0

	for i := 0; i < iterations; i++ {
		key := fmt.Sprintf("leaderless-window-%d-%d", time.Now().UnixNano(), i)
		value := fmt.Sprintf("v-%d", i)

		var writeDone atomic.Bool
		resultCh := make(chan windowResult, 1)

		go func() {
			res := sampleDuringWrite(target, key, value, &writeDone)
			resultCh <- res
		}()

		_, err := doSet(coordinator, key, value)
		writeDone.Store(true)
		if err != nil {
			return fmt.Errorf("iteration %d set failed: %w", i, err)
		}

		res := <-resultCh
		totalFresh += res.fresh
		totalNotFound += res.notFound
		totalOther += res.other
	}

	fmt.Printf("  iterations      = %d\n", iterations)
	fmt.Printf("  fresh reads      = %d\n", totalFresh)
	fmt.Printf("  not found reads  = %d\n", totalNotFound)
	fmt.Printf("  other reads      = %d\n", totalOther)

	if totalNotFound > 0 || totalOther > 0 {
		fmt.Println("  Observed inconsistency during the write window.")
	} else {
		fmt.Println("  No inconsistency observed in this run; timing may vary.")
	}

	return nil
}

type windowResult struct {
	fresh    int
	notFound int
	other    int
}

func sampleDuringWrite(targetNode, key, expectedValue string, writeDone *atomic.Bool) windowResult {
	res := windowResult{}

	for !writeDone.Load() {
		r, status, err := doGetWithStatus(targetNode, key)
		if err != nil {
			res.other++
			time.Sleep(10 * time.Millisecond)
			continue
		}

		if status == http.StatusNotFound {
			res.notFound++
		} else if status == http.StatusOK {
			if r.Value == expectedValue {
				res.fresh++
			} else {
				res.other++
			}
		} else {
			res.other++
		}

		time.Sleep(10 * time.Millisecond)
	}

	return res
}

func doSet(baseURL, key, value string) (GetResponse, error) {
	body, err := json.Marshal(SetRequest{
		Key:   key,
		Value: value,
	})
	if err != nil {
		return GetResponse{}, err
	}

	resp, err := http.Post(baseURL+"/set", "application/json", bytes.NewBuffer(body))
	if err != nil {
		return GetResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		return GetResponse{}, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(raw))
	}

	var result GetResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return GetResponse{}, err
	}
	return result, nil
}

func doGet(baseURL, key string) (GetResponse, error) {
	resp, err := http.Get(baseURL + "/get?key=" + key)
	if err != nil {
		return GetResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return GetResponse{}, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(raw))
	}

	var result GetResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return GetResponse{}, err
	}
	return result, nil
}

func doGetWithStatus(baseURL, key string) (GetResponse, int, error) {
	resp, err := http.Get(baseURL + "/get?key=" + key)
	if err != nil {
		return GetResponse{}, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return GetResponse{}, http.StatusNotFound, nil
	}

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return GetResponse{}, resp.StatusCode, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(raw))
	}

	var result GetResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return GetResponse{}, resp.StatusCode, err
	}

	return result, resp.StatusCode, nil
}

func splitCSV(raw string) []string {
	out := make([]string, 0)
	cur := ""
	for _, ch := range raw {
		if ch == ',' {
			if cur != "" {
				out = append(out, cur)
			}
			cur = ""
			continue
		}
		if ch != ' ' {
			cur += string(ch)
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
