package remote

import (
	"strings"
	"testing"
)

func TestVerifyRemoteAuthenticationHeadersSurvive(t *testing.T) {
	c := New(Config{BaseURL: "http://127.0.0.1:8096/custom"})
	headers, err := c.headers(Credential{CosyKey: "test-key", EncryptUserInfo: "test-user", UserID: "user", MachineID: "machine"}, chatPath, "{}")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(headers["Authorization"], "Bearer COSY.") {
		t.Fatal("signed authorization header removed")
	}
	if got := sanitizeHeaders(map[string]string{"Authorization": "Bearer test\r\n", "X-Test": "a\nb"}); got["Authorization"] != "Bearer test" || got["X-Test"] != "ab" {
		t.Fatal("header cleanup changed protocol fields")
	}
}

func TestVerifyConfiguredRemoteURLs(t *testing.T) {
	c := New(Config{})
	for _, address := range []string{"http://127.0.0.1:8096/custom", "https://custom.example.test/api"} {
		if err := c.validateURL(address); err != nil {
			t.Fatal(err)
		}
	}
	for _, address := range []string{"file:///etc/passwd", "https:///missing-host", "invalid"} {
		if err := c.validateURL(address); err == nil {
			t.Fatalf("accepted %q", address)
		}
	}
}
