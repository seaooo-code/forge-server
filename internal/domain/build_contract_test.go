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

func TestIsValidCapabilities(t *testing.T) {
	if !IsValidCapabilities(Capabilities{"commonApi": 3, "ui.kit_v2": 0}) || !IsValidCapabilities(nil) {
		t.Fatal("expected capabilities to be valid")
	}
	tooMany := Capabilities{}
	for i := range MaxCapabilities + 1 {
		tooMany["cap"+strings.Repeat("x", i)] = 1
	}
	for name, capabilities := range map[string]Capabilities{
		"negative level":   {"commonApi": -1},
		"level too large":  {"commonApi": MaxCapabilityLevel + 1},
		"digit first":      {"1api": 1},
		"space in name":    {"common api": 1},
		"name too long":    {strings.Repeat("a", 65): 1},
		"too many entries": tooMany,
	} {
		if IsValidCapabilities(capabilities) {
			t.Fatalf("%s: expected %v to be invalid", name, capabilities)
		}
	}
}

func TestUnmetCapabilities(t *testing.T) {
	shortfalls := UnmetCapabilities(Capabilities{"commonApi": 3, "bridge": 1, "ui": 2}, Capabilities{"commonApi": 2, "ui": 5})
	if len(shortfalls) != 2 || shortfalls[0].Name != "bridge" || shortfalls[0].Provided != nil ||
		shortfalls[1].Name != "commonApi" || shortfalls[1].Required != 3 || shortfalls[1].Provided == nil || *shortfalls[1].Provided != 2 {
		t.Fatalf("unexpected shortfalls: %+v", shortfalls)
	}
	if got := UnmetCapabilities(Capabilities{"commonApi": 2}, Capabilities{"commonApi": 2}); len(got) != 0 {
		t.Fatalf("expected equal level to satisfy, got %+v", got)
	}
	if got := UnmetCapabilities(nil, nil); len(got) != 0 {
		t.Fatalf("expected no requirements to be satisfied, got %+v", got)
	}
}
