package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSharedFeishuTokenStaysInPrivateFile(t *testing.T) {
	t.Setenv("ALIVYN_HOME", t.TempDir())
	c := SharedFeishu{
		RelayURL: "https://relay.example.org", Token: "abcdefghijklmnopqrstuvwxyz0123456789",
		BotURL: "https://applink.feishu.cn/example", Code: "AAAAAA-BBBBBB", ExpiresAt: time.Now().Add(time.Minute),
	}
	if err := SaveSharedFeishu(c); err != nil {
		t.Fatal(err)
	}
	path, err := SharedFeishuPath()
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("shared token file is not private: %v, %v", info, err)
	}
	loaded, err := LoadSharedFeishu()
	if err != nil || loaded.Token != c.Token {
		t.Fatalf("could not reload private token: %v", err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSharedFeishu(); err == nil {
		t.Fatal("world-readable token file was accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(t.TempDir(), "other.json"), path); err != nil {
		t.Fatal(err)
	}
	if err := SaveSharedFeishu(c); err == nil {
		t.Fatal("symlink credential target was accepted")
	}
}
