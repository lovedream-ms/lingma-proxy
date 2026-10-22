package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/pelletier/go-toml/v2"
	"gopkg.in/yaml.v3"
	"lingma-ipc-proxy/internal/service"
)

type ClientConfigPreview struct {
	Tool        string `json:"tool"`
	Path        string `json:"path"`
	Before      string `json:"before"`
	After       string `json:"after"`
	Fingerprint string `json:"fingerprint"`
	Instruction string `json:"instruction"`
	Exists      bool   `json:"exists"`
}
type clientBackup struct {
	Tool        string
	Before      []byte
	Existed     bool
	AppliedHash string
}

func digest(raw []byte) string { h := sha256.Sum256(raw); return hex.EncodeToString(h[:]) }
func clientPath(tool string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch tool {
	case "claude":
		dir := os.Getenv("CLAUDE_CONFIG_DIR")
		if dir == "" {
			dir = filepath.Join(home, ".claude")
		}
		return filepath.Join(dir, "settings.json"), nil
	case "codex":
		dir := os.Getenv("CODEX_HOME")
		if dir == "" {
			dir = filepath.Join(home, ".codex")
		}
		return filepath.Join(dir, "config.toml"), nil
	case "continue":
		return filepath.Join(home, ".continue", "config.yaml"), nil
	}
	return "", fmt.Errorf("未知客户端")
}

func readClientFile(path string) ([]byte, bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !info.Mode().IsRegular() || info.Size() > 2*1024*1024 {
		return nil, false, fmt.Errorf("配置必须是小于 2 MiB 的普通文件")
	}
	raw, err := os.ReadFile(path)
	return raw, true, err
}

func table(data map[string]any, key string) (map[string]any, error) {
	if data[key] == nil {
		data[key] = map[string]any{}
	}
	out, ok := data[key].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s 必须是配置对象", key)
	}
	return out, nil
}

func mergeClientConfig(tool string, before []byte, cfg service.Config) ([]byte, string, error) {
	host := cfg.Host
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	base := "http://" + net.JoinHostPort(host, fmt.Sprint(cfg.Port))
	key := cfg.APIKey
	if key == "" {
		key = "lingma-local"
	}
	model := cfg.Model
	if strings.TrimSpace(model) == "" {
		return nil, "", fmt.Errorf("请先选择默认模型并保存设置")
	}
	data := map[string]any{}
	if len(before) > 0 {
		var err error
		switch tool {
		case "claude":
			err = json.Unmarshal(before, &data)
		case "codex":
			err = toml.Unmarshal(before, &data)
		case "continue":
			err = yaml.Unmarshal(before, &data)
		default:
			return nil, "", fmt.Errorf("未知客户端")
		}
		if err != nil {
			return nil, "", fmt.Errorf("原配置格式错误，未修改文件: %w", err)
		}
	}
	if data == nil {
		return nil, "", fmt.Errorf("原配置不能是 null")
	}
	switch tool {
	case "claude":
		env, err := table(data, "env")
		if err != nil {
			return nil, "", err
		}
		env["ANTHROPIC_BASE_URL"] = base
		env["ANTHROPIC_AUTH_TOKEN"] = key
		env["ANTHROPIC_MODEL"] = model
		delete(env, "ANTHROPIC_API_KEY")
		data["model"] = model
		raw, err := json.MarshalIndent(data, "", "  ")
		return raw, "重启 Claude Code 后生效；项目/组织设置或进程环境变量可能覆盖用户设置。", err
	case "codex":
		providers, err := table(data, "model_providers")
		if err != nil {
			return nil, "", err
		}
		providers["lingma_proxy"] = map[string]any{"name": "Lingma Proxy", "base_url": base + "/v1", "wire_api": "responses", "requires_openai_auth": false, "request_max_retries": 0, "http_headers": map[string]any{"Authorization": "Bearer " + key}}
		profiles, err := table(data, "profiles")
		if err != nil {
			return nil, "", err
		}
		profiles["lingma_proxy"] = map[string]any{"model_provider": "lingma_proxy", "model": model}
		raw, err := toml.Marshal(data)
		return raw, "运行 codex --profile lingma_proxy；保留原默认配置。TOML 注释和排版会重新生成，原文件已备份。", err
	case "continue":
		if data["name"] == nil {
			data["name"] = "Local Config"
		}
		if data["version"] == nil {
			data["version"] = "1.0.0"
		}
		if data["schema"] == nil {
			data["schema"] = "v1"
		}
		models := []any{}
		if value := data["models"]; value != nil {
			var ok bool
			models, ok = value.([]any)
			if !ok {
				return nil, "", fmt.Errorf("models 必须是列表")
			}
		}
		entry := map[string]any{"name": "Lingma Proxy", "provider": "openai", "model": model, "apiBase": base + "/v1", "apiKey": key, "roles": []string{"chat", "edit", "apply"}}
		replaced := false
		for i, value := range models {
			if item, ok := value.(map[string]any); ok && item["name"] == "Lingma Proxy" {
				models[i] = entry
				replaced = true
			}
		}
		if !replaced {
			models = append(models, entry)
		}
		data["models"] = models
		raw, err := yaml.Marshal(data)
		return raw, "在 Continue 选择 Lingma Proxy。YAML 注释和排版会重新生成，原文件已备份。", err
	}
	return nil, "", fmt.Errorf("未知客户端")
}

