package ui

import (
	"encoding/json"
	"html/template"
	"net/http"
	"strings"

	"github.com/DocSpring/convox-gateway/internal/gateway/auth"
	"github.com/DocSpring/convox-gateway/internal/gateway/rbac"
	"github.com/go-chi/chi/v5"
)

type Handler struct {
	rbacManager *rbac.Manager
	adminUsers  []string
	templates   *template.Template
}

func NewHandler(rbacManager *rbac.Manager, adminUsers []string) *Handler {
	return &Handler{
		rbacManager: rbacManager,
		adminUsers:  adminUsers,
	}
}

func (h *Handler) Index(w http.ResponseWriter, r *http.Request) {
	user, isAuth := auth.GetUser(r.Context())

	data := struct {
		IsAuthenticated bool
		User            *auth.Claims
		IsAdmin         bool
	}{
		IsAuthenticated: isAuth,
		User:            user,
		IsAdmin:         h.isAdmin(user),
	}

	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(h.renderIndex(data)))
}

func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	users := h.rbacManager.GetUsers()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(users)
}

func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string   `json:"email"`
		Name  string   `json:"name"`
		Roles []string `json:"roles"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	if err := h.rbacManager.AddUser(req.Email, req.Name, req.Roles); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"status": "created"})
}

func (h *Handler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	email := chi.URLParam(r, "email")

	var req struct {
		Name  string   `json:"name"`
		Roles []string `json:"roles"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	if err := h.rbacManager.AddUser(email, req.Name, req.Roles); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}

func (h *Handler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotImplemented)
	json.NewEncoder(w).Encode(map[string]string{"error": "not implemented"})
}

func (h *Handler) ListRoles(w http.ResponseWriter, r *http.Request) {
	roles := h.rbacManager.GetRoles()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(roles)
}

func (h *Handler) CreateRole(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotImplemented)
	json.NewEncoder(w).Encode(map[string]string{"error": "not implemented"})
}

func (h *Handler) UpdateRole(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotImplemented)
	json.NewEncoder(w).Encode(map[string]string{"error": "not implemented"})
}

func (h *Handler) DeleteRole(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotImplemented)
	json.NewEncoder(w).Encode(map[string]string{"error": "not implemented"})
}

func (h *Handler) ServeStatic(w http.ResponseWriter, r *http.Request) {
	http.StripPrefix("/ui/", http.FileServer(http.Dir("web/ui/static"))).ServeHTTP(w, r)
}

func (h *Handler) isAdmin(user *auth.Claims) bool {
	if user == nil {
		return false
	}

	for _, admin := range h.adminUsers {
		if user.Email == admin {
			return true
		}
	}

	roles := h.rbacManager.GetUserRoles(user.Email)
	for _, role := range roles {
		if role == "admin" {
			return true
		}
	}

	return false
}

func (h *Handler) renderIndex(data interface{}) string {
	html := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Convox Auth Proxy</title>
    <style>
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            max-width: 1200px;
            margin: 0 auto;
            padding: 20px;
            background: #f5f5f5;
        }
        .header {
            background: white;
            padding: 20px;
            border-radius: 8px;
            margin-bottom: 20px;
            box-shadow: 0 2px 4px rgba(0,0,0,0.1);
        }
        .card {
            background: white;
            padding: 20px;
            border-radius: 8px;
            margin-bottom: 20px;
            box-shadow: 0 2px 4px rgba(0,0,0,0.1);
        }
        table {
            width: 100%;
            border-collapse: collapse;
        }
        th, td {
            padding: 10px;
            text-align: left;
            border-bottom: 1px solid #eee;
        }
        th {
            background: #f8f8f8;
            font-weight: 600;
        }
        .btn {
            display: inline-block;
            padding: 8px 16px;
            background: #0066cc;
            color: white;
            text-decoration: none;
            border-radius: 4px;
            border: none;
            cursor: pointer;
        }
        .btn:hover {
            background: #0055aa;
        }
        .btn-small {
            padding: 4px 8px;
            font-size: 14px;
        }
        .tag {
            display: inline-block;
            padding: 2px 8px;
            background: #e0e0e0;
            border-radius: 12px;
            font-size: 12px;
            margin: 2px;
        }
    </style>
</head>
<body>
    <div class="header">
        <h1>Convox Auth Proxy</h1>
        {{if .IsAuthenticated}}
            <p>Logged in as: <strong>{{.User.Email}}</strong></p>
        {{else}}
            <p>Not authenticated</p>
        {{end}}
    </div>

    {{if .IsAdmin}}
    <div class="card">
        <h2>Administration</h2>
        <div id="users-section">
            <h3>Users</h3>
            <table id="users-table">
                <thead>
                    <tr>
                        <th>Email</th>
                        <th>Name</th>
                        <th>Roles</th>
                        <th>Actions</th>
                    </tr>
                </thead>
                <tbody></tbody>
            </table>
        </div>

        <div id="roles-section" style="margin-top: 40px;">
            <h3>Roles</h3>
            <table id="roles-table">
                <thead>
                    <tr>
                        <th>Name</th>
                        <th>Description</th>
                        <th>Permissions</th>
                    </tr>
                </thead>
                <tbody></tbody>
            </table>
        </div>
    </div>

    <script>
        async function loadUsers() {
            const resp = await fetch('/v1/admin/users', {
                headers: {
                    'Authorization': 'Bearer ' + localStorage.getItem('token')
                }
            });
            const users = await resp.json();
            const tbody = document.querySelector('#users-table tbody');
            tbody.innerHTML = '';
            
            Object.entries(users).forEach(([email, user]) => {
                const row = tbody.insertRow();
                row.innerHTML = ` + "`" + `
                    <td>${user.email}</td>
                    <td>${user.name}</td>
                    <td>${user.roles.map(r => '<span class="tag">' + r + '</span>').join('')}</td>
                    <td><button class="btn btn-small" onclick="editUser('${user.email}')">Edit</button></td>
                ` + "`" + `;
            });
        }

        async function loadRoles() {
            const resp = await fetch('/v1/admin/roles', {
                headers: {
                    'Authorization': 'Bearer ' + localStorage.getItem('token')
                }
            });
            const roles = await resp.json();
            const tbody = document.querySelector('#roles-table tbody');
            tbody.innerHTML = '';
            
            Object.entries(roles).forEach(([name, role]) => {
                const row = tbody.insertRow();
                row.innerHTML = ` + "`" + `
                    <td>${role.name}</td>
                    <td>${role.description}</td>
                    <td>${role.permissions.join(', ')}</td>
                ` + "`" + `;
            });
        }

        function editUser(email) {
            console.log('Edit user:', email);
        }

        if ({{.IsAdmin}}) {
            loadUsers();
            loadRoles();
        }
    </script>
    {{else}}
    <div class="card">
        <h2>Access Denied</h2>
        <p>You must be an administrator to access this page.</p>
    </div>
    {{end}}
</body>
</html>`

	return strings.ReplaceAll(html, "{{", "{{")
}
