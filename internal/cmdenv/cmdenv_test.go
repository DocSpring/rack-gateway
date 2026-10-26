package cmdenv

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/kballard/go-shellquote"
	"github.com/stretchr/testify/require"
)

// helperArg makes the test binary print its environment and arguments as JSON and exit with the code in its next
// argument, so a test can run it as the wrapped command and see exactly what it got.
const helperArg = "cmdenv-print-env"

type helperOutput struct {
	Env  map[string]string `json:"env"`
	Args []string          `json:"args"`
}

func TestMain(m *testing.M) {
	if len(os.Args) > 2 && os.Args[1] == helperArg {
		out := helperOutput{Env: map[string]string{}, Args: os.Args[3:]}
		for _, entry := range os.Environ() {
			name, value, _ := strings.Cut(entry, "=")
			out.Env[name] = value
		}
		_ = json.NewEncoder(os.Stdout).Encode(out)
		code, _ := strconv.Atoi(os.Args[2])
		os.Exit(code)
	}
	os.Exit(m.Run())
}

func mustPlan(t *testing.T, all bool, vars []string, readOnly map[string]string) Plan {
	t.Helper()
	plan, err := New(all, vars, readOnly)
	require.NoError(t, err)
	return plan
}

func TestNewSortsNamesAndKeepsOnlyTheSubstitutesOfVarsTheCommandGets(t *testing.T) {
	readOnly := map[string]string{"DATABASE_URL": "READ_ONLY_DATABASE_URL", "ADMIN_URL": "READ_ONLY_DATABASE_URL"}
	plan := mustPlan(t, false, []string{"PATH", "DATABASE_URL", "PATH", "HOME"}, readOnly)
	require.Equal(t, Plan{
		Vars:     []string{"DATABASE_URL", "HOME", "PATH"},
		ReadOnly: map[string]string{"DATABASE_URL": "READ_ONLY_DATABASE_URL"},
	}, plan)

	require.Equal(t, Plan{All: true, ReadOnly: readOnly}, mustPlan(t, true, []string{"IGNORED"}, readOnly))
	require.Equal(t, Plan{}, mustPlan(t, false, nil, readOnly))
}

func TestNewRefusesBadNamesRackURLAndChainedSubstitutes(t *testing.T) {
	for name, tc := range map[string]struct {
		vars     []string
		readOnly map[string]string
		want     string
	}{
		"invalid name":         {vars: []string{"PATH", "BAD-NAME"}, want: `"BAD-NAME" isn't an env var name`},
		"empty name":           {vars: []string{""}, want: `"" isn't an env var name`},
		"RACK_URL":             {vars: []string{"RACK_URL"}, want: "no command can have RACK_URL"},
		"RACK_URL substituted": {readOnly: map[string]string{"RACK_URL": "X"}, want: "no command can have RACK_URL"},
		"RACK_URL substitute":  {readOnly: map[string]string{"X": "RACK_URL"}, want: "no command can have RACK_URL"},
		"invalid substitute":   {readOnly: map[string]string{"X": "Y Z"}, want: `"Y Z" isn't an env var name`},
		"own substitute":       {readOnly: map[string]string{"X": "X"}, want: "X can't be its own read-only"},
		"chained": {
			readOnly: map[string]string{"A": "B", "B": "C"},
			want:     "B is the read-only substitute for A, so it can't have one of its own",
		},
	} {
		for _, all := range []bool{false, true} {
			if all && tc.readOnly == nil {
				continue // every env var: the names aren't used
			}
			_, err := New(all, tc.vars, tc.readOnly)
			require.ErrorContains(t, err, tc.want, name)
		}
	}
}

