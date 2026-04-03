package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"
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
	mode := flag.String("mode", "final", "test mode: final or window")
	leader := flag.String("leader", "http://localhost:9201", "leader base url")
	followersRaw := flag.String("followers", "http://localhost:9202,http://localhost:9203,http://localhost:9204,http://localhost:9205", "comma-separated follower base urls")
	flag.Parse()

	followers := parseFollowers(*followersRaw)

	fmt.Println("=== Leader-Follower Tests ===")
	fmt.Printf("mode      = %s\n", *mode)
	fmt.Printf("leader    = %s\n", *leader)
	fmt.Printf("followers = %v\n", followers)

	switch *mode {
	case "final":
		if err := testFinalConsistency(*leader, followers); err != nil {
			fmt.Printf("Final consistency test FAILED: %v\n", err)
		} else {
			fmt.Println("Final consistency test PASSED")
		}
	case "window":
		if err := testStaleReadWindow(*leader, followers); err != nil {
			fmt.Printf("Stale read window test FAILED: %v\n", err)
		} else {
			fmt.Println("Stale read window test FINISHED")
		}
	default:
		fmt.Println("unknown mode; use -mode=final or -mode=window")
	}
}

func parseFollowers(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return []string{}
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func testFinalConsistency(leader string, followers []string) error {
	fmt.Println("Running final consistency test...")

	key := fmt.Sprintf("consistency-key-%d", time.Now().UnixNano())
	value := "stable-value"

	setResp, err := doSet(leader, key, value)
	if err != nil {
		return fmt.Errorf("set to leader failed: %w", err)
	}

	if setResp.Value != value {
		return fmt.Errorf("leader set returned wrong value: got=%s want=%s", setResp.Value, value)
	}

	leaderRead, err := doGet(leader, key)
	if err != nil {
		return fmt.Errorf("leader get failed after ack: %w", err)
	}

	if leaderRead.Value != value {
		return fmt.Errorf("leader get mismatch: got=%s want=%s", leaderRead.Value, value)
	}

	for _, follower := range followers {
		localRead, err := doLocalRead(follower, key)
		if err != nil {
			return fmt.Errorf("follower %s local_read failed after ack: %w", follower, err)
		}
		if localRead.Value != value {
			return fmt.Errorf("follower %s value mismatch: got=%s want=%s", follower, localRead.Value, value)
		}
	}

	fmt.Println("  - write acked by leader")
	fmt.Println("  - leader read is consistent")
	fmt.Println("  - all follower local_read values are consistent after ack")

	return nil
}

func testStaleReadWindow(leader string, followers []string) error {
	fmt.Println("Running stale read window test...")
	fmt.Println("This test assumes W=1 so leader returns before all followers finish replication.")

	if len(followers) == 0 {
		return fmt.Errorf("no followers provided")
	}

	iterations := 30
	staleCount := 0
	notFoundCount := 0
	freshCount := 0

	targetFollower := followers[len(followers)-1]

	for i := 0; i < iterations; i++ {
		key := fmt.Sprintf("hot-key-%d-%d", time.Now().UnixNano(), i)
		value := fmt.Sprintf("v-%d", i)

		_, err := doSet(leader, key, value)
		if err != nil {
			return fmt.Errorf("iteration %d: set failed: %w", i, err)
		}

		resp, statusCode, err := doLocalReadWithStatus(targetFollower, key)
		if err != nil {
			return fmt.Errorf("iteration %d: local_read request failed: %w", i, err)
		}

		if statusCode == http.StatusNotFound {
			notFoundCount++
		} else if statusCode == http.StatusOK {
			if resp.Value == value {
				freshCount++
			} else {
				staleCount++
			}
		}
	}

	fmt.Printf("  iterations      = %d\n", iterations)
	fmt.Printf("  fresh reads      = %d\n", freshCount)
	fmt.Printf("  stale reads      = %d\n", staleCount)
	fmt.Printf("  not found reads  = %d\n", notFoundCount)

	if staleCount == 0 && notFoundCount == 0 {
		fmt.Println("  No stale/not-found window observed in this run.")
		fmt.Println("  Try increasing iterations or delay values.")
	} else {
		fmt.Println("  Observed inconsistency window successfully.")
	}

	return nil
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

func doLocalRead(baseURL, key string) (GetResponse, error) {
	resp, err := http.Get(baseURL + "/local_read?key=" + key)
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

func doLocalReadWithStatus(baseURL, key string) (GetResponse, int, error) {
	resp, err := http.Get(baseURL + "/local_read?key=" + key)
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
