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
