package domain

import (
	"strings"
	"testing"
)

func TestIsValidRuntimeVersion(t *testing.T) {
	for _, value := range []string{"1", "1.2.0", "android-725_1.2.0", strings.Repeat("a", 64)} {
		if !IsValidRuntimeVersion(value) {
			t.Fatalf("expected %q to be valid", value)
		}
	}
	for _, value := range []string{"", " ", "1.0 beta", "1/2", "版本", "1.0+build", strings.Repeat("a", 65)} {
		if IsValidRuntimeVersion(value) {
			t.Fatalf("expected %q to be invalid", value)
		}
	}
}

func TestIsValidPlatform(t *testing.T) {
	for _, platform := range []string{"android", "ios", "harmonyos"} {
		if !IsValidPlatform(platform) {
			t.Fatalf("expected %q to be valid", platform)
		}
	}
	for _, platform := range []string{"", "Android", "iOS", "web", "harmony", "HarmonyOS"} {
		if IsValidPlatform(platform) {
			t.Fatalf("expected %q to be invalid", platform)
		}
	}
}

func TestIsValidBuildVariableKey(t *testing.T) {
	if !IsValidBuildVariableKey("API_HOST") || !IsValidBuildVariableKey("A") {
		t.Fatal("expected upper snake case keys to be valid")
	}
	for _, key := range []string{"", "api_host", "1KEY", "KEY-NAME", "A" + strings.Repeat("B", 64)} {
		if IsValidBuildVariableKey(key) {
			t.Fatalf("expected %q to be invalid", key)
		}
	}
}