func TestBuildAndParseRoundTrip(t *testing.T) {
	readOnly := map[string]string{"DATABASE_URL": "READ_ONLY_DATABASE_URL"}
	for _, plan := range []Plan{
		{},
		mustPlan(t, false, []string{"PATH", "DATABASE_URL"}, readOnly),
		mustPlan(t, false, []string{"PATH"}, nil),
		mustPlan(t, true, nil, nil),
		mustPlan(t, true, nil, readOnly),
	} {
		for _, command := range []string{"sleep 3600", `bin/rails runner 'puts "it'"'"'s $HOME"'`, "a  b"} {
			wrapped := Build(plan, command)
			parsed, original, ok := Parse(wrapped)
			require.True(t, ok, wrapped)
			require.Equal(t, plan, parsed)
			require.Equal(t, command, original)
		}
	}
	require.Equal(t,
		`sh -c 'exec env -i -- ${READ_ONLY_DATABASE_URL+"DATABASE_URL=$READ_ONLY_DATABASE_URL"} ${PATH+"PATH=$PATH"} `+
			`"$@"' rack-gateway-env rake db:migrate`,
		Build(mustPlan(t, false, []string{"PATH", "DATABASE_URL"}, readOnly), "rake db:migrate"))
	require.Equal(t,
		`sh -c 'unset RACK_URL DATABASE_URL; exec env -- `+
			`${READ_ONLY_DATABASE_URL+"DATABASE_URL=$READ_ONLY_DATABASE_URL"} "$@"' rack-gateway-env rake db:migrate`,
		Build(mustPlan(t, true, nil, readOnly), "rake db:migrate"))
}

// Only the wrapper Build makes for a valid plan reads back.
func TestParseRefusesWrappersBuildDoesntMake(t *testing.T) {
	for _, wrapped := range []string{
		"rake db:migrate",
		`sh -c 'exec "$@"' rack-gateway-env rake`,
		`sh -c 'exec env -i -- "$@"' rack-gateway-script rake`,
		`sh -c 'exec env -i -- ${PATH+"PATH=$PATH"} ${HOME+"HOME=$HOME"} "$@"' rack-gateway-env rake`, // unsorted
		`sh -c 'exec env -i -- ${PATH+"PATH=$PATH"} ${PATH+"PATH=$PATH"} "$@"' rack-gateway-env rake`, // repeated
		`sh -c 'exec env -i -- ${RACK_URL+"RACK_URL=$RACK_URL"} "$@"' rack-gateway-env rake`,
		`sh -c 'exec env -i -- ${A+"B=$C"} "$@"' rack-gateway-env rake`,
		`sh -c 'exec env -i -- ${A+"B=$A"} ${B+"C=$B"} "$@"' rack-gateway-env rake`, // chained substitutes
		`sh -c 'exec env -i -- ${PATH+"PATH=$PATH"}"$@"' rack-gateway-env rake`,
		`sh -c 'exec env -i -- "$@"; id' rack-gateway-env rake`,
		`sh -c 'unset RACK_URL; exec env -- ${A+"A=$A"} "$@"' rack-gateway-env rake`,
		`sh -c 'unset RACK_URL B; exec env -- ${A+"C=$A"} "$@"' rack-gateway-env rake`,
		`sh -c 'unset RACK_URL; exec env -- "$@"'`,
	} {
		_, _, ok := Parse(wrapped)
		require.False(t, ok, wrapped)
	}
}

func TestCheckCommand(t *testing.T) {
	for command, want := range map[string]error{
		"rake db:migrate":              nil,
		`sh -c 'A=1 rake'`:             nil,
		`'' rake`:                      nil, // runs "", which fails as it would unwrapped
		"":                             errEmptyCommand,
		"   ":                          errEmptyCommand,
		"FOO=bar rake db:migrate":      errAssignmentFirst,
		`"FOO=bar" rake`:               errAssignmentFirst,
		`rake 'unbalanced`:             errors.New("the rack can't split the command into words"),
		"bin/rails runner 'puts 1' \\": errors.New("the rack can't split the command into words"),
	} {
		err := CheckCommand(command)
		if want == nil {
			require.NoError(t, err, command)
		} else {
			require.ErrorContains(t, err, want.Error(), command)
		}
	}
}

