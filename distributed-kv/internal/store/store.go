package store

import (
	"errors"
	"sync"

	"distributed-kv/internal/model"
)

var ErrKeyNotFound = errors.New("key not found")
var ErrEmptyKey = errors.New("key cannot be empty")

type Store struct {
	mu   sync.RWMutex
	data map[string]model.VersionedValue
}

func NewStore() *Store {
	return &Store{
		data: make(map[string]model.VersionedValue),
	}
}

// Client write: generate the next logical version locally.
func (s *Store) Set(key string, value string) (model.VersionedValue, error) {
	if key == "" {
		return model.VersionedValue{}, ErrEmptyKey
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	current, exists := s.data[key]
	var newVersion int64 = 1
	if exists {
		newVersion = current.Version + 1
	}

	v := model.VersionedValue{
		Value:   value,
		Version: newVersion,
	}

	s.data[key] = v
	return v, nil
}

// Replication write: apply the version provided by the leader/coordinator.
func (s *Store) ApplyReplication(key string, value string, version int64) error {
	if key == "" {
		return ErrEmptyKey
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	current, exists := s.data[key]
	if exists && current.Version > version {
		return nil
	}

	s.data[key] = model.VersionedValue{
		Value:   value,
		Version: version,
	}

	return nil
}

func (s *Store) Get(key string) (model.VersionedValue, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	v, ok := s.data[key]
	if !ok {
		return model.VersionedValue{}, ErrKeyNotFound
	}

	return v, nil
}
