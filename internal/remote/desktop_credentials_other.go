//go:build !windows

package remote

// Exported desktop COSY credentials work on every platform. Reading a local
// Electron login requires the platform's keychain; never guess a decryption key.
func readDesktopUser(root string) ([]byte, bool, error) { return nil, false, nil }
