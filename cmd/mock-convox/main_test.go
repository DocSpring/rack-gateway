package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApps_ListAndInfo(t *testing.T) {
	// List apps
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/apps", nil)
	getApps(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
	var apps []App
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &apps))
	require.GreaterOrEqual(t, len(apps), 1)
	assert.Equal(t, "rack-gateway", apps[0].Name)
	assert.Equal(t, "RAPP123456", apps[0].Release)

	// App info
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/apps/rack-gateway", nil)
	req = mux.SetURLVars(req, map[string]string{"app": "rack-gateway"})
	getApp(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
	var app App
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &app))
	assert.Equal(t, "rack-gateway", app.Name)
	assert.Equal(t, "RAPP123456", app.Release)
}

func TestReleases_List_Get_Create(t *testing.T) {
	// List
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/apps/rack-gateway/releases", nil)
	req = mux.SetURLVars(req, map[string]string{"app": "rack-gateway"})
	handleReleases(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
	var rels []Release
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &rels))
	require.GreaterOrEqual(t, len(rels), 1)
	assert.Equal(t, "rack-gateway", rels[0].App)

	// Get
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/apps/rack-gateway/releases/RAPI123456", nil)
	req = mux.SetURLVars(req, map[string]string{"app": "rack-gateway", "id": "RAPI123456"})
	getRelease(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
	var rel Release
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &rel))
	assert.Equal(t, "RAPI123456", rel.ID)
	assert.Contains(t, rel.Env, "NODE_ENV=production")

	// Create (POST to /releases) must return a single Release object
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/apps/rack-gateway/releases", nil)
	req = mux.SetURLVars(req, map[string]string{"app": "rack-gateway"})
	handleReleases(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
	rel = Release{}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &rel))
	assert.NotEmpty(t, rel.ID)
	assert.Equal(t, "rack-gateway", rel.App)
	assert.Contains(t, rel.Env, "PORT=3000")
}

func TestSystem(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/system", nil)
	getSystem(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
	var s System
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &s))
	assert.Equal(t, "mock-rack", s.Name)
	assert.NotEmpty(t, s.Version)
}
