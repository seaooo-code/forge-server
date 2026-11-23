package domain

import (
	"regexp"
	"slices"
	"strings"
)

// Capabilities 是模块在 forge.json 里声明的能力等级：键是能力名，值是非负整数等级。
// Common 模块用 provides 声明自己提供的等级，Feature 模块用 requires 声明它需要所属 Common 模块达到的最低等级。
// 平台不解释能力名的含义，只比较等级大小，具体用哪些能力名由各接入方的构建适配器约定。
type Capabilities map[string]int64

const (
	MaxCapabilities      = 16
	MaxCapabilityLevel   = 1<<31 - 1
	maxCapabilityNameLen = 64
)

var capabilityNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._-]*$`)

// IsValidCapabilityName 校验能力名：以字母开头，长度不超过 64，只允许字母、数字和 . _ -。
func IsValidCapabilityName(name string) bool {
	return len(name) <= maxCapabilityNameLen && capabilityNamePattern.MatchString(name)
}

// IsValidCapabilities 校验能力声明：最多 16 项，能力名合法，等级在 0 到 MaxCapabilityLevel 之间。
func IsValidCapabilities(capabilities Capabilities) bool {
	if len(capabilities) > MaxCapabilities {
		return false
	}
	for name, level := range capabilities {
		if !IsValidCapabilityName(name) || level < 0 || level > MaxCapabilityLevel {
			return false
		}
	}
	return true
}

// CapabilityShortfall 是一项没有满足的能力要求。Provided 为 nil 表示对方没有声明这项能力。
type CapabilityShortfall struct {
	Name     string
	Required int64
	Provided *int64
}

// UnmetCapabilities 返回 requires 里没有被 provides 满足的能力，按能力名排序。全部满足时返回空切片。
func UnmetCapabilities(requires Capabilities, provides Capabilities) []CapabilityShortfall {
	shortfalls := []CapabilityShortfall{}
	for name, required := range requires {
		provided, ok := provides[name]
		if ok && provided >= required {
			continue
		}
		shortfall := CapabilityShortfall{Name: name, Required: required}
		if ok {
			shortfall.Provided = &provided
		}
		shortfalls = append(shortfalls, shortfall)
	}
	slices.SortFunc(shortfalls, func(a, b CapabilityShortfall) int { return strings.Compare(a.Name, b.Name) })
	return shortfalls
}
