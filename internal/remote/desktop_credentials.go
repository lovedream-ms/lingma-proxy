package remote

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// This is the public login-envelope key shipped by the official client.
const desktopAuthPublicKey = `-----BEGIN PUBLIC KEY-----
MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQDA8iMH5c02LilrsERw9t6Pv5Nc
4k6Pz1EaDicBMpdpxKduSZu5OANqUq8er4GM95omAGIOPOh+Nx0spthYA2BqGz+l
6HRkPJ7S236FZz73In/KVuLnwI8JJ2CbuJap8kvheCCZpmAWpb/cPx/3Vr/J6I17
XcW+ML9FoCI6AOvOzwIDAQAB
-----END PUBLIC KEY-----`

// DesktopCredentialContext travels with exported credentials so Linux/headless
// deployments use the same gateway and machine identity as the desktop login.
type DesktopCredentialContext struct {
	BaseURL        string `json:"base_url"`
	InferenceURL   string `json:"inference_url,omitempty"`
	Version        string `json:"version,omitempty"`
	MachineToken   string `json:"machine_token"`
	MachineType    string `json:"machine_type,omitempty"`
	MachineCode    string `json:"machine_code,omitempty"`
	MachineOS      string `json:"machine_os,omitempty"`
	OrganizationID string `json:"organization_id,omitempty"`
	DataPolicy     string `json:"data_policy,omitempty"`
}

type desktopUser struct {
	ID                  string          `json:"id"`
	AccountID           string          `json:"accountId"`
	Name                string          `json:"name"`
	Token               string          `json:"token"`
	RefreshToken        string          `json:"refreshToken"`
	ExpireTime          json.RawMessage `json:"expireTime"`
	OrgID               string          `json:"orgId"`
	OrgName             string          `json:"orgName"`
	YxUID               string          `json:"yxUid"`
	UserType            string          `json:"userType"`
	LoginSource         string          `json:"login_source"`
	PrivacyPolicyAgreed bool            `json:"privacyPolicyAgreed"`
}

func loadDesktopCredential() (Credential, bool, error) {
	for _, dir := range candidateLingmaCacheDirs() {
		root := dir
		if strings.EqualFold(filepath.Base(root), "SharedClientCache") {
			root = filepath.Dir(root)
		}
		path := filepath.Join(root, "User", "globalStorage", "state.vscdb")
		if _, err := os.Stat(path); err != nil {
			continue
		}
		var status struct {
			LoggedIn *bool `json:"logged_in"`
		}
		if data, err := os.ReadFile(filepath.Join(root, "SharedClientCache", "cache", "status.json")); err == nil {
			_ = json.Unmarshal(data, &status)
		}
		if status.LoggedIn != nil && !*status.LoggedIn {
			continue
		}
		plain, supported, err := readDesktopUser(root)
		if !supported {
			continue
		}
		if err != nil {
			// An installed but unused profile must not mask a valid legacy login.
			if status.LoggedIn == nil {
				continue
			}
			return Credential{}, true, fmt.Errorf("read current QoderCN login: %w", err)
		}
		var user desktopUser
		if json.Unmarshal(plain, &user) != nil || user.ID == "" || user.Token == "" {
			return Credential{}, true, fmt.Errorf("current QoderCN login is unavailable; sign in again")
		}
		// Leave legacy/international accounts to their existing importer.
		if user.LoginSource != "qodercn" && user.LoginSource != "dedicated_qodercn" {
			continue
		}
		cred, err := buildDesktopCredential(user, filepath.Join(root, "SharedClientCache", "cache"))
		if err != nil {
			return Credential{}, true, err
		}
		cred.Source = path
		return cred, true, nil
	}
	return Credential{}, false, nil
}

