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

func (s *Store) Get(key string) (model.VersionedValue, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	v, ok := s.data[key]
	if !ok {
		return model.VersionedValue{}, ErrKeyNotFound
	}

	return v, nil
}