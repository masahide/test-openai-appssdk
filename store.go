package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type fileTokenStore struct {
	Path string
}

func (s fileTokenStore) Load() (tokenSet, error) {
	data, err := os.ReadFile(s.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return tokenSet{}, err
		}
		return tokenSet{}, err
	}

	var token tokenSet
	if err := json.Unmarshal(data, &token); err != nil {
		return tokenSet{}, err
	}
	return token, nil
}

func (s fileTokenStore) Save(token tokenSet) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.Path, data, 0o600)
}
