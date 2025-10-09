package handlers

import (
	"net/http"

	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/gin-gonic/gin"
)

// ListRoles godoc
// @Summary List RBAC roles
// @Description Returns metadata and permissions for each gateway RBAC role.
// @Tags Roles
// @Produce json
// @Success 200 {object} map[string]RoleDescriptor
// @Security SessionCookie
// @Router /admin/roles [get]
func (h *AdminHandler) ListRoles(c *gin.Context) {
	rolePerms := rbac.DefaultRolePermissions()
	metaMap := rbac.RoleMetadataMap()

	roles := make(map[string]RoleDescriptor, len(metaMap))
	for _, role := range rbac.RoleOrder() {
		meta, ok := metaMap[role]
		if !ok {
			continue
		}
		perms := rolePerms[role]
		if role == "admin" {
			perms = []string{"convox:*:*"}
		}
		roles[role] = RoleDescriptor{
			Name:        role,
			Label:       meta.Label,
			Description: meta.Description,
			Permissions: perms,
		}
	}

	c.JSON(http.StatusOK, roles)
}
