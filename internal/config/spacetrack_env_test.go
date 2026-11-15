package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testSTPassword = "s3cret-Pa55"

func writeDotEnv(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	return path
}

// clearSpaceTrackEnv убирает учётку из окружения на время теста.
func clearSpaceTrackEnv(t *testing.T) {
	t.Helper()
	t.Setenv(envSpaceTrackIdentity, "")
	t.Setenv(envSpaceTrackPassword, "")
}

func TestLoadSpaceTrackCredentials_FromDotEnv(t *testing.T) {
	clearSpaceTrackEnv(t)
	path := writeDotEnv(t, "# учётка\n"+
		"OTHER_KEY=ignored\n"+
		"\n"+
		"export SPACETRACK_IDENTITY=user@example.org\n"+
		`SPACETRACK_PASSWORD="`+testSTPassword+`"`+"\n")

	got := loadSpaceTrackCredentials(path)
	if !got.Enabled() {
		t.Fatal("credentials must be enabled when both keys present")
	}
	if got.Identity != "user@example.org" || got.Password != testSTPassword {
		t.Fatalf("got identity=%q, password match=%v", got.Identity, got.Password == testSTPassword)
	}
}

func TestLoadSpaceTrackCredentials_EnvOverridesDotEnv(t *testing.T) {
	t.Setenv(envSpaceTrackIdentity, "env@example.org")
	t.Setenv(envSpaceTrackPassword, "env-pass")
	path := writeDotEnv(t, "SPACETRACK_IDENTITY=file@example.org\nSPACETRACK_PASSWORD=file-pass\n")

	got := loadSpaceTrackCredentials(path)
	if got.Identity != "env@example.org" || got.Password != "env-pass" {
		t.Fatalf("got identity=%q, want values from environment", got.Identity)
	}
}

func TestLoadSpaceTrackCredentials_IncompletePairDisabled(t *testing.T) {
	clearSpaceTrackEnv(t)
	path := writeDotEnv(t, "SPACETRACK_IDENTITY=user@example.org\n")

	got := loadSpaceTrackCredentials(path)
	if got.Enabled() || got.Identity != "" || got.Password != "" {
		t.Fatalf("incomplete pair must give empty credentials, got identity=%q", got.Identity)
	}
}

func TestLoadSpaceTrackCredentials_MissingFileDisabled(t *testing.T) {
	clearSpaceTrackEnv(t)

	got := loadSpaceTrackCredentials(filepath.Join(t.TempDir(), "absent.env"))
	if got.Enabled() {
		t.Fatal("missing .env must leave credentials disabled")
	}
}

func TestSpaceTrackCredentials_NotSerializedOrLogged(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SpaceTrack = SpaceTrackCredentials{Identity: "user@example.org", Password: testSTPassword}

	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if bytes.Contains(raw, []byte(testSTPassword)) {
		t.Fatalf("config JSON leaks password: %s", raw)
	}

	if s := fmt.Sprintf("%v %+v", cfg.SpaceTrack, cfg.SpaceTrack); strings.Contains(s, testSTPassword) {
		t.Fatalf("fmt output leaks password: %s", s)
	}

	var buf bytes.Buffer
	slog.New(slog.NewTextHandler(&buf, nil)).Info("cfg", slog.Any("spacetrack", cfg.SpaceTrack))
	if bytes.Contains(buf.Bytes(), []byte(testSTPassword)) {
		t.Fatalf("slog output leaks password: %s", buf.String())
	}
}
