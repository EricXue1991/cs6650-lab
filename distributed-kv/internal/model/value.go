package model

type VersionedValue struct {
	Value   string `json:"value"`
	Version int64  `json:"version"`
}