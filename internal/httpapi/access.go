package httpapi

import (
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
)

func loopbackHost(host string) bool {
	host = strings.Trim(host, "[]")
	return strings.EqualFold(host, "localhost") || net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()
}

func (s *Server) validateListener() error {
	host, _, err := net.SplitHostPort(s.http.Addr)
	if err != nil {
		return err
	}
	if !loopbackHost(host) && strings.TrimSpace(s.svc.RuntimeConfig().APIKey) == "" {
		return fmt.Errorf("非本机监听必须设置代理访问密钥 api_key")
	}
	return nil
}

func (s *Server) ValidateListener() error { return s.validateListener() }

func sameKey(got, want string) bool {
	if want == "" || got == "" {
		return false
	}
	a, b := sha256.Sum256([]byte(got)), sha256.Sum256([]byte(want))
	return subtle.ConstantTimeCompare(a[:], b[:]) == 1
}

func (s *Server) withAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg := s.svc.RuntimeConfig()
		origin := r.Header.Get("Origin")
		if origin != "" {
			parsed, err := url.Parse(origin)
			allowed := err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host == r.Host && parsed.Path == ""
			for _, entry := range cfg.AllowedOrigins {
				if entry == origin {
					allowed = true
				}
			}
			if !allowed {
				writeOpenAIError(w, 403, "origin_not_allowed", "浏览器来源未授权")
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Add("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, x-api-key, anthropic-version")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		key := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if key == "" {
			key = r.Header.Get("x-api-key")
		}
		if isDebugInspectionPath(r.URL.Path) {
			host, _, _ := net.SplitHostPort(r.RemoteAddr)
			if cfg.AdminKey != "" {
				if !sameKey(key, cfg.AdminKey) {
					writeOpenAIError(w, 401, "authentication_error", "调试接口需要管理密钥")
					return
				}
			} else if !loopbackHost(host) || cfg.APIKey != "" && !sameKey(key, cfg.APIKey) {
				writeOpenAIError(w, 403, "debug_access_denied", "调试接口仅允许本机授权访问")
				return
			}
		} else if cfg.APIKey != "" && !sameKey(key, cfg.APIKey) {
			writeOpenAIError(w, 401, "authentication_error", "代理访问密钥无效")
			return
		}
		next.ServeHTTP(w, r)
	})
}
