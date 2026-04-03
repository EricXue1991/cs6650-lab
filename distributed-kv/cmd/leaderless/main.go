package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
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
	ID    string
	Port  string
	Peers []string
}

func main() {
	nodeID := flag.String("id", "node1", "node id")
	port := flag.String("port", "9301", "server port")
	peersRaw := flag.String("peers", "", "comma-separated peer addresses")
	flag.Parse()

	cfg := NodeConfig{
		ID:    *nodeID,
		Port:  *port,
		Peers: parsePeers(*peersRaw),
	}

	kv := store.NewStore()

	http.HandleFunc("/set", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req SetRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json body", http.StatusBadRequest)
			return
		}

		// This node becomes the write coordinator for this request.
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

		successCount := 1 // coordinator itself

		for _, peer := range cfg.Peers {
			if err := replicateToPeer(peer, repReq); err != nil {
				log.Printf("replication to %s failed: %v", peer, err)
				continue
			}
			successCount++
			time.Sleep(200 * time.Millisecond)
		}

		requiredW := 1 + len(cfg.Peers) // W = N = all nodes
		if successCount < requiredW {
			http.Error(w, fmt.Sprintf("write failed to reach all replicas: success=%d required=%d", successCount, requiredW), http.StatusServiceUnavailable)
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

	log.Printf("leaderless node=%s port=%s peers=%v", cfg.ID, cfg.Port, cfg.Peers)
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

func replicateToPeer(peer string, req ReplicateRequest) error {
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
