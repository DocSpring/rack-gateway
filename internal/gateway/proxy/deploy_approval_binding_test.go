package proxy

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

const (
	approvedCommit    = "0123456789abcdef0123456789abcdef01234567"
	otherCommit       = "fedcba9876543210fedcba9876543210fedcba98"
	approvedObjectURL = "object://docspring/tmp/approved.tgz"
	buildPath         = "/apps/docspring/builds"
)

type bindingFixture struct {
	h        *Handler
	database *db.Database
	authUser *auth.User
	approval *db.DeployApprovalRequest
	manifest string // convox.yml served inside the uploaded archive
}

func newBindingFixture(t *testing.T) *bindingFixture {
	t.Helper()
	f := &bindingFixture{}
	rack := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/apps/docspring/objects/tmp/approved.tgz" {
			_, _ = w.Write(tarballWithManifest(t, f.manifest))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	h, database, _, cleanup := newProxyWithRackServer(t, rack)
	t.Cleanup(cleanup)
	f.h, f.database = h, database

	owner, err := database.CreateUser("ci-owner@example.com", "CI", []string{"admin"})
	require.NoError(t, err)
	perms := rbac.DefaultPermissionsForRole("cicd")
	token, err := database.CreateAPIToken(strings.Repeat("b", 64), "ci", owner.ID, perms, nil, nil)
	require.NoError(t, err)
	f.authUser = &auth.User{
		Email: owner.Email, IsAPIToken: true, TokenID: &token.ID, Permissions: perms, DBUser: owner,
	}

	approval, err := database.CreateDeployApprovalRequest(
		"Deploy", "docspring", approvedCommit, "feature/x", "", nil, owner.ID, &token.ID, token.ID,
	)
	require.NoError(t, err)
	_, err = database.ApproveDeployApprovalRequest(approval.ID, owner.ID, time.Now().Add(time.Hour), "ok")
	require.NoError(t, err)
	require.NoError(t, database.UpdateDeployApprovalRequestObjectURL(approval.ID, approvedObjectURL))
	f.approval = approval
	f.setImagePatterns(t, map[string]string{"*": `docker\.io/docspringcom/app:{{GIT_COMMIT}}(-amd64)?`})
	return f
}

// setImagePatterns configures the app's service_image_patterns (nil clears them).
func (f *bindingFixture) setImagePatterns(t *testing.T, patterns map[string]string) {
	t.Helper()
	app := "docspring"
	if patterns == nil {
		require.NoError(t, f.database.DeleteSetting(&app, "service_image_patterns"))
		return
	}
	require.NoError(t, f.database.UpsertSetting(&app, "service_image_patterns", patterns, nil))
}

// build runs the permission check and build validation the proxy performs for POST /apps/{app}/builds.
func (f *bindingFixture) build(t *testing.T, body string) error {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, buildPath, strings.NewReader(body))
	allowed, tracker, err := f.h.evaluateAPITokenPermission(r, f.authUser, rbac.ResourceBuild, rbac.ActionCreate)
	if err != nil {
		return err
	}
	require.True(t, allowed)
	require.NotNil(t, tracker)
	r = r.WithContext(context.WithValue(r.Context(), deployApprovalContextKey, tracker))
	return f.h.validateBuildRequest(r, buildPath, []byte(body), f.authUser)
}

func tarballWithManifest(t *testing.T, manifest string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name: "convox.yml", Mode: 0o644, Size: int64(len(manifest)), Typeflag: tar.TypeReg,
	}))
	_, err := tw.Write([]byte(manifest))
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return buf.Bytes()
}

func imageManifest(images ...string) string {
	var b strings.Builder
	b.WriteString("services:\n")
	for i, image := range images {
		b.WriteString("  svc" + string(rune('a'+i)) + ":\n    image: " + image + "\n")
	}
	return b.String()
}

func TestTokenBuildAllowsImagesTaggedWithApprovedCommit(t *testing.T) {
	f := newBindingFixture(t)
	f.manifest = imageManifest(
		"docker.io/docspringcom/app:"+approvedCommit+"-amd64",
		"docker.io/docspringcom/app:"+approvedCommit,
	)
	require.NoError(t, f.build(t, "url="+approvedObjectURL))
	require.NoError(t, f.build(t, "url="+approvedObjectURL+"&git-sha="+approvedCommit))
}

func TestTokenBuildRejectsImageForAnotherCommit(t *testing.T) {
	f := newBindingFixture(t)
	f.manifest = imageManifest(
		"docker.io/docspringcom/app:"+approvedCommit+"-amd64",
		"docker.io/docspringcom/app:"+otherCommit+"-amd64",
	)
	err := f.build(t, "url="+approvedObjectURL)
	require.ErrorContains(t, err, "manifest validation failed")
	require.ErrorContains(t, err, "not tagged with the approved commit")
}

func TestTokenBuildRejectsOtherRepositoryWithApprovedCommitTag(t *testing.T) {
	f := newBindingFixture(t)
	f.manifest = imageManifest("ghcr.io/attacker/payload:" + approvedCommit + "-amd64")
	err := f.build(t, "url="+approvedObjectURL)
	require.ErrorContains(t, err, "does not match required pattern")
}

