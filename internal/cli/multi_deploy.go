package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/convox/convox/pkg/cli"
	"github.com/convox/convox/sdk"
	"github.com/convox/stdcli"
	"github.com/spf13/cobra"
)

// rackColors assigns distinct colors to each rack for interleaved output
var rackColors = []string{
	"\033[36m", // Cyan
	"\033[33m", // Yellow
	"\033[35m", // Magenta
	"\033[32m", // Green
	"\033[34m", // Blue
	"\033[91m", // Bright Red
}

const multiDeployColorReset = "\033[0m"

type multiDeployOptions struct {
	racks       string
	app         string
	description string
	file        string
	manifest    string
	noCache     bool
	replace     []string
	wait        bool
}

// MultiDeployCommand creates the multi-deploy command for deploying to multiple racks
func MultiDeployCommand() *cobra.Command {
	var opts multiDeployOptions

	cmd := &cobra.Command{
		Use:   "multi-deploy [dir]",
		Short: "Deploy to multiple racks in parallel",
		Long: `Deploy an app to multiple racks simultaneously with interleaved output.

This command collects MFA authentication once (single PIN entry), then requires
one security key tap per rack. Deploys run in parallel with color-coded output
showing which rack each line comes from.

Examples:
  # Deploy to staging, us, and eu racks
  cx multi-deploy --racks staging,us,eu

  # Deploy with specific app and wait for completion
  cx multi-deploy --racks staging,us,eu --app myapp --wait

  # Deploy with custom manifest
  cx multi-deploy --racks staging,us,eu -m convox.production.yml`,
		Args: cobra.MaximumNArgs(1),
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			return executeMultiDeploy(cobraCmd, args, opts)
		}),
	}

	cmd.Flags().StringVar(&opts.racks, "racks", "", "Comma-separated list of racks (required)")
	cmd.Flags().StringVarP(&opts.app, "app", "a", "", appFlagHelp)
	cmd.Flags().StringVar(&opts.description, "description", "", "build description")
	cmd.Flags().StringVarP(&opts.file, "file", "f", "", "path to Dockerfile")
	cmd.Flags().StringVarP(&opts.manifest, "manifest", "m", "", "path to manifest file")
	cmd.Flags().BoolVar(&opts.noCache, "no-cache", false, "disable build cache")
	cmd.Flags().StringSliceVar(&opts.replace, "replace", []string{}, "replace environment variable")
	cmd.Flags().BoolVar(&opts.wait, "wait", false, "wait for deployment to complete")

	_ = cmd.MarkFlagRequired("racks")

	return cmd
}

func executeMultiDeploy(cmd *cobra.Command, args []string, opts multiDeployOptions) error {
	racks, err := resolveRacks(opts.racks)
	if err != nil {
		return err
	}

	if len(racks) < 2 {
		return fmt.Errorf("multi-deploy requires at least 2 racks (use regular deploy for single rack)")
	}

	// Resolve app name once
	app, err := ResolveApp(opts.app)
	if err != nil {
		return err
	}

	// Display deployment plan
	printDeploymentPlan(cmd, racks, app)

	// Collect MFA for all racks sequentially (one PIN, one tap per rack)
	mfaAuths, err := collectMFAForAllRacks(cmd, racks)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintln(cmd.ErrOrStderr())
	_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "Starting parallel deploys...")
	_, _ = fmt.Fprintln(cmd.ErrOrStderr())

	// Run deploys in parallel with interleaved output
	return runParallelDeploys(cmd, args, racks, app, mfaAuths, opts)
}

func printDeploymentPlan(cmd *cobra.Command, racks []string, app string) {
	out := cmd.ErrOrStderr()
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintf(out, "Multi-rack deployment plan:\n")
	_, _ = fmt.Fprintf(out, "  App:   %s\n", app)
	_, _ = fmt.Fprintf(out, "  Racks: %s\n", strings.Join(racks, ", "))
	_, _ = fmt.Fprintln(out)
}

