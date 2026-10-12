package app

import (
	"fmt"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/colcrypt"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/testbypass"
)

const (
	// developmentAppKey is the APP_SECRET_KEY that mise.toml and docker-compose.yml set for development and tests.
	developmentAppKey = "dev-secret-key-change-in-production"
	// minSecretBytes is the shortest APP_SECRET_KEY or AUDIT_HMAC_SECRET a deployment may use: 256 bits, as
	// `openssl rand -base64 32` generates.
	minSecretBytes = 32
)

// checkProductionSafety refuses to start a release build (releaseBuild) or a gateway whose database is marked
// production with a weak secret (see weakSecrets), and a production database while any test bypass is set. A
// release build has already refused the bypasses at startup (testbypass.Check), so for them the bypass check guards
// testbypass builds. getenv is os.Getenv in production; tests pass a fake.
func checkProductionSafety(dbEnvironment string, releaseBuild bool, getenv func(string) string) error {
	production := dbEnvironment == "production"
	var refused []string
	if production {
		refused = testbypass.Active(getenv)
	}
	if production || releaseBuild {
		refused = append(refused, weakSecrets(getenv)...)
	}
	if len(refused) == 0 {
		return nil
	}
	return fmt.Errorf(
		"refusing to start: a release build or a production database can't use these test settings or weak "+
			"secrets: %s",
		strings.Join(refused, ", "),
	)
}

// CheckReleaseSecrets refuses to run a release build (one without the testbypass tag) with a weak secret (see
// weakSecrets). The server checks this when it starts (checkProductionSafety); main runs it before `migrate` too,
// so a deploy, which migrates before it promotes, fails before the new release could start and crash. getenv is
// os.Getenv outside tests.
func CheckReleaseSecrets(getenv func(string) string) error {
	return refuseWeakReleaseSecrets(!testbypass.Compiled, getenv)
}

func refuseWeakReleaseSecrets(releaseBuild bool, getenv func(string) string) error {
	if !releaseBuild {
		return nil
	}
	if weak := weakSecrets(getenv); len(weak) > 0 {
		return fmt.Errorf("refusing to run: a release build can't use these weak secrets: %s", strings.Join(weak, ", "))
	}
	return nil
}

// weakSecrets lists the secrets in the environment that no deployment may use: a development value published in
// this repository, with which anyone could forge sessions, CSRF tokens and audit hashes or decrypt stored secrets,
// or an APP_SECRET_KEY or AUDIT_HMAC_SECRET that is blank or shorter than 32 bytes. Unset secrets aren't listed:
// the server refuses those itself.
func weakSecrets(getenv func(string) string) []string {
	var weak []string
	if colcrypt.IsDevelopmentKey(getenv(colcrypt.EnvVar)) {
		weak = append(weak, colcrypt.EnvVar+" (the development key published in this repository)")
	}
	secrets := []struct {
		name  string
		isDev func(string) bool
	}{
		{name: "APP_SECRET_KEY", isDev: func(value string) bool { return value == developmentAppKey }},
		{name: "AUDIT_HMAC_SECRET", isDev: db.IsDevelopmentAuditHMACSecret},
	}
	for _, secret := range secrets {
		if problem := secretProblem(getenv(secret.name), secret.isDev); problem != "" {
			weak = append(weak, secret.name+" ("+problem+")")
		}
	}
	return weak
}

// secretProblem says why a secret's value can't be used, or returns "" when it can (or is unset).
func secretProblem(raw string, isDev func(string) bool) string {
	value := strings.TrimSpace(raw)
	switch {
	case raw == "":
		return ""
	case value == "":
		return "blank: only whitespace"
	case isDev(value):
		return "the development value published in this repository"
	case len(value) < minSecretBytes:
		return fmt.Sprintf("shorter than %d bytes", minSecretBytes)
	default:
		return ""
	}
}
