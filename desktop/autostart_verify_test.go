package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"lingma-ipc-proxy/internal/service"
)

type verifyAutostarter struct {
	enabled      bool
	enableCalls  int
	disableCalls int
	err          error
}

func (s *verifyAutostarter) Enable() error {
	s.enableCalls++
	if s.err == nil {
		s.enabled = true
	}
	return s.err
}

func (s *verifyAutostarter) Disable() error {
	s.disableCalls++
	s.enabled = false
	return s.err
}

func (s *verifyAutostarter) IsEnabled() (bool, error) { return s.enabled, s.err }

func TestVerifyAutoStartRefreshesPathAndDisables(t *testing.T) {
	starter := &verifyAutostarter{enabled: true}
	app := &App{autostarter: starter}
	if err := app.applyAutoStart(true); err != nil || starter.enableCalls != 1 {
		t.Fatalf("refresh = %v, calls %d", err, starter.enableCalls)
	}
	if err := app.applyAutoStart(false); err != nil || starter.enabled {
		t.Fatalf("disable = %v, enabled %v", err, starter.enabled)
	}
	if err := app.applyAutoStart(false); err != nil || starter.disableCalls != 1 {
		t.Fatalf("repeat disable = %v, calls %d", err, starter.disableCalls)
	}
}

func TestVerifyAutoStartFailurePreservesConfig(t *testing.T) {
	starter := &verifyAutostarter{err: errors.New("permission denied")}
	app := &App{autostarter: starter, cfg: service.Config{Port: 8095}}
	err := app.UpdateConfig(service.Config{AutoStartOnBoot: true, Port: 8096})
	if err == nil || app.cfg.AutoStartOnBoot || app.cfg.Port != 8095 {
		t.Fatalf("failed registration changed config: error=%v, config=%+v", err, app.cfg)
	}
}

func TestVerifyAutoStartRollsBackWhenConfigSaveFails(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	if err := os.WriteFile(filepath.Join(dir, ".config"), []byte("block directory creation"), 0600); err != nil {
		t.Fatal(err)
	}
	starter := &verifyAutostarter{}
	app := &App{autostarter: starter, cfg: service.Config{Port: 8095}}
	err := app.UpdateConfig(service.Config{AutoStartOnBoot: true, Port: 8096})
	if err == nil || starter.enabled || starter.disableCalls != 1 || app.cfg.Port != 8095 {
		t.Fatalf("save rollback failed: error=%v, enabled=%v, disableCalls=%d", err, starter.enabled, starter.disableCalls)
	}
}