// collectMFAForAllRacks collects MFA authentication for each rack sequentially.
// Uses PIN caching so the user only enters their PIN once, then taps for each rack.
func collectMFAForAllRacks(cmd *cobra.Command, racks []string) (map[string]string, error) {
	// Skip MFA if using API token
	if os.Getenv("RACK_GATEWAY_API_TOKEN") != "" {
		result := make(map[string]string)
		for _, rack := range racks {
			result[rack] = ""
		}
		return result, nil
	}

	mfaAuths := make(map[string]string)
	var cachedPIN string

	_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "Authenticating with each rack (one tap per rack)...")
	_, _ = fmt.Fprintln(cmd.ErrOrStderr())

	for i, rack := range racks {
		color := rackColors[i%len(rackColors)]
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "%s[%s]%s Collecting MFA authentication...\n",
			color, rack, multiDeployColorReset)

		gatewayURL, bearer, err := LoadRackAuth(rack)
		if err != nil {
			return nil, fmt.Errorf("failed to load auth for rack %s: %w", rack, err)
		}

		mfaAuth, pin, err := GetMFAAuthWithPIN(cmd, gatewayURL, bearer, rack, cachedPIN)
		if err != nil {
			return nil, fmt.Errorf("MFA failed for rack %s: %w", rack, err)
		}

		// Cache the PIN for subsequent racks
		if cachedPIN == "" && pin != "" {
			cachedPIN = pin
		}

		mfaAuths[rack] = mfaAuth
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "%s[%s]%s ✓ Authenticated\n",
			color, rack, multiDeployColorReset)
	}

	return mfaAuths, nil
}

type deployResult struct {
	rack    string
	err     error
	elapsed time.Duration
}

func runParallelDeploys(
	cmd *cobra.Command,
	args []string,
	racks []string,
	app string,
	mfaAuths map[string]string,
	opts multiDeployOptions,
) error {
	var wg sync.WaitGroup
	results := make(chan deployResult, len(racks))

	// Create a mutex for synchronized output
	var outputMu sync.Mutex

	for i, rack := range racks {
		wg.Add(1)
		color := rackColors[i%len(rackColors)]

		go func(rack, color string, mfaAuth string) {
			defer wg.Done()

			start := time.Now()
			err := runSingleRackDeploy(cmd, args, rack, app, mfaAuth, opts, color, &outputMu)
			results <- deployResult{
				rack:    rack,
				err:     err,
				elapsed: time.Since(start),
			}
		}(rack, color, mfaAuths[rack])
	}

	// Wait for all deploys to complete
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results
	var errors []string
	var successes []string

	for result := range results {
		if result.err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", result.rack, result.err))
		} else {
			successes = append(successes,
				fmt.Sprintf("%s (%s)", result.rack, result.elapsed.Round(time.Second)))
		}
	}

	// Print summary
	_, _ = fmt.Fprintln(cmd.ErrOrStderr())
	_, _ = fmt.Fprintln(cmd.ErrOrStderr(), strings.Repeat("─", 60))

	if len(successes) > 0 {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "✅ Successful deploys: %s\n", strings.Join(successes, ", "))
	}

	if len(errors) > 0 {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "❌ Failed deploys:\n")
		for _, e := range errors {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "   - %s\n", e)
		}
		return fmt.Errorf("%d of %d deploys failed", len(errors), len(racks))
	}

	return nil
}

func runSingleRackDeploy(
	cmd *cobra.Command,
	args []string,
	rack, app, mfaAuth string,
	opts multiDeployOptions,
	color string,
	outputMu *sync.Mutex,
) error {
	// Build the auth token with MFA
	gatewayURL, token, err := LoadRackAuth(rack)
	if err != nil {
		return fmt.Errorf("failed to load rack auth: %w", err)
	}

	auth := token
	if mfaAuth != "" {
		auth = token + "." + mfaAuth
	}

	// Create SDK client
	client, err := sdk.New(buildRackURL(gatewayURL, auth))
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	// Create prefixed writers for this rack's output
	prefixedOut := newPrefixedWriter(cmd.OutOrStdout(), rack, color, outputMu)
	prefixedErr := newPrefixedWriter(cmd.ErrOrStderr(), rack, color, outputMu)

	// Create stdcli context with prefixed output
	ctx := createMultiDeployContext(args, app, opts, prefixedOut, prefixedErr)

	// Run the deploy
	err = cli.Deploy(client, ctx)

	// Flush any remaining output
	prefixedOut.Flush()
	prefixedErr.Flush()

	return normalizeConvoxExit(err)
}

