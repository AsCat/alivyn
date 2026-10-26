package config

import "testing"

func TestRemoteModelMustUseHTTPS(t *testing.T) {
	base := Config{SchemaVersion: 1, Name: "Ari", Language: "en", Provider: "custom", Model: "example"}
	for _, tc := range []struct {
		url  string
		want bool
	}{
		{"https://models.example/v1", true},
		{"http://127.0.0.1:8080/v1", true},
		{"http://[::1]:8080/v1", true},
		{"http://models.example/v1", false},
		{"https://key:secret@models.example/v1", false},
	} {
		base.BaseURL = tc.url
		if got := base.Validate() == nil; got != tc.want {
			t.Errorf("%s: valid=%v, want %v", tc.url, got, tc.want)
		}
	}
}

func TestTimezoneValidation(t *testing.T) {
	c := Config{SchemaVersion: 1, Name: "Ari", Language: "en", Provider: "custom", BaseURL: "http://localhost:1234/v1", Model: "example", Timezone: "Asia/Shanghai"}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.Timezone = "Not/A_Real_Zone"
	if err := c.Validate(); err == nil {
		t.Fatal("invalid timezone was accepted")
	}
}

func TestDeepSeekEnvironmentKeyCannotUseCustomEndpoint(t *testing.T) {
	c := Config{SchemaVersion: 1, Name: "Ari", Language: "en", Provider: "deepseek", BaseURL: "https://models.example/v1", Model: DefaultDeepSeekModel}
	if err := c.Validate(); err == nil {
		t.Fatal("DeepSeek provider accepted an unrelated endpoint")
	}
}

func TestPersonaStyleUpgradePreservesModelSettings(t *testing.T) {
	t.Setenv("ALIVYN_HOME", t.TempDir())
	original := Config{SchemaVersion: 1, Name: "意续", Language: "zh-CN", Provider: "deepseek", BaseURL: DefaultDeepSeekURL, Model: DefaultDeepSeekModel, Timezone: "Asia/Shanghai"}
	if err := SaveNew(original); err != nil {
		t.Fatal(err)
	}
	if err := SaveStyle("natural"); err != nil {
		t.Fatal(err)
	}
	updated, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if updated.Style != "natural" || updated.Provider != original.Provider || updated.BaseURL != original.BaseURL || updated.Model != original.Model || updated.Name != original.Name {
		t.Fatalf("persona update altered identity or model: %+v", updated)
	}
	if err := SaveStyle("invalid"); err == nil {
		t.Fatal("invalid persona style accepted")
	}
	if err := SaveBackground("慢热，先观察对方"); err != nil {
		t.Fatal(err)
	}
	updated, err = Load()
	if err != nil || updated.Background != "慢热，先观察对方" || updated.Model != original.Model {
		t.Fatalf("background update altered config: %+v %v", updated, err)
	}
	if err := SaveBackground("\nunsafe"); err == nil {
		t.Fatal("multiline background accepted")
	}
	unchanged, err := Load()
	if err != nil || unchanged.Style != "natural" {
		t.Fatalf("invalid update changed config: %+v %v", unchanged, err)
	}
}

func TestPersonaPackChangesTemperamentWithoutReplacingIdentity(t *testing.T) {
	t.Setenv("ALIVYN_HOME", t.TempDir())
	original := Config{SchemaVersion: 1, Name: "意续", Language: "zh-CN", Provider: "deepseek", BaseURL: DefaultDeepSeekURL, Model: DefaultDeepSeekModel, Timezone: "Asia/Shanghai"}
	if err := SaveNew(original); err != nil {
		t.Fatal(err)
	}
	if err := SavePersonaPack("vivid"); err != nil {
		t.Fatal(err)
	}
	changed, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if changed.Name != original.Name || changed.Provider != original.Provider || changed.Model != original.Model || changed.Timezone != original.Timezone || changed.EffectivePersonaPack() != "vivid" {
		t.Fatalf("pack replaced identity or model: %+v", changed)
	}
	if err := SavePersonaPack("unknown"); err == nil {
		t.Fatal("unknown pack accepted")
	}
	unchanged, err := Load()
	if err != nil || unchanged.EffectivePersonaPack() != "vivid" {
		t.Fatalf("failed update changed pack: %+v %v", unchanged, err)
	}
	if err := SavePersonaPack("natural"); err != nil {
		t.Fatal(err)
	}
	if reset, err := Load(); err != nil || reset.EffectivePersonaPack() != "natural" {
		t.Fatalf("cannot return to original temperament: %+v %v", reset, err)
	}
}
