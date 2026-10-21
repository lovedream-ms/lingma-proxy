package remote

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestVerifyWindowsReadOnlyDesktopLogin(t *testing.T) {
	root := t.TempDir()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	input := windows.DataBlob{Size: uint32(len(key)), Data: &key[0]}
	var output windows.DataBlob
	if err := windows.CryptProtectData(&input, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &output); err != nil {
		t.Fatal(err)
	}
	defer windows.LocalFree(windows.Handle(uintptr(unsafe.Pointer(output.Data))))
	protected := append([]byte("DPAPI"), unsafe.Slice(output.Data, int(output.Size))...)
	state, _ := json.Marshal(map[string]any{"os_crypt": map[string]any{"encrypted_key": base64.StdEncoding.EncodeToString(protected)}})
	if err := os.WriteFile(filepath.Join(root, "Local State"), state, 0600); err != nil {
		t.Fatal(err)
	}
	plain := []byte(`{"id":"synthetic-user","token":"synthetic-token","login_source":"qodercn","expireTime":"4102444800000"}`)
	block, _ := aes.NewCipher(key)
	aead, _ := cipher.NewGCM(block)
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	sealed := append(append([]byte("v10"), nonce...), aead.Seal(nil, nonce, plain, nil)...)
	buffer := make([]int, len(sealed))
	for i, value := range sealed {
		buffer[i] = int(value)
	}
	envelope, _ := json.Marshal(map[string]any{"type": "Buffer", "data": buffer})
	dbPath := filepath.Join(root, "User", "globalStorage", "state.vscdb")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0700); err != nil {
		t.Fatal(err)
	}
	dll := windows.NewLazySystemDLL("winsqlite3.dll")
	filename, _ := windows.BytePtrFromString(dbPath)
	var database uintptr
	rc, _, _ := dll.NewProc("sqlite3_open_v2").Call(uintptr(unsafe.Pointer(filename)), uintptr(unsafe.Pointer(&database)), 6, 0)
	if rc != 0 {
		t.Fatal("create synthetic credential database")
	}
	sql, _ := windows.BytePtrFromString("CREATE TABLE ItemTable(key TEXT,value TEXT); INSERT INTO ItemTable VALUES ('secret://aicoding.auth.userInfo','" + strings.ReplaceAll(string(envelope), "'", "''") + "');")
	rc, _, _ = dll.NewProc("sqlite3_exec").Call(database, uintptr(unsafe.Pointer(sql)), 0, 0, 0)
	dll.NewProc("sqlite3_close").Call(database)
	if rc != 0 {
		t.Fatal("populate synthetic credential database")
	}
	decoded, supported, err := readDesktopUser(root)
	if err != nil || !supported || string(decoded) != string(plain) {
		t.Fatalf("read-only encrypted credential roundtrip failed: %v", err)
	}
	cache := filepath.Join(root, "SharedClientCache", "cache")
	if err := os.MkdirAll(cache, 0700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"machine_token.json": `{"id":"synthetic-machine","token":"synthetic-machine-token","version":"1.34.0"}`,
		"cache.json":         `{"qoderCN20RegionConfig":{"preferredCenterNode":{"endpoint":"https://catalog.invalid"}}}`,
	} {
		if err := os.WriteFile(filepath.Join(cache, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("LINGMA_CACHE_DIR", root)
	credential, err := LoadCredential("")
	if err != nil || credential.Desktop == nil || credential.UserID != "synthetic-user" || credential.Source != dbPath {
		t.Fatalf("automatic current desktop login selection failed: %v", err)
	}
	if warning := CredentialCompatibilityWarning(""); warning != "" {
		t.Fatalf("working current login was incorrectly warned: %s", warning)
	}
	if err := os.WriteFile(filepath.Join(cache, "status.json"), []byte(`{"logged_in":false}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, found, err := loadDesktopCredential(); found || err != nil {
		t.Fatal("signed-out desktop profile was selected")
	}
	sealed[len(sealed)-1] ^= 1
	if _, _, err := decryptDesktopSecret(key, sealed); err == nil {
		t.Fatal("tampered encrypted login accepted")
	}
	if _, _, err := decryptDesktopSecret(key, []byte("v10")); err == nil {
		t.Fatal("truncated encrypted login accepted")
	}
}