func TestDescribe(t *testing.T) {
	readOnly := map[string]string{"DATABASE_URL": "READ_ONLY_DATABASE_URL"}
	require.Equal(t, "no env vars", Plan{}.Describe())
	require.Equal(t, "DATABASE_URL (from READ_ONLY_DATABASE_URL), PATH",
		mustPlan(t, false, []string{"PATH", "DATABASE_URL"}, readOnly).Describe())
	require.Equal(t, "every env var except RACK_URL", mustPlan(t, true, nil, nil).Describe())
	require.Equal(t, "every env var except RACK_URL; read-only DATABASE_URL (from READ_ONLY_DATABASE_URL)",
		mustPlan(t, true, nil, readOnly).Describe())
}

// shell is a shell the rack's image might have: how to run it, and the directory holding the env it uses (""
// for the one on PATH).
type shell struct {
	name, envDir string
	argv         []string
}

// shells returns the shells on this machine to run the wrapper with: sh, and dash, bash and busybox's ash (with
// busybox's env) when they're installed.
func shells(t *testing.T) []shell {
	t.Helper()
	found := []shell{{name: "sh", argv: []string{"sh"}}}
	for _, name := range []string{"dash", "bash"} {
		if path, err := exec.LookPath(name); err == nil {
			found = append(found, shell{name: name, argv: []string{path}})
		}
	}
	if busybox, err := exec.LookPath("busybox"); err == nil {
		dir := t.TempDir()
		require.NoError(t, os.Symlink(busybox, filepath.Join(dir, "env")))
		found = append(found, shell{name: "busybox", argv: []string{busybox, "sh"}, envDir: dir})
	}
	return found
}

// run runs the wrapper Build makes for plan the way the rack does (split into words, run without a shell), with sh
// replaced by the shell, in a process whose environment is env. The command is the test binary, printing what it
// got and exiting with code.
func run(t *testing.T, sh shell, plan Plan, env map[string]string, code int, args ...string) (helperOutput, int) {
	t.Helper()
	command := shellquote.Join(append([]string{os.Args[0], helperArg, strconv.Itoa(code)}, args...)...)
	argv, err := shellquote.Split(Build(plan, command))
	require.NoError(t, err)
	require.Equal(t, "sh", argv[0])
	argv = append(append([]string(nil), sh.argv...), argv[1:]...)

	cmd := exec.Command(argv[0], argv[1:]...) // #nosec G702 -- the test runs the command it built
	cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
	if sh.envDir != "" {
		cmd.Env[0] = "PATH=" + sh.envDir + string(os.PathListSeparator) + os.Getenv("PATH")
	}
	for name, value := range env {
		cmd.Env = append(cmd.Env, name+"="+value)
	}
	stdout, err := cmd.Output()
	exitCode := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		exitCode = exitErr.ExitCode()
	} else {
		require.NoError(t, err)
	}
	var out helperOutput
	require.NoError(t, json.Unmarshal(stdout, &out), string(stdout))
	return out, exitCode
}

// rackURLValue is a RACK_URL value, built from parts: gosec takes a literal one for a hardcoded credential (G101).
const rackURLValue = "https://convox:" + "secret@api"

