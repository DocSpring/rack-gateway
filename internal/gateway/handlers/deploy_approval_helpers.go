package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
)

var (
	errDeployApprovalRequestTokenNotFound = errors.New("api token not found")
	errDeployApprovalRequestForbidden     = errors.New("not authorized for target token")
	errDeployApprovalRequestTargetMissing = errors.New("target_api_token_id or target_api_token is required")
)

func resolveDeployApprovalRequestToken(database *db.Database, rbacSvc rbac.RBACManager, user *db.User, req CreateDeployApprovalRequestRequest, authUser *auth.AuthUser) (*db.APIToken, error) {
	identifier := strings.TrimSpace(req.TargetAPITokenName)
	var token *db.APIToken
	var err error

	hasExplicitTarget := false

	if req.TargetAPITokenID != nil {
		if trimmed := strings.TrimSpace(*req.TargetAPITokenID); trimmed != "" {
			token, err = database.GetAPITokenByPublicID(trimmed)
			hasExplicitTarget = true
		}
	}

	if token == nil && err == nil {
		if identifier != "" {
			hasExplicitTarget = true
			if id, parseErr := strconv.ParseInt(identifier, 10, 64); parseErr == nil {
				token, err = database.GetAPITokenByID(id)
			} else {
				token, err = database.GetAPITokenByName(identifier)
			}
		} else if authUser != nil && authUser.IsAPIToken && authUser.TokenID != nil && *authUser.TokenID > 0 {
			token, err = database.GetAPITokenByID(*authUser.TokenID)
		}
	}

	if err != nil {
		return nil, fmt.Errorf("failed to resolve api token: %w", err)
	}
	if token == nil {
		if hasExplicitTarget {
			return nil, errDeployApprovalRequestTokenNotFound
		}
		return nil, errDeployApprovalRequestTargetMissing
	}

	if token.UserID != user.ID {
		allowedAdmin, err := rbacSvc.Enforce(user.Email, rbac.ScopeGateway, rbac.ResourceDeployApprovalRequest, rbac.ActionApprove)
		if err != nil {
			return nil, fmt.Errorf("failed to check admin permission: %w", err)
		}
		if !allowedAdmin {
			return nil, errDeployApprovalRequestForbidden
		}
	}

	return token, nil
}

func toDeployApprovalRequestResponse(dr *db.DeployApprovalRequest) DeployApprovalRequestResponse {
	if dr == nil {
		return DeployApprovalRequestResponse{}
	}
	resp := DeployApprovalRequestResponse{
		PublicID:                  dr.PublicID,
		Message:                   dr.Message,
		Status:                    dr.Status,
		CreatedAt:                 dr.CreatedAt,
		UpdatedAt:                 dr.UpdatedAt,
		CreatedByEmail:            dr.CreatedByEmail,
		CreatedByName:             dr.CreatedByName,
		CreatedByAPITokenPublicID: dr.CreatedByAPITokenPublicID,
		CreatedByAPITokenName:     dr.CreatedByAPITokenName,
		TargetAPITokenID:          dr.TargetAPITokenPublicID,
		TargetAPITokenName:        dr.TargetAPITokenName,
		ApprovedByEmail:           dr.ApprovedByEmail,
		ApprovedByName:            dr.ApprovedByName,
		ApprovalNotes:             dr.ApprovalNotes,
		RejectedByEmail:           dr.RejectedByEmail,
		RejectedByName:            dr.RejectedByName,
		GitCommitHash:             dr.GitCommitHash,
		GitBranch:                 dr.GitBranch,
		PrURL:                     dr.PrURL,
		App:                       dr.App,
		ObjectURL:                 dr.ObjectURL,
		BuildID:                   dr.BuildID,
		ReleaseID:                 dr.ReleaseID,
		ProcessIDs:                dr.ProcessIDs,
	}
	// Unmarshal exec commands
	if len(dr.ExecCommands) > 0 {
		var commands map[string]interface{}
		if err := json.Unmarshal(dr.ExecCommands, &commands); err == nil {
			resp.ExecCommands = commands
		}
	}
	// Unmarshal CI metadata
	if len(dr.CIMetadata) > 0 {
		var metadata map[string]interface{}
		if err := json.Unmarshal(dr.CIMetadata, &metadata); err == nil {
			resp.CIMetadata = metadata
		}
	}
	if dr.ApprovedAt != nil {
		resp.ApprovedAt = dr.ApprovedAt
	}
	if dr.ApprovalExpiresAt != nil {
		resp.ApprovalExpiresAt = dr.ApprovalExpiresAt
	}
	if dr.RejectedAt != nil {
		resp.RejectedAt = dr.RejectedAt
	}
	if dr.ReleaseCreatedAt != nil {
		resp.ReleaseCreatedAt = dr.ReleaseCreatedAt
	}
	if dr.ReleasePromotedAt != nil {
		resp.ReleasePromotedAt = dr.ReleasePromotedAt
	}
	if dr.ReleasePromotedByAPITokenID != nil {
		resp.ReleasePromotedByTokenID = dr.ReleasePromotedByAPITokenID
	}
	return resp
}

func auditDetails(values map[string]string) string {
	filtered := make(map[string]string)
	for key, value := range values {
		k := strings.TrimSpace(key)
		v := strings.TrimSpace(value)
		if k == "" || v == "" {
			continue
		}
		filtered[k] = v
	}
	if len(filtered) == 0 {
		return "{}"
	}
	data, err := json.Marshal(filtered)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func getAppSettingBool(svc *settings.Service, appName, key string, defaultValue bool) (bool, error) {
	setting, err := svc.GetAppSetting(appName, key, defaultValue)
	if err != nil {
		return defaultValue, err
	}
	if val, ok := setting.Value.(bool); ok {
		return val, nil
	}
	return defaultValue, nil
}

func getAppSettingString(svc *settings.Service, appName, key string, defaultValue string) (string, error) {
	setting, err := svc.GetAppSetting(appName, key, defaultValue)
	if err != nil {
		return defaultValue, err
	}
	if val, ok := setting.Value.(string); ok {
		return val, nil
	}
	return defaultValue, nil
}