func (a *App) PreviewClientConfig(tool string) (ClientConfigPreview, error) {
	path, err := clientPath(tool)
	if err != nil {
		return ClientConfigPreview{}, err
	}
	before, exists, err := readClientFile(path)
	if err != nil {
		return ClientConfigPreview{}, err
	}
	a.mu.RLock()
	cfg := a.cfg
	a.mu.RUnlock()
	after, instruction, err := mergeClientConfig(tool, before, cfg)
	if err != nil {
		return ClientConfigPreview{}, err
	}
	fingerprint := digest(append(append([]byte(path+fmt.Sprint(exists)+"\x00"), before...), append([]byte("\x00"), after...)...))
	return ClientConfigPreview{tool, path, string(before), string(after), fingerprint, instruction, exists}, nil
}

func atomicClientWrite(path string, raw []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".lingma-config-*")
	if err != nil {
		return err
	}
	temp := file.Name()
	defer os.Remove(temp)
	if _, err = file.Write(raw); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(temp, path)
}

func (a *App) ApplyClientConfig(tool, fingerprint string) (string, error) {
	a.clientMu.Lock()
	defer a.clientMu.Unlock()
	preview, err := a.PreviewClientConfig(tool)
	if err != nil {
		return "", err
	}
	if fingerprint == "" || preview.Fingerprint != fingerprint {
		return "", fmt.Errorf("配置或代理设置已变化，请重新预览后再应用")
	}
	backup := clientBackup{tool, []byte(preview.Before), preview.Exists, digest([]byte(preview.After))}
	raw, err := json.Marshal(backup)
	if err != nil {
		return "", err
	}
	id := uuid.NewString()
	backupPath := filepath.Join(filepath.Dir(preview.Path), ".lingma-proxy-backups", id+".json")
	if err = atomicClientWrite(backupPath, raw); err != nil {
		return "", err
	}
	current, exists, readErr := readClientFile(preview.Path)
	if readErr != nil {
		return "", readErr
	}
	if exists != preview.Exists || string(current) != preview.Before {
		return "", fmt.Errorf("配置刚刚被其他程序修改，请重新预览")
	}
	if err = atomicClientWrite(preview.Path, []byte(preview.After)); err != nil {
		return "", err
	}
	return id, nil
}

func (a *App) RestoreClientConfig(tool, id string) error {
	a.clientMu.Lock()
	defer a.clientMu.Unlock()
	if _, err := uuid.Parse(id); err != nil {
		return fmt.Errorf("备份 ID 无效")
	}
	path, err := clientPath(tool)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(path), ".lingma-proxy-backups", id+".json"))
	if err != nil {
		return err
	}
	var backup clientBackup
	if err = json.Unmarshal(raw, &backup); err != nil {
		return err
	}
	if backup.Tool != tool {
		return fmt.Errorf("备份客户端不匹配")
	}
	current, _, err := readClientFile(path)
	if err != nil {
		return err
	}
	if digest(current) != backup.AppliedHash {
		return fmt.Errorf("客户端配置已有后续修改，为避免覆盖，请先手工检查备份")
	}
	if !backup.Existed {
		return os.Remove(path)
	}
	return atomicClientWrite(path, backup.Before)
}
