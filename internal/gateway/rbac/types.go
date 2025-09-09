package rbac

// UserConfig represents a user and their roles in the RBAC layer.
type UserConfig struct {
	Name  string   `json:"name"`
	Roles []string `json:"roles"`
}
