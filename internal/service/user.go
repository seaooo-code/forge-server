package service

import (
	"strings"

	"forge-server/internal/domain"
)

func normalizeUserInput(input domain.UserInput) domain.UserInput {
	return domain.UserInput{
		ID:            strings.TrimSpace(input.ID),
		Name:          strings.TrimSpace(input.Name),
		Email:         strings.TrimSpace(input.Email),
		AvatarURL:     strings.TrimSpace(input.AvatarURL),
		DepartmentIDs: trimStringSlice(input.DepartmentIDs),
		FeishuUnionID: strings.TrimSpace(input.FeishuUnionID),
		FeishuOpenID:  strings.TrimSpace(input.FeishuOpenID),
		FeishuUserID:  strings.TrimSpace(input.FeishuUserID),
	}
}
