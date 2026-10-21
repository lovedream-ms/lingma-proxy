package remote

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

func readDesktopUser(root string) ([]byte, bool, error) {
	state, err := os.ReadFile(filepath.Join(root, "Local State"))
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, true, fmt.Errorf("read Electron encryption configuration")
	}
	var config struct {
		OSCrypt struct {
			Key string `json:"encrypted_key"`
		} `json:"os_crypt"`
	}
	if json.Unmarshal(state, &config) != nil {
		return nil, true, fmt.Errorf("invalid Electron encryption configuration")
	}
	protected, err := base64.StdEncoding.DecodeString(config.OSCrypt.Key)
	if err != nil || !bytes.HasPrefix(protected, []byte("DPAPI")) || len(protected) <= 5 {
		return nil, true, fmt.Errorf("unsupported Electron encryption key format")
	}
	input := windows.DataBlob{Size: uint32(len(protected) - 5), Data: &protected[5]}
	var output windows.DataBlob
	if err := windows.CryptUnprotectData(&input, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &output); err != nil {
		return nil, true, fmt.Errorf("unlock current-user Electron encryption key")
	}
	defer windows.LocalFree(windows.Handle(uintptr(unsafe.Pointer(output.Data))))
	key := append([]byte(nil), unsafe.Slice(output.Data, int(output.Size))...)
	defer clear(key)
	sealed, err := readDesktopSecret(filepath.Join(root, "User", "globalStorage", "state.vscdb"))
	if err != nil {
		return nil, true, err
	}
	var envelope struct {
		Data []byte `json:"data"`
	}
	if json.Unmarshal(sealed, &envelope) != nil {
		return nil, true, fmt.Errorf("invalid Electron credential envelope")
	}
	return decryptDesktopSecret(key, envelope.Data)
}

func decryptDesktopSecret(key, sealed []byte) ([]byte, bool, error) {
	if len(sealed) < 3+12+16 || (!bytes.HasPrefix(sealed, []byte("v10")) && !bytes.HasPrefix(sealed, []byte("v11"))) {
		return nil, true, fmt.Errorf("unsupported Electron credential encryption")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, true, fmt.Errorf("invalid Electron encryption key size")
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, true, fmt.Errorf("initialize Electron credential decryption")
	}
	plain, err := aead.Open(nil, sealed[3:15], sealed[15:], nil)
	if err != nil {
		return nil, true, fmt.Errorf("unlock current-user Electron credential")
	}
	return plain, true, nil
}

// Windows ships SQLite as a system DLL. Read-only access avoids an external
// runtime, CGO, copying credentials, and modifying the active Electron database.
func readDesktopSecret(path string) ([]byte, error) {
	dll := windows.NewLazySystemDLL("winsqlite3.dll")
	if err := dll.Load(); err != nil {
		return nil, fmt.Errorf("Windows SQLite runtime unavailable")
	}
	filename, err := windows.BytePtrFromString(path)
	if err != nil {
		return nil, fmt.Errorf("invalid credential database path")
	}
	var database uintptr
	rc, _, _ := dll.NewProc("sqlite3_open_v2").Call(uintptr(unsafe.Pointer(filename)), uintptr(unsafe.Pointer(&database)), 1, 0)
	if database != 0 {
		defer dll.NewProc("sqlite3_close").Call(database)
	}
	if rc != 0 {
		return nil, fmt.Errorf("open current-user credential database read-only (SQLite %d)", rc)
	}
	query, _ := windows.BytePtrFromString("SELECT value FROM ItemTable WHERE key='secret://aicoding.auth.userInfo'")
	var statement uintptr
	rc, _, _ = dll.NewProc("sqlite3_prepare_v2").Call(database, uintptr(unsafe.Pointer(query)), ^uintptr(0), uintptr(unsafe.Pointer(&statement)), 0)
	if statement != 0 {
		defer dll.NewProc("sqlite3_finalize").Call(statement)
	}
	if rc != 0 {
		return nil, fmt.Errorf("read current-user credential schema (SQLite %d)", rc)
	}
	rc, _, _ = dll.NewProc("sqlite3_step").Call(statement)
	if rc != 100 {
		return nil, fmt.Errorf("current-user QoderCN credential is unavailable (SQLite %d)", rc)
	}
	n, _, _ := dll.NewProc("sqlite3_column_bytes").Call(statement, 0)
	ptr, _, _ := dll.NewProc("sqlite3_column_blob").Call(statement, 0)
	if ptr == 0 || n == 0 || n > 8*1024*1024 {
		return nil, fmt.Errorf("invalid current-user credential envelope size")
	}
	// Copy SQLite-owned memory through the OS API rather than converting a
	// foreign integer address into a Go pointer retained across calls.
	data := make([]byte, int(n))
	var copied uintptr
	if err := windows.ReadProcessMemory(windows.CurrentProcess(), ptr, &data[0], n, &copied); err != nil || copied != n {
		return nil, fmt.Errorf("copy current-user credential envelope")
	}
	return data, nil
}
