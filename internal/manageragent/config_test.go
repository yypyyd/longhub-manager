package manageragent

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type memorySecretStore struct {
	value     string
	setErr    error
	deleteErr error
}

func (s *memorySecretStore) Get() (string, error) { return s.value, nil }
func (s *memorySecretStore) Set(value string) error {
	if s.setErr != nil {
		return s.setErr
	}
	s.value = value
	return nil
}
func (s *memorySecretStore) Delete() error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	s.value = ""
	return nil
}

func newTestConfigStore(t *testing.T, secrets SecretStore) *ConfigStore {
	t.Helper()
	store, err := NewConfigStore(filepath.Join(t.TempDir(), "manager-agent.json"), secrets)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestConfigStoreKeepsAPIKeyOutOfConfigFile(t *testing.T) {
	secrets := &memorySecretStore{}
	store := newTestConfigStore(t, secrets)
	config, err := store.Save("https://models.example.test/v1/", "example-model", "secret-key")
	if err != nil {
		t.Fatal(err)
	}
	if !config.Configured || config.BaseURL != "https://models.example.test/v1" || secrets.value != "secret-key" {
		t.Fatalf("unexpected saved config: %#v", config)
	}
	data, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "secret-key") {
		t.Fatal("API key leaked into the non-secret config file")
	}
	loaded, key, err := store.Credentials()
	if err != nil || !loaded.Configured || key != "secret-key" {
		t.Fatalf("credentials not available: config=%#v key=%q err=%v", loaded, key, err)
	}
}

func TestConfigStorePersistsValidatedProtocol(t *testing.T) {
	store := newTestConfigStore(t, &memorySecretStore{})
	config, err := store.SaveWithProtocol("https://models.example.test/v1", "example-model", ProtocolResponses, "key")
	if err != nil || config.Protocol != ProtocolResponses {
		t.Fatalf("Responses protocol was not saved: config=%#v err=%v", config, err)
	}
	loaded, _, err := store.Credentials()
	if err != nil || loaded.Protocol != ProtocolResponses {
		t.Fatalf("Responses protocol was not loaded: config=%#v err=%v", loaded, err)
	}
	if _, err := store.SaveWithProtocol("https://models.example.test/v1", "example-model", "unknown", "key"); err == nil {
		t.Fatal("unknown model protocol was accepted")
	}
}

func TestConfigStoreRejectsUnsafeEndpoints(t *testing.T) {
	for _, endpoint := range []string{
		"http://models.example.test/v1",
		"https://user:pass@models.example.test/v1",
		"https://models.example.test/v1?key=value",
		"file:///tmp/model",
	} {
		store := newTestConfigStore(t, &memorySecretStore{})
		if _, err := store.Save(endpoint, "model", "key"); err == nil {
			t.Fatalf("unsafe endpoint accepted: %s", endpoint)
		}
	}
	store := newTestConfigStore(t, &memorySecretStore{})
	if _, err := store.Save("http://127.0.0.1:1234/v1", "local", "key"); err != nil {
		t.Fatalf("loopback endpoint should be accepted: %v", err)
	}
	if _, err := store.Save("http://127.0.0.1:1234/v1", "local", " key "); err == nil {
		t.Fatal("whitespace-padded API key was accepted")
	}
}

func TestConfigStoreRollsBackFileWhenSecretSaveFails(t *testing.T) {
	secrets := &memorySecretStore{}
	store := newTestConfigStore(t, secrets)
	if _, err := store.Save("https://one.example/v1", "one", "first"); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	secrets.setErr = errors.New("credential manager unavailable")
	if _, err := store.Save("https://two.example/v1", "two", "second"); err == nil {
		t.Fatal("expected secret store failure")
	}
	after, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) || secrets.value != "first" {
		t.Fatal("config transaction was not rolled back")
	}
}

func TestConfigStoreRestoresFileWhenSecretDeleteFails(t *testing.T) {
	secrets := &memorySecretStore{}
	store := newTestConfigStore(t, secrets)
	if _, err := store.Save("https://one.example/v1", "one", "first"); err != nil {
		t.Fatal(err)
	}
	secrets.deleteErr = errors.New("credential manager unavailable")
	if err := store.Delete(); err == nil {
		t.Fatal("expected delete failure")
	}
	if _, err := os.Stat(store.path); err != nil {
		t.Fatalf("config file was not restored: %v", err)
	}
}
