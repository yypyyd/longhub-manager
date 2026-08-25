package manageragent

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const ConfigSchema = "longhub/manager-agent-config/v1"

const (
	ProtocolAuto            = "auto"
	ProtocolResponses       = "responses"
	ProtocolChatCompletions = "chat_completions"
)

type Config struct {
	SchemaVersion string `json:"schema_version"`
	BaseURL       string `json:"base_url"`
	Model         string `json:"model"`
	Protocol      string `json:"protocol"`
	Configured    bool   `json:"configured"`
	UpdatedAt     string `json:"updated_at,omitempty"`
}

type SecretStore interface {
	Get() (string, error)
	Set(string) error
	Delete() error
}

type ConfigStore struct {
	path    string
	secrets SecretStore
	mu      sync.RWMutex
	now     func() time.Time
}

func NewConfigStore(path string, secrets SecretStore) (*ConfigStore, error) {
	if strings.TrimSpace(path) == "" || !filepath.IsAbs(path) || secrets == nil {
		return nil, errors.New("agent config path and secret store are required")
	}
	return &ConfigStore{path: filepath.Clean(path), secrets: secrets, now: time.Now}, nil
}

func (s *ConfigStore) Public() (Config, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	config, err := s.readLocked()
	if err != nil {
		return Config{}, err
	}
	secret, secretErr := s.secrets.Get()
	if secretErr != nil {
		return Config{}, secretErr
	}
	config.Configured = validAPIKey(secret) && config.BaseURL != "" && config.Model != ""
	return config, nil
}

func (s *ConfigStore) Credentials() (Config, string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	config, err := s.readLocked()
	if err != nil {
		return Config{}, "", err
	}
	secret, err := s.secrets.Get()
	if err != nil {
		return Config{}, "", err
	}
	if !validAPIKey(secret) || config.BaseURL == "" || config.Model == "" {
		return Config{}, "", errors.New("LongHub 管家模型尚未配置")
	}
	config.Configured = true
	return config, secret, nil
}

func (s *ConfigStore) Save(baseURL, model, apiKey string) (Config, error) {
	return s.SaveWithProtocol(baseURL, model, ProtocolAuto, apiKey)
}

func (s *ConfigStore) SaveWithProtocol(baseURL, model, protocol, apiKey string) (Config, error) {
	baseURL, model, err := validateConfig(baseURL, model)
	if err != nil {
		return Config{}, err
	}
	protocol, err = validateProtocol(protocol)
	if err != nil {
		return Config{}, err
	}
	if apiKey != "" && !validAPIKey(apiKey) {
		return Config{}, errors.New("API Key 格式无效")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	existingSecret, err := s.secrets.Get()
	if err != nil {
		return Config{}, fmt.Errorf("读取现有 API Key: %w", err)
	}
	if strings.TrimSpace(apiKey) == "" && !validAPIKey(existingSecret) {
		return Config{}, errors.New("首次配置必须提供 API Key")
	}
	previous, previousExisted, err := readOptionalFile(s.path)
	if err != nil {
		return Config{}, err
	}
	config := Config{
		SchemaVersion: ConfigSchema,
		BaseURL:       baseURL,
		Model:         model,
		Protocol:      protocol,
		Configured:    true,
		UpdatedAt:     s.now().UTC().Format(time.RFC3339Nano),
	}
	if err := writeConfigAtomic(s.path, config); err != nil {
		return Config{}, err
	}
	if strings.TrimSpace(apiKey) != "" {
		if err := s.secrets.Set(apiKey); err != nil {
			rollbackErr := restoreOptionalFile(s.path, previous, previousExisted)
			if rollbackErr != nil {
				return Config{}, fmt.Errorf("保存 API Key 失败且配置回滚失败: %v; %w", rollbackErr, err)
			}
			return Config{}, fmt.Errorf("保存 API Key: %w", err)
		}
	}
	return config, nil
}

func validAPIKey(value string) bool {
	return value != "" && len(value) <= 2048 && value == strings.TrimSpace(value) &&
		!strings.ContainsAny(value, "\r\n\x00")
}

func (s *ConfigStore) Delete() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, previousExisted, err := readOptionalFile(s.path)
	if err != nil {
		return err
	}
	if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := s.secrets.Delete(); err != nil {
		if rollbackErr := restoreOptionalFile(s.path, previous, previousExisted); rollbackErr != nil {
			return fmt.Errorf("删除 API Key 失败且配置回滚失败: %v; %w", rollbackErr, err)
		}
		return err
	}
	return nil
}

func (s *ConfigStore) readLocked() (Config, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{SchemaVersion: ConfigSchema}, nil
	}
	if err != nil {
		return Config{}, err
	}
	if len(data) > 32*1024 {
		return Config{}, errors.New("agent config exceeds size limit")
	}
	var config Config
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil || config.SchemaVersion != ConfigSchema {
		return Config{}, errors.New("agent config is invalid")
	}
	baseURL, model, err := validateConfig(config.BaseURL, config.Model)
	if err != nil {
		return Config{}, err
	}
	config.BaseURL = baseURL
	config.Model = model
	config.Protocol, err = validateProtocol(config.Protocol)
	if err != nil {
		return Config{}, err
	}
	config.Configured = false
	return config, nil
}

func validateProtocol(protocol string) (string, error) {
	protocol = strings.TrimSpace(protocol)
	if protocol == "" {
		return ProtocolAuto, nil
	}
	switch protocol {
	case ProtocolAuto, ProtocolResponses, ProtocolChatCompletions:
		return protocol, nil
	default:
		return "", errors.New("模型 API 协议无效")
	}
}

func validateConfig(baseURL, model string) (string, string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	model = strings.TrimSpace(model)
	if len(baseURL) > 2048 || len(model) == 0 || len(model) > 200 || strings.ContainsAny(model, "\r\n\x00") {
		return "", "", errors.New("模型配置格式无效")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Hostname() == "" {
		return "", "", errors.New("模型服务地址无效")
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLoopbackHost(parsed.Hostname())) {
		return "", "", errors.New("模型服务必须使用 HTTPS；本机回环地址可使用 HTTP")
	}
	return parsed.String(), model, nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func writeConfigAtomic(path string, config Config) error {
	encoded, err := json.Marshal(config)
	if err != nil {
		return err
	}
	return writeBytesAtomic(path, encoded)
}

func readOptionalFile(path string) ([]byte, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

func restoreOptionalFile(path string, data []byte, existed bool) error {
	if !existed {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	return writeBytesAtomic(path, data)
}

func writeBytesAtomic(path string, encoded []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".manager-agent-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(encoded); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
