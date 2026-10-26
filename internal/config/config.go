package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	_ "time/tzdata"
	"unicode"

	"github.com/AsCat/alivyn/internal/persona"
)

const DefaultDeepSeekURL = "https://api.deepseek.com"
const DefaultDeepSeekModel = "deepseek-flash"

type Config struct {
	SchemaVersion int    `json:"schema_version"`
	Name          string `json:"name"`
	Language      string `json:"language"`
	Style         string `json:"style,omitempty"`
	PersonaPack   string `json:"persona_pack,omitempty"`
	Background    string `json:"background,omitempty"`
	Routine       string `json:"routine,omitempty"`
	WorldPack     string `json:"world_pack,omitempty"`
	Engine        string `json:"engine,omitempty"`
	Provider      string `json:"provider"`
	BaseURL       string `json:"base_url"`
	Model         string `json:"model"`
	Timezone      string `json:"timezone,omitempty"`
}

func Directory() (string, error) {
	if value := os.Getenv("ALIVYN_HOME"); value != "" {
		return filepath.Abs(value)
	}
	root, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "alivyn"), nil
}

func Path() (string, error) {
	dir, err := Directory()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

func (c Config) Validate() error {
	if c.SchemaVersion != 1 {
		return errors.New("unsupported config schema")
	}
	if strings.TrimSpace(c.Name) == "" || strings.IndexFunc(c.Name, unicode.IsControl) >= 0 {
		return errors.New("name must be nonempty and single-line")
	}
	if c.Language != "en" && c.Language != "zh-CN" {
		return errors.New("language must be en or zh-CN")
	}
	if c.Style != "" && c.Style != "natural" && c.Style != "romantic" && c.Style != "companion" {
		return errors.New("style must be natural, romantic, or companion")
	}
	if _, ok := persona.Get(c.EffectivePersonaPack()); !ok {
		return errors.New("unknown persona pack")
	}
	if len([]rune(c.Background)) > 500 || strings.IndexFunc(c.Background, unicode.IsControl) >= 0 {
		return errors.New("background must be single-line and at most 500 characters")
	}
	if len([]rune(c.Routine)) > 500 || strings.IndexFunc(c.Routine, unicode.IsControl) >= 0 {
		return errors.New("routine must be single-line and at most 500 characters")
	}
	if c.WorldPack != "" && c.WorldPack != "tidehaven" && c.WorldPack != "none" {
		return errors.New("world pack must be tidehaven or none")
	}
	if c.Engine != "" && c.Engine != "legacy" && c.Engine != "actor_v1" && c.Engine != "character_v2" {
		return errors.New("engine must be legacy, actor_v1 or character_v2")
	}
	if c.Provider != "deepseek" && c.Provider != "custom" {
		return errors.New("provider must be deepseek or custom")
	}
	if c.Provider == "deepseek" && strings.TrimRight(c.BaseURL, "/") != DefaultDeepSeekURL {
		return errors.New("DeepSeek provider must use its official endpoint; use custom for other endpoints")
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("base URL must be an absolute HTTP(S) URL without credentials, query, or fragment")
	}
	if u.Scheme == "http" {
		host := u.Hostname()
		ip := net.ParseIP(host)
		if !strings.EqualFold(host, "localhost") && (ip == nil || !ip.IsLoopback()) {
			return errors.New("plain HTTP is only allowed for loopback model servers")
		}
	}
	if strings.TrimSpace(c.Model) == "" || strings.IndexFunc(c.Model, unicode.IsControl) >= 0 {
		return errors.New("model must be nonempty and single-line")
	}
	if _, err := c.Location(); err != nil {
		return fmt.Errorf("invalid timezone: %w", err)
	}
	return nil
}

func (c Config) EffectiveStyle() string {
	if c.Style == "" {
		return "natural"
	}
	return c.Style
}

func (c Config) EffectivePersonaPack() string {
	if c.PersonaPack == "" {
		return "natural"
	}
	return c.PersonaPack
}

func (c Config) EffectiveWorldPack() string {
	if c.WorldPack == "" {
		return "tidehaven"
	}
	return c.WorldPack
}

func (c Config) EffectiveEngine() string {
	if c.Engine == "" {
		return "legacy"
	}
	return c.Engine
}

func (c Config) UsesGoalWorld() bool {
	return c.EffectiveEngine() == "actor_v1" || c.EffectiveEngine() == "character_v2"
}

func SaveEngine(engine string) error {
	c, err := Load()
	if err != nil {
		return err
	}
	c.Engine = engine
	return saveExisting(c)
}

func SaveWorldPack(pack string) error {
	c, err := Load()
	if err != nil {
		return err
	}
	c.WorldPack = pack
	return saveExisting(c)
}

func (c Config) EffectiveBackground() string {
	if selected, ok := persona.Get(c.EffectivePersonaPack()); ok && selected.Standalone {
		return persona.Text(selected.ID, c.Language)
	}
	base := strings.TrimSpace(c.Background)
	if base == "" {
		if c.Language == "zh-CN" {
			base = "慢热。对陌生人礼貌但有所保留，不会刚认识就亲昵；熟悉后会表达好奇，偶尔开玩笑。讨厌空话，重视兑现承诺。"
		} else {
			base = "Slow to trust. Polite but reserved with strangers; curious and occasionally playful as familiarity grows. Dislikes empty promises."
		}
	}
	if extra := persona.Text(c.EffectivePersonaPack(), c.Language); extra != "" {
		return base + "\n\n" + extra
	}
	return base
}

// SavePersonaPack keeps existing history and refuses to cross the boundary
// between Alivyn and a standalone guest once a history database exists.
func SavePersonaPack(id string) error {
	c, err := Load()
	if err != nil {
		return err
	}
	current, _ := persona.Get(c.EffectivePersonaPack())
	next, ok := persona.Get(id)
	if !ok {
		return errors.New("unknown persona pack")
	}
	if current.Standalone != next.Standalone {
		dir, err := Directory()
		if err != nil {
			return err
		}
		if _, err := os.Stat(filepath.Join(dir, "alivyn.db")); err == nil {
			return errors.New("guest characters require a separate ALIVYN_HOME so their memories do not mix")
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	c.PersonaPack = id
	return saveExisting(c)
}

// SaveStyle replaces only the public persona setting, preserving existing
// identity and model configuration. The old file remains intact on failure.
func SaveStyle(style string) error {
	c, err := Load()
	if err != nil {
		return err
	}
	c.Style = style
	return saveExisting(c)
}

func SaveBackground(background string) error {
	c, err := Load()
	if err != nil {
		return err
	}
	c.Background = background
	return saveExisting(c)
}

func SaveRoutine(routine string) error {
	c, err := Load()
	if err != nil {
		return err
	}
	c.Routine = routine
	return saveExisting(c)
}

func saveExisting(c Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	path, err := Path()
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".config-*")
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

// Location accepts old configs without a timezone and keeps their local-time behavior.
func (c Config) Location() (*time.Location, error) {
	if c.Timezone == "" || c.Timezone == "Local" {
		return time.Local, nil
	}
	return time.LoadLocation(c.Timezone)
}

func Load() (Config, error) {
	path, err := Path()
	if err != nil {
		return Config{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, errors.New("not initialized; run 'alivyn init'")
	}
	if err != nil {
		return Config{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return Config{}, err
	}
	if info.Mode().Perm()&0077 != 0 {
		return Config{}, errors.New("config permissions are too open; expected 0600")
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return Config{}, fmt.Errorf("invalid config: %w", err)
	}
	return c, c.Validate()
}

func SaveNew(c Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if err := os.Chmod(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		return errors.New("already initialized; existing identity was left untouched")
	}
	if err != nil {
		return err
	}
	defer file.Close()
	return json.NewEncoder(file).Encode(c)
}