// prefixedWriter wraps an io.Writer to prefix each line with a rack identifier
type prefixedWriter struct {
	underlying io.Writer
	prefix     string
	color      string
	mu         *sync.Mutex
	buffer     []byte
}

func newPrefixedWriter(w io.Writer, rack, color string, mu *sync.Mutex) *prefixedWriter {
	return &prefixedWriter{
		underlying: w,
		prefix:     fmt.Sprintf("[%s] ", rack),
		color:      color,
		mu:         mu,
		buffer:     make([]byte, 0),
	}
}

func (pw *prefixedWriter) Write(p []byte) (n int, err error) {
	pw.mu.Lock()
	defer pw.mu.Unlock()

	// Append to buffer
	pw.buffer = append(pw.buffer, p...)

	// Process complete lines
	for {
		idx := bytesIndexByte(pw.buffer, '\n')
		if idx < 0 {
			break
		}

		line := pw.buffer[:idx]
		pw.buffer = pw.buffer[idx+1:]

		// Write prefixed line
		if multiDeployColorsEnabled() {
			_, _ = fmt.Fprintf(pw.underlying, "%s%s%s%s\n",
				pw.color, pw.prefix, multiDeployColorReset, string(line))
		} else {
			_, _ = fmt.Fprintf(pw.underlying, "%s%s\n", pw.prefix, string(line))
		}
	}

	return len(p), nil
}

// Flush writes any remaining buffered content
func (pw *prefixedWriter) Flush() {
	pw.mu.Lock()
	defer pw.mu.Unlock()

	if len(pw.buffer) > 0 {
		if multiDeployColorsEnabled() {
			_, _ = fmt.Fprintf(pw.underlying, "%s%s%s%s\n",
				pw.color, pw.prefix, multiDeployColorReset, string(pw.buffer))
		} else {
			_, _ = fmt.Fprintf(pw.underlying, "%s%s\n", pw.prefix, string(pw.buffer))
		}
		pw.buffer = pw.buffer[:0]
	}
}

func bytesIndexByte(b []byte, c byte) int {
	for i, v := range b {
		if v == c {
			return i
		}
	}
	return -1
}

func createMultiDeployContext(
	args []string,
	app string,
	opts multiDeployOptions,
	stdout, stderr io.Writer,
) *stdcli.Context {
	// Build flags to pass to the deploy command
	var flags []*stdcli.Flag

	// App flag is always required
	flags = append(flags, &stdcli.Flag{Name: "app", Value: app})

	if opts.description != "" {
		flags = append(flags, &stdcli.Flag{Name: "description", Value: opts.description})
	}
	if opts.file != "" {
		flags = append(flags, &stdcli.Flag{Name: "file", Value: opts.file})
	}
	if opts.manifest != "" {
		flags = append(flags, &stdcli.Flag{Name: "manifest", Value: opts.manifest})
	}
	if opts.noCache {
		flags = append(flags, &stdcli.Flag{Name: "no-cache", Value: true})
	}
	if len(opts.replace) > 0 {
		flags = append(flags, &stdcli.Flag{Name: "replace", Value: opts.replace})
	}
	if opts.wait {
		flags = append(flags, &stdcli.Flag{Name: "wait", Value: true})
	}

	// Create an engine for output
	engine := stdcli.New("rack-gateway", Version)
	engine.Writer = &stdcli.Writer{
		Color:  multiDeployColorsEnabled(),
		Stdout: stdout,
		Stderr: stderr,
		Tags:   stdcli.DefaultWriter.Tags,
	}

	ctx := &stdcli.Context{
		Args:  args,
		Flags: flags,
	}

	// Inject the engine
	injectStdCLIEngine(ctx, engine)

	return ctx
}

// multiDeployColorsEnabled checks if terminal colors should be used
func multiDeployColorsEnabled() bool {
	// Check for NO_COLOR environment variable (standard)
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	// Check if stdout is a terminal
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// Ensure prefixedWriter implements io.Writer
var _ io.Writer = (*prefixedWriter)(nil)
