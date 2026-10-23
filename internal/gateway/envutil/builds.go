package envutil

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/httpclient"
)

// ErrBuildReadingEnv is returned by CheckNoBuildReadingEnv while a checked build may still read the app's env.
var ErrBuildReadingEnv = errors.New("a build of this app may still read its env")

// CheckNoBuildReadingEnv refuses an env change while a build whose manifest the gateway checked with the app's env
// may still read it. The rack's build reads the app's env when it starts (Convox common.AppEnvironment) and
// substitutes it into the manifest after the gateway's check, so a changed env could have it run images the check
// never saw. A build stops holding up env changes once the rack reports it complete or failed, or after
// db.CheckedBuildMaxAge. The caller holds the app's env lock.
func CheckNoBuildReadingEnv(
	ctx context.Context,
	database *db.Database,
	rack config.RackConfig,
	tlsConfig *tls.Config,
	app string,
) error {
	builds, err := database.UnfinishedCheckedBuilds(app)
	if err != nil {
		return err
	}
	for _, build := range builds {
		if build.BuildID == "" {
			return fmt.Errorf("%w: a build started at %s hasn't reported its ID; try again after %s",
				ErrBuildReadingEnv, build.CheckedAt.UTC().Format(time.RFC3339),
				build.CheckedAt.Add(db.CheckedBuildMaxAge).UTC().Format(time.RFC3339))
		}
		status, err := fetchBuildStatus(ctx, rack, tlsConfig, app, build.BuildID)
		if err != nil {
			return fmt.Errorf("failed to read build %s: %w", build.BuildID, err)
		}
		if status != "complete" && status != "failed" {
			return fmt.Errorf("%w: build %s is %s; change the env once it has finished",
				ErrBuildReadingEnv, build.BuildID, status)
		}
		if err := database.FinishCheckedBuild(build.ID); err != nil {
			return err
		}
	}
	return nil
}

// fetchBuildStatus reads a build's status from the rack.
func fetchBuildStatus(
	ctx context.Context,
	rack config.RackConfig,
	tlsConfig *tls.Config,
	app, buildID string,
) (string, error) {
	base := strings.TrimRight(rack.URL, "/")
	client := httpclient.NewRackClient(10*time.Second, tlsConfig)
	buildURL := fmt.Sprintf("%s/apps/%s/builds/%s", base, url.PathEscape(app), url.PathEscape(buildID))
	var build struct {
		Status string `json:"status"`
	}
	if err := getRackJSON(ctx, client, buildURL, rackAuthHeader(rack), &build); err != nil {
		return "", err
	}
	return build.Status, nil
}
