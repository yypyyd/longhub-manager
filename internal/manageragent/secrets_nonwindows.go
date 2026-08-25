//go:build !windows

package manageragent

import (
	"errors"
	"os"
	"strings"
)

type platformSecretStore struct{ path string }

func NewPlatformSecretStore(path string) SecretStore {
	return &platformSecretStore{path: path}
}

func (s *platformSecretStore) Get() (string, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if len(data) > 2048 {
		return "", errors.New("credential exceeds size limit")
	}
	return strings.TrimSpace(string(data)), nil
}

func (s *platformSecretStore) Set(value string) error {
	if value == "" || len(value) > 2048 {
		return errors.New("credential value is invalid")
	}
	return writeBytesAtomic(s.path, []byte(value))
}

func (s *platformSecretStore) Delete() error {
	err := os.Remove(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
