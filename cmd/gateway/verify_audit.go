package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/auditverify"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/jobs"
)

type verifyAuditFlags struct {
	from       int64
	to         int64
	hashV2From chainIndexFlag
	// maxAnchorAge is 0 unless --max-anchor-age is given.
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
	index, err := auditverify.ParseChainIndex(value)
	if err != nil {
		return err
	}
	f.value = &index
	return nil
}

func parseVerifyAuditFlags(args []string) (verifyAuditFlags, error) {
	var parsed verifyAuditFlags
	set := flag.NewFlagSet("verify-audit", flag.ContinueOnError)
	set.Int64Var(&parsed.from, "from", 0, "first chain index to verify")
	set.Int64Var(&parsed.to, "to", -1, "last chain index to verify (default: the latest event)")
	set.Var(&parsed.hashV2From, "hash-v2-from",
		"chain index of the first hash v2 event, recorded after upgrading (default: $"+auditverify.HashV2FromEnv+")")
	set.DurationVar(&parsed.maxAnchorAge, "max-anchor-age", 0,
		"fail when the latest anchor is older than this (default: 3h, or three anchor intervals when "+
			"AUDIT_ANCHOR_INTERVAL_MINUTES is longer than an hour)")
	set.BoolVar(&parsed.noAnchors, "no-anchors", false, "skip the S3 anchor checks")
	if err := set.Parse(args); err != nil {
		return parsed, err
	}
	if set.NArg() > 0 {
		return parsed, fmt.Errorf("verify-audit does not accept arguments, got: %v", set.Args())
	}
	maxAnchorAgeGiven := false
	set.Visit(func(given *flag.Flag) { maxAnchorAgeGiven = maxAnchorAgeGiven || given.Name == "max-anchor-age" })
	if maxAnchorAgeGiven && parsed.maxAnchorAge <= 0 {
		return parsed, fmt.Errorf("--max-anchor-age must be positive, got %s", parsed.maxAnchorAge)
	}
	return parsed, nil
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
	if report != nil {
		// A run that stopped partway prints what it found until then
		if writeErr := report.Write(os.Stdout); writeErr != nil {
			return writeErr
		}
	}
	if err != nil {
		return fmt.Errorf("verify audit log: %w", err)
	}
	if !report.OK() {
		return fmt.Errorf("audit log verification failed: %d integrity failures", report.FailureCount)
	}
	return nil
}

// verifyAuditOptions is the configuration the gateway's scheduled verification uses (jobs.AuditVerifyOptions),
// with the flags applied.
func verifyAuditOptions(parsed verifyAuditFlags) (auditverify.Options, error) {
	anchors, err := verifyAuditAnchors(parsed)
	if err != nil {
		return auditverify.Options{}, err
	}
	opts, err := jobs.AuditVerifyOptions(anchors)
	if err != nil {
		return auditverify.Options{}, err
	}
	opts.From = parsed.from
	if parsed.to >= 0 {
		opts.To = &parsed.to
	}
	if parsed.hashV2From.value != nil {
		opts.HashV2From = parsed.hashV2From.value
	}
	if opts.Anchors != nil && parsed.maxAnchorAge > 0 {
		opts.Anchors.MaxAge = parsed.maxAnchorAge
	}
	return opts, nil
}

// verifyAuditAnchors returns the anchor configuration, or nil with --no-anchors. Unlike the scheduled verification,
// the command doesn't skip the anchor checks just because anchors aren't configured.
func verifyAuditAnchors(parsed verifyAuditFlags) (*jobs.AuditAnchorConfig, error) {
	if parsed.noAnchors {
		return nil, nil
	}
	config, err := jobs.NewAuditAnchorConfigFromEnv()
	if err != nil {
		return nil, fmt.Errorf("failed to load audit anchor config: %w", err)
	}
	if config == nil {
		return nil, errors.New(
			"audit anchors aren't configured: set AUDIT_ANCHOR_BUCKET and AUDIT_ANCHOR_CHAIN_ID, or pass --no-anchors",
		)
	}
	return config, nil
}