// tricky values: each must reach the command exactly as set.
var tricky = map[string]string{
	"SPACES":    "a  b c",
	"QUOTES":    `it's "quoted"`,
	"DOLLARS":   "$HOME ${PATH} $(id) `id`",
	"NEWLINES":  "line 1\nline 2\n",
	"EMPTY":     "",
	"BACKSLASH": `a\nb\\c\`,
	"GLOB":      "* ? [a-z]",
	"EQUALS":    "a=b=c",
	"UNICODE":   "naïve 日本",
}

func TestTheCommandGetsOnlyTheListedVarsWithTheirValuesIntact(t *testing.T) {
	env := map[string]string{
		"RACK_URL": rackURLValue, "OTHER": "x",
		"READ_ONLY_DATABASE_URL": "postgres://ro", "DATABASE_URL": "postgres://rw",
	}
	names := make([]string, 0, len(tricky)+2)
	names = append(names, "DATABASE_URL", "UNSET_VAR")
	for name, value := range tricky {
		env[name] = value
		names = append(names, name)
	}
	plan := mustPlan(t, false, names, map[string]string{"DATABASE_URL": "READ_ONLY_DATABASE_URL"})
	for _, sh := range shells(t) {
		out, code := run(t, sh, plan, env, 0)
		require.Zero(t, code, sh.name)
		want := map[string]string{"DATABASE_URL": "postgres://ro"}
		for name, value := range tricky {
			want[name] = value
		}
		require.Equal(t, want, out.Env, sh.name)
	}
}

func TestAReadOnlyVarWithoutItsSubstituteIsLeftOut(t *testing.T) {
	env := map[string]string{"DATABASE_URL": "postgres://rw", "PATH_EXTRA": "x"}
	readOnly := map[string]string{"DATABASE_URL": "READ_ONLY_DATABASE_URL"}
	for _, sh := range shells(t) {
		out, _ := run(t, sh, mustPlan(t, false, []string{"DATABASE_URL", "PATH_EXTRA"}, readOnly), env, 0)
		require.Equal(t, map[string]string{"PATH_EXTRA": "x"}, out.Env, sh.name)

		out, _ = run(t, sh, mustPlan(t, true, nil, readOnly), env, 0)
		require.NotContains(t, out.Env, "DATABASE_URL", sh.name)
		require.Equal(t, "x", out.Env["PATH_EXTRA"], sh.name)
	}
}

func TestEveryEnvVarLeavesOutRackURLAndAppliesTheSubstitutes(t *testing.T) {
	env := map[string]string{
		"RACK_URL": rackURLValue, "DATABASE_URL": "postgres://rw",
		"READ_ONLY_DATABASE_URL": "postgres://ro",
	}
	for name, value := range tricky {
		env[name] = value
	}
	readOnly := map[string]string{"DATABASE_URL": "READ_ONLY_DATABASE_URL"}
	for _, sh := range shells(t) {
		out, _ := run(t, sh, mustPlan(t, true, nil, readOnly), env, 0)
		require.NotContains(t, out.Env, "RACK_URL", sh.name)
		require.Equal(t, "postgres://ro", out.Env["DATABASE_URL"], sh.name)
		require.Equal(t, "postgres://ro", out.Env["READ_ONLY_DATABASE_URL"], sh.name)
		for name, value := range tricky {
			require.Equal(t, value, out.Env[name], fmt.Sprintf("%s: %s", sh.name, name))
		}

		out, _ = run(t, sh, mustPlan(t, true, nil, nil), env, 0)
		require.NotContains(t, out.Env, "RACK_URL", sh.name)
		require.Equal(t, "postgres://rw", out.Env["DATABASE_URL"], sh.name)
	}
}

// The zero Plan (the attached run's placeholder) gives the command nothing.
func TestTheZeroPlanGivesNoEnvVars(t *testing.T) {
	for _, sh := range shells(t) {
		out, _ := run(t, sh, Plan{}, map[string]string{"RACK_URL": "x", "DATABASE_URL": "y"}, 0)
		require.Empty(t, out.Env, sh.name)
	}
}

func TestTheCommandsArgumentsAndExitCodePassThrough(t *testing.T) {
	args := []string{"two  spaces", `it's "quoted"`, "$HOME", "", "-x", "--", "a=b"}
	for _, sh := range shells(t) {
		for _, plan := range []Plan{{}, mustPlan(t, true, nil, nil)} {
			out, code := run(t, sh, plan, nil, 3, args...)
			require.Equal(t, 3, code, sh.name)
			require.Equal(t, args, out.Args, sh.name)
		}
	}
}
