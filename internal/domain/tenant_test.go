package domain

import "testing"

func TestIsValidFeishuRedirectURI(t *testing.T) {
	for value, want := range map[string]bool{
		"https://forge.example.com/login/feishu/callback": true,
		"http://127.0.0.1:5173/login/feishu/callback":     true,
		"":                       false,
		"/login/feishu/callback": false,
		"https://forge.example.com/login/feishu/callback/":          false,
		"https://forge.example.com/login/feishu/callback?next=/":    false,
		"https://forge.example.com/login/feishu/callback#x":         false,
		"https://user:pass@forge.example.com/login/feishu/callback": false,
		"javascript://forge.example.com/login/feishu/callback":      false,
		"https:///login/feishu/callback":                            false,
	} {
		if got := IsValidFeishuRedirectURI(value); got != want {
			t.Fatalf("IsValidFeishuRedirectURI(%q) = %v, want %v", value, got, want)
		}
	}
}
