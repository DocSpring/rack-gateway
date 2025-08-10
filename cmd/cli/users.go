package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type User struct {
	ID        int64     `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	Roles     []string  `json:"roles"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Suspended bool      `json:"suspended"`
}

func createUsersCmd() *cobra.Command {
	usersCmd := &cobra.Command{
		Use:   "users",
		Short: "Manage gateway users",
		Long:  "Commands for managing users in the convox-gateway",
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all users",
		RunE:  listUsers,
	}

	addCmd := &cobra.Command{
		Use:   "add <email> <name> <roles>",
		Short: "Add a new user",
		Long:  "Add a new user with specified roles (comma-separated: viewer,ops,deployer,admin)",
		Args:  cobra.ExactArgs(3),
		RunE:  addUser,
	}

	removeCmd := &cobra.Command{
		Use:   "remove <email>",
		Short: "Remove a user",
		Args:  cobra.ExactArgs(1),
		RunE:  removeUser,
	}

	setRolesCmd := &cobra.Command{
		Use:   "set-roles <email> <roles>",
		Short: "Update user roles",
		Long:  "Update user roles (comma-separated: viewer,ops,deployer,admin)",
		Args:  cobra.ExactArgs(2),
		RunE:  setUserRoles,
	}

	suspendCmd := &cobra.Command{
		Use:   "suspend <email>",
		Short: "Suspend a user",
		Args:  cobra.ExactArgs(1),
		RunE:  suspendUser,
	}

	unsuspendCmd := &cobra.Command{
		Use:   "unsuspend <email>",
		Short: "Unsuspend a user",
		Args:  cobra.ExactArgs(1),
		RunE:  unsuspendUser,
	}

	usersCmd.AddCommand(listCmd, addCmd, removeCmd, setRolesCmd, suspendCmd, unsuspendCmd)
	return usersCmd
}

func listUsers(cmd *cobra.Command, args []string) error {
	// Get current rack and token
	rack, err := getCurrentRack()
	if err != nil {
		return fmt.Errorf("no rack selected. Run: convox-gateway login <rack> <gateway-url>")
	}

	gatewayURL, err := loadGatewayURL(rack)
	if err != nil {
		return fmt.Errorf("rack %s not configured", rack)
	}

	token, err := loadToken(rack)
	if err != nil {
		return fmt.Errorf("not logged in to rack %s", rack)
	}

	// Make request to gateway
	req, err := http.NewRequest("GET", gatewayURL+"/.gateway/admin/users", nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+token.Token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to gateway: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("unauthorized - admin role required")
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to list users: %s", string(body))
	}

	var users []User
	if err := json.NewDecoder(resp.Body).Decode(&users); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	// Display users
	fmt.Printf("%-40s %-20s %-20s %-10s\n", "EMAIL", "NAME", "ROLES", "STATUS")
	fmt.Printf("%s\n", strings.Repeat("-", 90))
	for _, user := range users {
		status := "active"
		if user.Suspended {
			status = "suspended"
		}
		fmt.Printf("%-40s %-20s %-20s %-10s\n",
			user.Email,
			truncate(user.Name, 20),
			strings.Join(user.Roles, ","),
			status)
	}

	return nil
}

func addUser(cmd *cobra.Command, args []string) error {
	email := args[0]
	name := args[1]
	roles := strings.Split(args[2], ",")

	// Validate roles
	validRoles := map[string]bool{
		"viewer":   true,
		"ops":      true,
		"deployer": true,
		"admin":    true,
	}
	for _, role := range roles {
		if !validRoles[role] {
			return fmt.Errorf("invalid role: %s. Valid roles are: viewer, ops, deployer, admin", role)
		}
	}

	// Get current rack and token
	rack, err := getCurrentRack()
	if err != nil {
		return fmt.Errorf("no rack selected. Run: convox-gateway login <rack> <gateway-url>")
	}

	gatewayURL, err := loadGatewayURL(rack)
	if err != nil {
		return fmt.Errorf("rack %s not configured", rack)
	}

	token, err := loadToken(rack)
	if err != nil {
		return fmt.Errorf("not logged in to rack %s", rack)
	}

	// Create request body
	reqBody := map[string]interface{}{
		"email": email,
		"name":  name,
		"roles": roles,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	// Make request to gateway
	req, err := http.NewRequest("POST", gatewayURL+"/.gateway/admin/users", bytes.NewBuffer(body))
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+token.Token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to gateway: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("unauthorized - admin role required")
	}

	if resp.StatusCode == http.StatusConflict {
		return fmt.Errorf("user %s already exists", email)
	}

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to add user: %s", string(respBody))
	}

	fmt.Printf("User %s added successfully\n", email)
	return nil
}

