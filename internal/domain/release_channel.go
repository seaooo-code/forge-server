package domain

import (
	"errors"
	"regexp"
	"time"
)

var (
	ErrInvalidReleaseChannelInput = errors.New("invalid release channel request")
	ErrReleaseChannelNotFound     = errors.New("release channel not found")
	ErrReleaseChannelExists       = errors.New("release channel already exists")
	// ErrReleaseChannelInUse 表示渠道在某个环境下还有生效中的发布，不能删除。
	ErrReleaseChannelInUse = errors.New("release channel in use")
)

// 渠道名称、说明的最大长度（按字符计）。
const (
	MaxReleaseChannelNameLength        = 50
	MaxReleaseChannelDescriptionLength = 200
)

var releaseChannelCodePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// IsValidReleaseChannelCode 校验渠道标识：小写字母开头，只含小写字母、数字和连字符，最长 32 个字符。
// 默认渠道是空字符串，不经过这个校验。
func IsValidReleaseChannelCode(code string) bool {
	return releaseChannelCodePattern.MatchString(code)
}

// ReleaseChannel 是应用下的一个发布渠道，在所有环境通用，规则见 docs/channels.md。
type ReleaseChannel struct {
	ApplicationID string
	Code          string
	Name          string
	Description   string
	// ActiveReleases 是该渠道在所有环境下生效中的发布数。
	ActiveReleases  int
	CreatedByUserID string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type ReleaseChannelInput struct {
	Code            string
	Name            string
	Description     string
	CreatedByUserID string
}