func TestTokenBuildRequiresImagePatterns(t *testing.T) {
	f := newBindingFixture(t)
	f.setImagePatterns(t, nil)
	f.manifest = imageManifest("docker.io/docspringcom/app:" + approvedCommit + "-amd64")
	err := f.build(t, "url="+approvedObjectURL)
	require.ErrorContains(t, err, "require service_image_patterns")
}

func TestTokenBuildRejectsServicesBuiltFromSource(t *testing.T) {
	f := newBindingFixture(t)
	f.manifest = "services:\n  web:\n    build: .\n"
	err := f.build(t, "url="+approvedObjectURL)
	require.ErrorContains(t, err, "must use a pre-built image")
}

func TestTokenBuildRejectsObjectNotUploadedUnderApproval(t *testing.T) {
	f := newBindingFixture(t)
	f.manifest = imageManifest("docker.io/docspringcom/app:" + approvedCommit)
	err := f.build(t, "url=object://docspring/tmp/attacker.tgz")
	var approvalErr *deployApprovalError
	require.ErrorAs(t, err, &approvalErr, "a build from another archive must not find the approval")
	require.Equal(t, http.StatusForbidden, approvalErr.status)

	// Even with the right tracker, the binding check compares the build URL to the approved object.
	tracker := &deployApprovalTracker{request: f.approval, app: "docspring"}
	tracker.request.ObjectURL = approvedObjectURL
	r := httptest.NewRequest(http.MethodPost, buildPath, nil)
	err = f.h.validateTokenBuildBinding(r, map[string][]string{"url": {"object://docspring/tmp/x.tgz"}}, tracker)
	require.ErrorContains(t, err, "must use the archive uploaded for deploy approval")
}

func TestTokenBuildRejectsGitSHAMismatch(t *testing.T) {
	f := newBindingFixture(t)
	f.manifest = imageManifest("docker.io/docspringcom/app:" + approvedCommit)
	err := f.build(t, "url="+approvedObjectURL+"&git-sha="+otherCommit)
	require.ErrorContains(t, err, "does not match the approved commit")
}

func TestTokenBuildAppliesAnchoredImagePatterns(t *testing.T) {
	f := newBindingFixture(t)
	app := "docspring"
	require.NoError(t, f.database.UpsertSetting(
		&app, "service_image_patterns", map[string]string{"*": `docker\.io/docspringcom/app:{{GIT_COMMIT}}-amd64`}, nil,
	))

	f.manifest = imageManifest("docker.io/docspringcom/app:" + approvedCommit + "-amd64")
	require.NoError(t, f.build(t, "url="+approvedObjectURL))

	// Tagged with the approved commit, but from another registry: the anchored pattern rejects it.
	f.manifest = imageManifest("ghcr.io/attacker/docker.io/docspringcom/app:" + approvedCommit + "-amd64")
	require.ErrorContains(t, f.build(t, "url="+approvedObjectURL), "does not match required pattern")
}

func TestImagePatternsAreAnchoredAndEscapeCommit(t *testing.T) {
	manifest := func(image string) *convoxManifest {
		return &convoxManifest{Services: map[string]convoxService{"web": {Image: image}}}
	}
	templates := map[string]string{"*": `.*:{{GIT_COMMIT}}-amd64`}

	policy := newImagePolicy(templates, approvedCommit, false)
	require.NoError(t, policy.validate(manifest("docspringcom/app:"+approvedCommit+"-amd64")))
	require.Error(t, policy.validate(manifest("docspringcom/app:"+approvedCommit+"-amd64-backdoor")))

	// A client-supplied commit is regex-escaped, so "." can't act as a wildcard.
	dotPolicy := newImagePolicy(templates, ".", false)
	require.Error(t, dotPolicy.validate(manifest("docspringcom/app:a-amd64")))
	require.NoError(t, dotPolicy.validate(manifest("docspringcom/app:.-amd64")))
}

func TestImageTaggedWithCommit(t *testing.T) {
	cases := map[string]bool{
		"docspringcom/app:" + approvedCommit:                              true,
		"docspringcom/app:" + approvedCommit + "-amd64":                   true,
		"docspringcom/app:" + strings.ToUpper(approvedCommit) + "-amd64":  true,
		"localhost:5000/app:" + approvedCommit:                            true,
		"docspringcom/app:" + approvedCommit + "@sha256:" + otherCommit:   true,
		"docspringcom/app:" + approvedCommit + "x":                        false,
		"docspringcom/app:" + approvedCommit[:7] + "-amd64":               false,
		"docspringcom/app:latest":                                         false,
		"docspringcom/app":                                                false,
		"localhost:5000/app":                                              false,
		"docspringcom/" + approvedCommit + ":latest":                      false,
		"docspringcom/app@sha256:" + approvedCommit + approvedCommit[:24]: false,
		"docspringcom/app:prefix-" + approvedCommit:                       false,
	}
	for image, want := range cases {
		require.Equalf(t, want, imageTaggedWithCommit(image, approvedCommit), "image %q", image)
	}
}

func TestApprovedDeployCommandsFailClosedWhenEmpty(t *testing.T) {
	h, _ := newProxyForDeployApprovalTest(t)
	require.False(t, h.isCommandApproved("docspring", "bin/rails console"),
		"an empty approved_deploy_commands list must not approve every command")
}
