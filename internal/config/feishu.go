package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const feishuFile = "feishu.json"

type FeishuConnection struct {
	AppID       string `json:"app_id"`
	AppSecret   string `json:"app_secret"`
	OwnerOpenID string `json:"owner_open_id"`
	ChatID      string `json:"chat_id,omitempty"`
}

func (c FeishuConnection) validate() error {
	if c.AppID == "" || c.AppSecret == "" || c.OwnerOpenID == "" ||
		strings.ContainsAny(c.AppID+c.AppSecret+c.OwnerOpenID+c.ChatID, "\r\n\x00") {
		return errors.New("invalid Feishu connection")
	}
	return nil
}

func FeishuPath() (string, error) {
	dir, err := Directory()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, feishuFile), nil
}

func LoadFeishu() (FeishuConnection, error) {
	path, err := FeishuPath()
	if err != nil {
		return FeishuConnection{}, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return FeishuConnection{}, errors.New("Feishu is not connected; run 'alivyn channel feishu add'")
	}
	if err != nil {
		return FeishuConnection{}, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return FeishuConnection{}, errors.New("Feishu credential file must be regular with 0600 permissions")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return FeishuConnection{}, err
	}
	var c FeishuConnection
	if err := json.Unmarshal(data, &c); err != nil {
		return FeishuConnection{}, errors.New("invalid Feishu credential file")
	}
	return c, c.validate()
}

// SaveFeishu writes a private file atomically. It never prints credentials.
func SaveFeishu(c FeishuConnection) error {
	if err := c.validate(); err != nil {
		return err
	}
	path, err := FeishuPath()
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
			return errors.New("Feishu credential file must be regular with 0600 permissions")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	file, err := os.CreateTemp(dir, ".feishu-*")
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