func removeUser(cmd *cobra.Command, args []string) error {
	email := args[0]

	// Get current rack and token
	rack, err := getCurrentRack()
	if err != nil {
		return fmt.Errorf("no rack selected. Run: convox-gateway login <rack> <gateway-url>")
	}

	gatewayURL, err := loadGatewayURL(rack)
	if err != nil {
		return fmt.Errorf("rack %s not configured", rack)
	}

	token, err := loadToken(rack)
	if err != nil {
		return fmt.Errorf("not logged in to rack %s", rack)
	}

	// Make request to gateway
	req, err := http.NewRequest("DELETE", gatewayURL+"/.gateway/admin/users/"+email, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+token.Token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to gateway: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("unauthorized - admin role required")
	}

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("user %s not found", email)
	}

	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to remove user: %s", string(body))
	}

	fmt.Printf("User %s removed successfully\n", email)
	return nil
}

func setUserRoles(cmd *cobra.Command, args []string) error {
	email := args[0]
	roles := strings.Split(args[1], ",")

	// Validate roles
	validRoles := map[string]bool{
		"viewer":   true,
		"ops":      true,
		"deployer": true,
		"admin":    true,
	}
	for _, role := range roles {
		if !validRoles[role] {
			return fmt.Errorf("invalid role: %s. Valid roles are: viewer, ops, deployer, admin", role)
		}
	}

	// Get current rack and token
	rack, err := getCurrentRack()
	if err != nil {
		return fmt.Errorf("no rack selected. Run: convox-gateway login <rack> <gateway-url>")
	}

	gatewayURL, err := loadGatewayURL(rack)
	if err != nil {
		return fmt.Errorf("rack %s not configured", rack)
	}

	token, err := loadToken(rack)
	if err != nil {
		return fmt.Errorf("not logged in to rack %s", rack)
	}

	// Create request body
	reqBody := map[string]interface{}{
		"roles": roles,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	// Make request to gateway
	req, err := http.NewRequest("PUT", gatewayURL+"/.gateway/admin/users/"+email+"/roles", bytes.NewBuffer(body))
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+token.Token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to gateway: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("unauthorized - admin role required")
	}

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("user %s not found", email)
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to update user roles: %s", string(respBody))
	}

	fmt.Printf("User %s roles updated to: %s\n", email, strings.Join(roles, ","))
	return nil
}

func suspendUser(cmd *cobra.Command, args []string) error {
	email := args[0]
	return setUserSuspended(email, true)
}

func unsuspendUser(cmd *cobra.Command, args []string) error {
	email := args[0]
	return setUserSuspended(email, false)
}

func setUserSuspended(email string, suspended bool) error {
	// Get current rack and token
	rack, err := getCurrentRack()
	if err != nil {
		return fmt.Errorf("no rack selected. Run: convox-gateway login <rack> <gateway-url>")
	}

	gatewayURL, err := loadGatewayURL(rack)
	if err != nil {
		return fmt.Errorf("rack %s not configured", rack)
	}

	token, err := loadToken(rack)
	if err != nil {
		return fmt.Errorf("not logged in to rack %s", rack)
	}

	// Create request body
	reqBody := map[string]interface{}{
		"suspended": suspended,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	// Make request to gateway
	req, err := http.NewRequest("PUT", gatewayURL+"/.gateway/admin/users/"+email+"/suspend", bytes.NewBuffer(body))
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+token.Token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to gateway: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("unauthorized - admin role required")
	}

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("user %s not found", email)
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to update user status: %s", string(respBody))
	}

	if suspended {
		fmt.Printf("User %s suspended successfully\n", email)
	} else {
		fmt.Printf("User %s unsuspended successfully\n", email)
	}
	return nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
