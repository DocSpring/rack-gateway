package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/DocSpring/rack-gateway/internal/gateway/auditverify"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/jobs"
)

// hashV2FromEnv pins where hash v2 starts when --hash-v2-from isn't given.
const hashV2FromEnv = "AUDIT_HASH_V2_FROM"

type verifyAuditFlags struct {
	from         int64
	to           int64
	hashV2From   chainIndexFlag
	maxAnchorAge time.Duration
	noAnchors    bool
}

// chainIndexFlag is an optional, non-negative chain index.
type chainIndexFlag struct {
	value *int64
}

func (f *chainIndexFlag) String() string {
	if f == nil || f.value == nil {
		return ""
	}
	return strconv.FormatInt(*f.value, 10)
}

func (f *chainIndexFlag) Set(value string) error {
	index, err := parseChainIndex(value)
	if err != nil {
		return err
	}
	f.value = &index
	return nil
}

func parseChainIndex(value string) (int64, error) {
	index, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || index < 0 {
		return 0, fmt.Errorf("expected a chain index (a whole number, 0 or more), got %q", value)
	}
	return index, nil
}

func parseVerifyAuditFlags(args []string) (verifyAuditFlags, error) {
	var parsed verifyAuditFlags
	set := flag.NewFlagSet("verify-audit", flag.ContinueOnError)
	set.Int64Var(&parsed.from, "from", 0, "first chain index to verify")
	set.Int64Var(&parsed.to, "to", -1, "last chain index to verify (default: the latest event)")
	set.Var(&parsed.hashV2From, "hash-v2-from",
		"chain index of the first hash v2 event, recorded after upgrading (default: $"+hashV2FromEnv+")")
	set.DurationVar(&parsed.maxAnchorAge, "max-anchor-age", auditverify.DefaultMaxAnchorAge,
		"fail when the latest anchor is older than this")
	set.BoolVar(&parsed.noAnchors, "no-anchors", false, "skip the S3 anchor checks")
	if err := set.Parse(args); err != nil {
		return parsed, err
	}
	if set.NArg() > 0 {
		return parsed, fmt.Errorf("verify-audit does not accept arguments, got: %v", set.Args())
	}
	if parsed.maxAnchorAge <= 0 {
		return parsed, fmt.Errorf("--max-anchor-age must be positive, got %s", parsed.maxAnchorAge)
	}
	return parsed, nil
}

// pinnedHashV2Start returns the hash v2 start from --hash-v2-from, or else from AUDIT_HASH_V2_FROM (nil if
// neither is set).
func pinnedHashV2Start(parsed verifyAuditFlags) (*int64, error) {
	if parsed.hashV2From.value != nil {
		return parsed.hashV2From.value, nil
	}
	value := os.Getenv(hashV2FromEnv)
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	start, err := parseChainIndex(value)
	if err != nil {
		return nil, fmt.Errorf("invalid %s: %w", hashV2FromEnv, err)
	}
	return &start, nil
}

// verifyAudit runs `rack-gateway-api verify-audit`: it checks the audit log's hash chain and, unless
// --no-anchors is given, every anchor in the S3 bucket, prints a summary and fails on any integrity failure.
func verifyAudit(args []string) error {
	parsed, err := parseVerifyAuditFlags(args)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	opts, err := verifyAuditOptions(parsed)
	if err != nil {
		return err
	}

	dsn, err := firstDatabaseURL("RGW_DATABASE_URL", "GATEWAY_DATABASE_URL", "DATABASE_URL")
	if err != nil {
		return err
	}
	database, err := db.NewWithPoolConfigAndMigration(dsn, nil, false)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer closeDatabase(database)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	report, err := auditverify.Run(ctx, database, opts)
	if err != nil {
		return fmt.Errorf("verify audit log: %w", err)
	}
	if err := report.Write(os.Stdout); err != nil {
		return err
	}
	if !report.OK() {
		return fmt.Errorf("audit log verification failed: %d integrity failures", report.FailureCount)
	}
	return nil
}

func verifyAuditOptions(parsed verifyAuditFlags) (auditverify.Options, error) {
	// Never the development fallback: hashes written with the real secret would all fail, and hashes written
	// with the development secret prove nothing.
	secret, err := db.AuditHMACSecretFromEnv()
	if err != nil {
		return auditverify.Options{}, err
	}
	opts := auditverify.Options{Secret: secret, From: parsed.from}
	if parsed.to >= 0 {
		opts.To = &parsed.to
	}
	if opts.HashV2From, err = pinnedHashV2Start(parsed); err != nil {
		return auditverify.Options{}, err
	}
	if !parsed.noAnchors {
		if opts.Anchors, err = anchorOptionsFromEnv(); err != nil {
			return auditverify.Options{}, err
		}
		opts.Anchors.MaxAge = parsed.maxAnchorAge
	}
	return opts, nil
}

func anchorOptionsFromEnv() (*auditverify.AnchorOptions, error) {
	config, err := jobs.NewAuditAnchorConfigFromEnv()
	if err != nil {
		return nil, fmt.Errorf("failed to load audit anchor config: %w", err)
	}
	if config == nil {
		return nil, errors.New(
			"audit anchors aren't configured: set AUDIT_ANCHOR_BUCKET and AUDIT_ANCHOR_CHAIN_ID, or pass --no-anchors",
		)
	}
	client, ok := config.S3Client.(*s3.Client)
	if !ok {
		return nil, errors.New("invalid S3 client type")
	}
	return &auditverify.AnchorOptions{Store: client, Bucket: config.Bucket, ChainID: config.ChainID}, nil
}