func buildDesktopCredential(user desktopUser, cacheDir string) (Credential, error) {
	var machine struct {
		ID      string `json:"id"`
		Token   string `json:"token"`
		Type    any    `json:"type"`
		Code    any    `json:"code"`
		OS      string `json:"os"`
		Version string `json:"version"`
	}
	if data, err := os.ReadFile(filepath.Join(cacheDir, "machine_token.json")); err != nil || json.Unmarshal(data, &machine) != nil || machine.ID == "" || machine.Token == "" {
		return Credential{}, fmt.Errorf("current QoderCN machine identity is unavailable")
	}
	var settings struct {
		Region struct {
			Preferred struct {
				Endpoint string `json:"endpoint"`
			} `json:"preferredCenterNode"`
			Inference struct {
				Endpoint string `json:"endpoint"`
			} `json:"preferredInferenceNode"`
		} `json:"qoderCN20RegionConfig"`
	}
	data, err := os.ReadFile(filepath.Join(cacheDir, "cache.json"))
	if err != nil || json.Unmarshal(data, &settings) != nil || settings.Region.Preferred.Endpoint == "" {
		return Credential{}, fmt.Errorf("current QoderCN inference gateway is unavailable")
	}
	version := machine.Version
	var status struct {
		Version string `json:"version"`
	}
	if data, err := os.ReadFile(filepath.Join(cacheDir, "status.json")); err == nil && json.Unmarshal(data, &status) == nil && status.Version != "" {
		version = status.Version
	}
	key, info, err := encryptDesktopUser(user)
	if err != nil {
		return Credential{}, err
	}
	var expires any
	_ = json.Unmarshal(user.ExpireTime, &expires)
	text := func(value any) string {
		if value == nil {
			return ""
		}
		return fmt.Sprint(value)
	}
	policy := "DISAGREE"
	if user.PrivacyPolicyAgreed {
		policy = "AGREE"
	}
	cred := Credential{CosyKey: key, EncryptUserInfo: info, UserID: user.ID, MachineID: machine.ID, TokenExpireTime: parseExpireAny(expires), Desktop: &DesktopCredentialContext{
		BaseURL: strings.TrimRight(settings.Region.Preferred.Endpoint, "/"), Version: version, MachineToken: machine.Token, MachineType: text(machine.Type), MachineCode: text(machine.Code), MachineOS: machine.OS, OrganizationID: user.OrgID, DataPolicy: policy,
		InferenceURL: strings.TrimRight(settings.Region.Inference.Endpoint, "/"),
	}}
	if IsExpired(cred, 0) {
		return Credential{}, fmt.Errorf("current QoderCN login has expired; open QoderCN to refresh the login")
	}
	return cred, validateCredential(cred)
}

// Match SaveUserInfo: PKCS#1 v1.5 protects a random 16-byte ASCII key;
// AES-CBC uses that key as IV and PKCS#7 padding for the login envelope.
func encryptDesktopUser(user desktopUser) (string, string, error) {
	block, _ := pem.Decode([]byte(desktopAuthPublicKey))
	if block == nil {
		return "", "", fmt.Errorf("invalid desktop authentication public key")
	}
	public, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return "", "", fmt.Errorf("invalid desktop authentication public key")
	}
	return encryptDesktopUserWithKey(user, public.(*rsa.PublicKey))
}

func encryptDesktopUserWithKey(user desktopUser, public *rsa.PublicKey) (string, string, error) {
	key := []byte(strings.ReplaceAll(newUUID(), "-", "")[:16])
	encryptedKey, err := rsa.EncryptPKCS1v15(rand.Reader, public, key)
	if err != nil {
		return "", "", fmt.Errorf("encrypt desktop authentication key: %w", err)
	}
	payload := map[string]any{
		"name": user.Name, "aid": valueOr(user.AccountID, user.ID), "uid": user.ID, "yx_uid": user.YxUID, "organization_id": user.OrgID, "organization_name": user.OrgName,
		"staffId": "", "avatar_url": "", "security_oauth_token": user.Token, "refresh_token": user.RefreshToken, "expire_time": 0, "key": "", "encrypt_user_info": "",
		"user_source_channel": "", "account_source": "qodercn_2_0", "login_method": "2.0", "user_type": user.UserType, "data_policy_agreed": user.PrivacyPolicyAgreed,
		"email": "", "is_data_policy_modifiable": false, "is_quota_exceeded": false, "organization_tags": nil,
	}
	plain, err := json.Marshal(payload)
	if err != nil {
		return "", "", fmt.Errorf("encode desktop authentication envelope")
	}
	pad := aes.BlockSize - len(plain)%aes.BlockSize
	for i := 0; i < pad; i++ {
		plain = append(plain, byte(pad))
	}
	algorithm, err := aes.NewCipher(key)
	if err != nil {
		return "", "", err
	}
	encrypted := make([]byte, len(plain))
	cipher.NewCBCEncrypter(algorithm, key).CryptBlocks(encrypted, plain)
	return base64.StdEncoding.EncodeToString(encryptedKey), base64.StdEncoding.EncodeToString(encrypted), nil
}
