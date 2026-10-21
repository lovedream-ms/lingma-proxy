package remote

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyDesktopAuthenticationEnvelope(t *testing.T) {
	private, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	user := desktopUser{ID: "synthetic-user", Token: "synthetic-token", RefreshToken: "synthetic-refresh", OrgID: "synthetic-org", LoginSource: "dedicated_qodercn", PrivacyPolicyAgreed: true}
	key, info, err := encryptDesktopUserWithKey(user, &private.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	wrapped, _ := base64.StdEncoding.DecodeString(key)
	secret, err := rsa.DecryptPKCS1v15(rand.Reader, private, wrapped)
	if err != nil || len(secret) != 16 {
		t.Fatalf("invalid key envelope: %v", err)
	}
	sealed, _ := base64.StdEncoding.DecodeString(info)
	block, err := aes.NewCipher(secret)
	if err != nil {
		t.Fatal(err)
	}
	plain := make([]byte, len(sealed))
	cipher.NewCBCDecrypter(block, secret).CryptBlocks(plain, sealed)
	plain, err = unpadPKCS7(plain)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(plain, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["uid"] != user.ID || payload["security_oauth_token"] != user.Token || payload["organization_id"] != user.OrgID || payload["account_source"] != "qodercn_2_0" || payload["data_policy_agreed"] != true {
		t.Fatal("official login envelope fields were not retained")
	}
	key2, info2, err := encryptDesktopUserWithKey(user, &private.PublicKey)
	if err != nil || key2 == key || info2 == info {
		t.Fatal("login envelopes must use fresh random keys")
	}
}

func TestVerifyPortableDesktopCredentialRoutesCatalogAndChat(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("LINGMA_REMOTE_BASE_URL", "")
	check := func(r *http.Request) {
		if r.Header.Get("Cosy-Machinetoken") != "synthetic-machine-token" || r.Header.Get("Cosy-Organization-Id") != "synthetic-org" || r.Header.Get("Cosy-Clienttype") != "0" || r.Header.Get("Cosy-Version") != "1.34.0" {
			t.Error("modern machine/version/organization headers were lost")
		}
	}
	catalogCount, chatCount := 0, 0
	catalog := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		catalogCount++
		check(r)
		if r.Method != http.MethodGet || r.URL.Path != modelListPath {
			t.Error("wrong catalogue route")
		}
		fmt.Fprint(w, `{"app":[{"key":"new-route","displayName":"New Model","maxInputTokens":180000,"isVl":false,"contextConfig":{"200K":{"isDefault":true,"tokenCount":200000}}}]}`)
	}))
	defer catalog.Close()
	inference := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chatCount++
		check(r)
		if r.Method != http.MethodPost || r.URL.Path != chatPath {
			t.Error("wrong inference route")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["business"].(map[string]any)["version"] != "1.34.0" {
			t.Error("request body uses stale version")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"OK\"}}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":123,\"completion_tokens\":1}}\n\ndata: [DONE]\n\n")
	}))
	defer inference.Close()
	cred := Credential{CosyKey: "synthetic", EncryptUserInfo: "synthetic", UserID: "synthetic", MachineID: "synthetic", Desktop: &DesktopCredentialContext{BaseURL: catalog.URL, InferenceURL: inference.URL, Version: "1.34.0", MachineToken: "synthetic-machine-token", OrganizationID: "synthetic-org"}}
	path := filepath.Join(dir, "credentials.json")
	if err := SaveCredentialFile(cred, path); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadCredential(path)
	if err != nil || loaded.Desktop == nil || loaded.Desktop.InferenceURL != inference.URL {
		t.Fatal("portable routing metadata lost")
	}
	client := New(Config{AuthFile: path})
	models, err := client.ListModels(context.Background())
	if err != nil || len(models) != 1 || models[0].ContextWindow != 200000 || models[0].MaxInputTokens != 180000 {
		t.Fatalf("modern catalogue failed: %v", err)
	}
	result, err := client.Chat(context.Background(), ChatRequest{Model: "new-route", Prompt: "hello"}, nil)
	if err != nil || result.Text != "OK" || result.InputTokens != 123 || result.OutputTokens != 1 {
		t.Fatalf("modern inference failed: %v", err)
	}
	if catalogCount != 1 || chatCount != 1 {
		t.Fatalf("unexpected catalogue/chat requests: %d/%d", catalogCount, chatCount)
	}
	explicit := New(Config{BaseURL: "https://explicit.invalid", AuthFile: path, CosyVersion: "explicit-version"})
	if explicit.credentialBaseURL(loaded) != "https://explicit.invalid" || explicit.credentialVersion(loaded) != "explicit-version" {
		t.Fatal("desktop metadata overrides explicit configuration")
	}
}

func TestVerifyDesktopLoginConfigurationAndExpiry(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("machine_token.json", `{"id":"synthetic","token":"synthetic-machine","type":1,"code":"code","version":"1.34.0"}`)
	write("cache.json", `{"qoderCN20RegionConfig":{"preferredCenterNode":{"endpoint":"https://catalog.invalid"},"preferredInferenceNode":{"endpoint":"https://infer.invalid"}}}`)
	user := desktopUser{ID: "user", Token: "synthetic", ExpireTime: json.RawMessage(`"4102444800000"`), LoginSource: "qodercn"}
	cred, err := buildDesktopCredential(user, dir)
	if err != nil || cred.Desktop == nil || cred.Desktop.BaseURL != "https://catalog.invalid" || cred.Desktop.InferenceURL != "https://infer.invalid" || cred.Desktop.MachineType != "1" {
		t.Fatalf("login context not retained: %v", err)
	}
	user.ExpireTime = json.RawMessage(`"1"`)
	if _, err := buildDesktopCredential(user, dir); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatal("expired current login silently accepted")
	}
}
