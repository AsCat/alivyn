package config

import (
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type SharedFeishu struct {
	RelayURL  string    `json:"relay_url"`
	Token     string    `json:"token"`
	Code      string    `json:"code,omitempty"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
	BotURL    string    `json:"bot_url"`
}

func SharedFeishuPath() (string, error) {
	dir, err := Directory()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "shared-feishu.json"), nil
}

func ValidateRelayURL(value string) error {
	u, err := url.Parse(value)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" {
		return errors.New("relay URL must be a base HTTP(S) URL")
	}
	if u.Scheme == "https" {
		return nil
	}
	if u.Scheme != "http" {
		return errors.New("relay URL must use HTTPS")
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	if strings.EqualFold(host, "localhost") || ip != nil && ip.IsLoopback() {
		return nil
	}
	return errors.New("plain HTTP is only allowed for a loopback relay")
}

func (c SharedFeishu) validate() error {
	if err := ValidateRelayURL(c.RelayURL); err != nil {
		return err
	}
	bot, err := url.Parse(c.BotURL)
	if err != nil || bot.Scheme != "https" || bot.Host == "" || bot.User != nil {
		return errors.New("invalid Feishu bot link")
	}
	if len(c.Token) < 32 || len(c.Token) > 128 || strings.IndexFunc(c.Token, func(r rune) bool { return r <= ' ' || r == 127 }) >= 0 {
		return errors.New("invalid relay token")
	}
	return nil
}

func LoadSharedFeishu() (SharedFeishu, error) {
	path, err := SharedFeishuPath()
	if err != nil {
		return SharedFeishu{}, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return SharedFeishu{}, errors.New("shared Feishu bot is not paired; run 'alivyn channel feishu connect'")
	}
	if err != nil {
		return SharedFeishu{}, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return SharedFeishu{}, errors.New("shared Feishu credential file must be regular with 0600 permissions")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return SharedFeishu{}, err
	}
	var c SharedFeishu
	if err := json.Unmarshal(data, &c); err != nil {
		return SharedFeishu{}, errors.New("invalid shared Feishu credential file")
	}
	return c, c.validate()
}

func SaveSharedFeishu(c SharedFeishu) error {
	if err := c.validate(); err != nil {
		return err
	}
	path, err := SharedFeishuPath()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return errors.New("shared Feishu credential file must be regular with 0600 permissions")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	file, err := os.CreateTemp(dir, ".shared-feishu-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err := file.Chmod(0600); err != nil {
		file.Close()
		return err
	}
	if err := json.NewEncoder(file).Encode(c); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
