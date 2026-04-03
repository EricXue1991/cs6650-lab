package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"distributed-kv/internal/store"
)

type SetRequest struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type ReplicateRequest struct {
	Key     string `json:"key"`
	Value   string `json:"value"`
	Version int64  `json:"version"`
}

type GetResponse struct {
	Key     string `json:"key"`
	Value   string `json:"value"`
	Version int64  `json:"version"`
}

type NodeConfig struct {
	ID       string
	Port     string
	IsLeader bool
	Peers    []string
	R        int
	W        int
}

func main() {
	nodeID := flag.String("id", "node1", "node id")
	port := flag.String("port", "8080", "server port")
	role := flag.String("role", "follower", "leader or follower")
	peersRaw := flag.String("peers", "", "comma-separated peer addresses")
	rValue := flag.Int("r", 1, "read quorum")
	wValue := flag.Int("w", 1, "write quorum")
	flag.Parse()

	cfg := NodeConfig{
		ID:       *nodeID,
		Port:     *port,
		IsLeader: *role == "leader",
		Peers:    parsePeers(*peersRaw),
		R:        *rValue,
		W:        *wValue,
	}

	kv := store.NewStore()

	http.HandleFunc("/set", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if !cfg.IsLeader {
			http.Error(w, "writes must go to leader", http.StatusBadRequest)
			return
		}

		var req SetRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json body", http.StatusBadRequest)
			return
		}

		localValue, err := kv.Set(req.Key, req.Value)
		if err != nil {
			if errors.Is(err, store.ErrEmptyKey) {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		repReq := ReplicateRequest{
			Key:     req.Key,
			Value:   localValue.Value,
			Version: localValue.Version,
		}

		// W=1: leader local write is enough, return immediately.
		// Replication continues asynchronously in the background.
		if cfg.W == 1 {
			go replicateToAllFollowers(cfg.Peers, repReq)
			resp := GetResponse{
				Key:     req.Key,
				Value:   localValue.Value,
				Version: localValue.Version,
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		// W>1: wait for enough replicas synchronously.
		successCount := 1 // leader itself
		replicaSuccesses := replicateSync(cfg.Peers, repReq)
		successCount += replicaSuccesses

		if successCount < cfg.W {
			http.Error(w, fmt.Sprintf("write failed quorum: success=%d required=%d", successCount, cfg.W), http.StatusServiceUnavailable)
			return
		}

		resp := GetResponse{
			Key:     req.Key,
			Value:   localValue.Value,
			Version: localValue.Version,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(resp)
	})

	http.HandleFunc("/replicate", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req ReplicateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json body", http.StatusBadRequest)
			return
		}

		time.Sleep(100 * time.Millisecond)

		if err := kv.ApplyReplication(req.Key, req.Value, req.Version); err != nil {
			if errors.Is(err, store.ErrEmptyKey) {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusCreated)
	})

	http.HandleFunc("/get", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		key := r.URL.Query().Get("key")
		if key == "" {
			http.Error(w, "missing key", http.StatusBadRequest)
			return
		}

		// follower currently returns local value only
		if !cfg.IsLeader {
			v, err := kv.Get(key)
			if err != nil {
				if errors.Is(err, store.ErrKeyNotFound) {
					http.Error(w, err.Error(), http.StatusNotFound)
					return
				}
				http.Error(w, "internal server error", http.StatusInternalServerError)
				return
			}

			resp := GetResponse{
				Key:     key,
				Value:   v.Value,
				Version: v.Version,
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		results := make([]GetResponse, 0, 1+len(cfg.Peers))

		localValue, err := kv.Get(key)
		if err == nil {
			results = append(results, GetResponse{
				Key:     key,
				Value:   localValue.Value,
				Version: localValue.Version,
			})
		} else if !errors.Is(err, store.ErrKeyNotFound) {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		if cfg.R > 1 {
			for _, peer := range cfg.Peers {
				val, readErr := readFromReplica(peer, key)
				if readErr != nil {
					log.Printf("read from %s failed: %v", peer, readErr)
					continue
				}
				results = append(results, val)
			}
		}

		if len(results) < cfg.R {
			http.Error(w, fmt.Sprintf("read failed quorum: got=%d required=%d", len(results), cfg.R), http.StatusServiceUnavailable)
			return
		}

		latest, err := pickLatest(results)
		if err != nil {
			http.Error(w, "key not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(latest)
	})

	http.HandleFunc("/internal_read", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		key := r.URL.Query().Get("key")
		if key == "" {
			http.Error(w, "missing key", http.StatusBadRequest)
			return
		}

		time.Sleep(50 * time.Millisecond)

		v, err := kv.Get(key)
		if err != nil {
			if errors.Is(err, store.ErrKeyNotFound) {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		resp := GetResponse{
			Key:     key,
			Value:   v.Value,
			Version: v.Version,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	})

	http.HandleFunc("/local_read", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		key := r.URL.Query().Get("key")
		if key == "" {
			http.Error(w, "missing key", http.StatusBadRequest)
			return
		}

		v, err := kv.Get(key)
		if err != nil {
			if errors.Is(err, store.ErrKeyNotFound) {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		resp := GetResponse{
			Key:     key,
			Value:   v.Value,
			Version: v.Version,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	})

	log.Printf("node=%s role=%s port=%s peers=%v R=%d W=%d", cfg.ID, roleName(cfg.IsLeader), cfg.Port, cfg.Peers, cfg.R, cfg.W)
	log.Fatal(http.ListenAndServe(":"+cfg.Port, nil))
}

func parsePeers(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return []string{}
	}

	parts := strings.Split(raw, ",")
	peers := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			peers = append(peers, p)
		}
	}
	return peers
}

func roleName(isLeader bool) string {
	if isLeader {
		return "leader"
	}
	return "follower"
}

func replicateToFollower(peer string, req ReplicateRequest) error {
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}

	resp, err := http.Post(peer+"/replicate", "application/json", bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	return nil
}

func replicateSync(peers []string, req ReplicateRequest) int {
	successes := 0
	for _, peer := range peers {
		if err := replicateToFollower(peer, req); err != nil {
			log.Printf("replication to %s failed: %v", peer, err)
			continue
		}
		successes++
		time.Sleep(200 * time.Millisecond)
	}
	return successes
}

func replicateToAllFollowers(peers []string, req ReplicateRequest) {
	for _, peer := range peers {
		if err := replicateToFollower(peer, req); err != nil {
			log.Printf("async replication to %s failed: %v", peer, err)
			continue
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func readFromReplica(baseURL string, key string) (GetResponse, error) {
	resp, err := http.Get(baseURL + "/internal_read?key=" + url.QueryEscape(key))
	if err != nil {
		return GetResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return GetResponse{}, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var result GetResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return GetResponse{}, err
	}

	return result, nil
}

func pickLatest(values []GetResponse) (GetResponse, error) {
	if len(values) == 0 {
		return GetResponse{}, fmt.Errorf("no values available")
	}

	latest := values[0]
	for _, v := range values[1:] {
		if v.Version > latest.Version {
			latest = v
		}
	}
	return latest, nil
}
