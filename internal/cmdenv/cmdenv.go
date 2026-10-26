// Package cmdenv decides the environment of a command run through the gateway (an exec's, or a process start's)
// and wraps the command so it runs with only that environment.
//
// A command travels as one header line, and the rack splits it into words the way a shell would (go-shellquote)
// and runs them with no shell, in a process that has every env var of the app's release (see scriptrun). The
// gateway sends the command inside a fixed sh script that starts it with only the env vars it may have:
//
//	sh -c 'exec env -i -- ${A+"A=$A"} ${RO+"DATABASE_URL=$RO"} "$@"' rack-gateway-env <command words...>
//
// Each name the command gets is passed on only when it's set, with its value untouched; a read-only var takes the
// value of its substitute instead. A command given every env var drops RACK_URL and the read-only vars, then adds
// the substitutes back:
//
//	sh -c 'unset RACK_URL DATABASE_URL; exec env -- ${RO+"DATABASE_URL=$RO"} "$@"' rack-gateway-env <command words...>
//
// The image needs sh and env.
package cmdenv

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/kballard/go-shellquote"
)

// RackURL is the env var Convox gives every process of every app: the rack API's URL, with the rack's API password
// in it. A command holding it could use the rack's API without the gateway, so no command gets it, and no setting
// or flag can give it one.
const RackURL = "RACK_URL"

// argv0 names the wrapper in process listings ($0).
const argv0 = "rack-gateway-env"

// Plan is the environment a command gets. The zero Plan gives it no env vars at all.
type Plan struct {
	// All gives the command every env var its process has, except RACK_URL. Otherwise it gets only Vars.
	All bool
	// Vars are the env vars the command gets when All is false, sorted.
	Vars []string
	// ReadOnly maps an env var the command gets to the env var it takes its value from instead (DATABASE_URL to
	// READ_ONLY_DATABASE_URL, say). When the substitute isn't set, the command doesn't get the var at all.
	ReadOnly map[string]string
	// Added are the Vars the command gets besides the ones every command in its app gets (the ones it asked for),
	// sorted. They only change how Summary describes the plan.
	Added []string
}

// New returns the plan that gives a command every env var (all), or only vars, with the substitutes in readOnly
// applied to the vars it gets. It refuses a name that isn't a valid env var name, RACK_URL anywhere, and a
// substitute that is itself replaced by one.
func New(all bool, vars []string, readOnly map[string]string) (Plan, error) {
	if err := CheckReadOnly(readOnly); err != nil {
		return Plan{}, err
	}
	plan := Plan{All: all}
	if !all {
		if err := CheckNames(vars); err != nil {
			return Plan{}, err
		}
		plan.Vars = sortedUnique(vars)
	}
	for name, source := range readOnly {
		if plan.gets(name) {
			if plan.ReadOnly == nil {
				plan.ReadOnly = map[string]string{}
			}
			plan.ReadOnly[name] = source
		}
	}
	return plan, nil
}

// gets reports whether the command gets the env var name (from its own value or a substitute's).
func (p Plan) gets(name string) bool {
	if p.All {
		return true
	}
	i := sort.SearchStrings(p.Vars, name)
	return i < len(p.Vars) && p.Vars[i] == name
}

// CheckNames refuses a list of env var names with one that isn't a valid env var name, or RACK_URL.
func CheckNames(names []string) error {
	for _, name := range names {
		if err := checkName(name); err != nil {
			return err
		}
	}
	return nil
}

// CheckReadOnly refuses read-only substitutes (var to substitute) with a name that isn't a valid env var name,
// RACK_URL, or a substitute that is itself replaced: the substitutes all read the process's own values.
func CheckReadOnly(readOnly map[string]string) error {
	for _, name := range sortedKeys(readOnly) {
		source := readOnly[name]
		if err := CheckNames([]string{name, source}); err != nil {
			return err
		}
		if name == source {
			return fmt.Errorf("%s can't be its own read-only substitute", name)
		}
		if _, replaced := readOnly[source]; replaced {
			return fmt.Errorf("%s is the read-only substitute for %s, so it can't have one of its own", source, name)
		}
	}
	return nil
}

func checkName(name string) error {
	if !ValidName(name) {
		return fmt.Errorf("%q isn't an env var name (letters, digits and underscores, not starting with a digit)",
			name)
	}
	if name == RackURL {
		return fmt.Errorf("no command can have %s: it holds the rack's API password", RackURL)
	}
	return nil
}

// Build returns the command the rack runs to run command with plan's environment. plan must be one New returns,
// or the zero Plan; command must be one CheckCommand accepts.
func Build(plan Plan, command string) string {
	return "sh -c '" + plan.script() + "' " + argv0 + " " + command
}

// script is the sh script that runs the command words ("$@") with the plan's environment. It has no single quotes,
// so it can be single-quoted as one word.
func (p Plan) script() string {
	if !p.All {
		return "exec env -i -- " + p.assignments(p.Vars) + `"$@"`
	}
	replaced := sortedKeys(p.ReadOnly)
	return "unset " + strings.Join(append([]string{RackURL}, replaced...), " ") + "; exec env -- " +
		p.assignments(replaced) + `"$@"`
}

// assignments passes each of names to env as NAME=value when its source (the name, or its read-only substitute) is
// set. The value is double-quoted, so it stays one word whatever it holds.
func (p Plan) assignments(names []string) string {
	var b strings.Builder
	for _, name := range names {
		source := name
		if substitute, ok := p.ReadOnly[name]; ok {
			source = substitute
		}
		fmt.Fprintf(&b, `${%s+"%s=$%s"} `, source, name, source)
	}
	return b.String()
}

var (
	errEmptyCommand    = errors.New("the command is empty")
	errAssignmentFirst = errors.New("the command can't start with NAME=value: a command gets env vars from the " +
		"gateway (see --env)")
)

// CheckCommand refuses a command the gateway can't wrap: one the rack can't split into words, one with no words
// (a process start without a command would run the service's own command, with every env var), and one starting
// with NAME=value, which env would read as an env var of its own rather than the program to run.
func CheckCommand(command string) error {
	words, err := shellquote.Split(command)
	if err != nil {
		return fmt.Errorf("the rack can't split the command into words: %w", err)
	}
	if len(words) == 0 {
		return errEmptyCommand
	}
	if strings.Contains(words[0], "=") {
		return errAssignmentFirst
	}
	return nil
}

const (
	namePattern  = `[A-Za-z_][A-Za-z0-9_]*`
	scriptPrefix = "sh -c '"
	scriptEnd    = "' " + argv0 + " "
)

var (
	validName  = regexp.MustCompile(`^` + namePattern + `$`)
	assignment = regexp.MustCompile(`^\$\{(` + namePattern + `)\+"(` + namePattern + `)=\$(` + namePattern + `)"\} `)
	unsetLine  = regexp.MustCompile(`^unset ` + RackURL + `((?: ` + namePattern + `)*); exec env -- `)
)

// ValidName reports whether name is a valid env var name: letters, digits and underscores, not starting with a
// digit.
func ValidName(name string) bool {
	return validName.MatchString(name)
}

// Parse reads a command Build made back into its plan and the command it runs. ok is false for anything else.
func Parse(wrapped string) (plan Plan, command string, ok bool) {
	rest, found := strings.CutPrefix(wrapped, scriptPrefix)
	if !found {
		return Plan{}, "", false
	}
	script, command, found := strings.Cut(rest, scriptEnd)
	if !found {
		return Plan{}, "", false
	}
	parsed, ok := parseScript(script)
	if !ok {
		return Plan{}, "", false
	}
	// New sorts the names and checks them, so only the wrapper Build makes for a valid plan reads back.
	plan, err := New(parsed.All, parsed.Vars, parsed.ReadOnly)
	if err != nil || Build(plan, command) != wrapped {
		return Plan{}, "", false
	}
	return plan, command, true
}

// parseScript reads a script Plan.script made back into its plan.
func parseScript(script string) (Plan, bool) {
	var plan Plan
	if rest, found := strings.CutPrefix(script, "exec env -i -- "); found {
		script = rest
	} else if m := unsetLine.FindStringSubmatch(script); m != nil {
		plan.All = true
		script = script[len(m[0]):]
	} else {
		return Plan{}, false
	}
	for {
		m := assignment.FindStringSubmatch(script)
		if m == nil {
			return plan, script == `"$@"`
		}
		if !plan.add(m[1], m[2], m[3]) {
			return Plan{}, false
		}
		script = script[len(m[0]):]
	}
}

// add adds an assignment ${source+"target=$value"} the script passes to env, or reports false when its value isn't
// its source.
func (p *Plan) add(source, target, value string) bool {
	if value != source {
		return false
	}
	if !p.All {
		p.Vars = append(p.Vars, target)
	}
	if source != target {
		if p.ReadOnly == nil {
			p.ReadOnly = map[string]string{}
		}
		p.ReadOnly[target] = source
	}
	return true
}

// Describe says what env vars the plan gives a command, for a person: "every env var except RACK_URL", or the
// names, each read-only one with its substitute.
func (p Plan) Describe() string {
	if p.All {
		description := "every env var except " + RackURL
		if len(p.ReadOnly) > 0 {
			description += "; read-only " + strings.Join(p.described(sortedKeys(p.ReadOnly)), ", ")
		}
		return description
	}
	if len(p.Vars) == 0 {
		return "no env vars"
	}
	return strings.Join(p.described(p.Vars), ", ")
}

// Summary describes the plan in a line, for the person approving the command or reading about it: how many of the
// app's command_env_vars it gets, the ones added for it, and the read-only ones. Describe names every env var.
func (p Plan) Summary() string {
	if p.All || len(p.Vars) == 0 {
		return p.Describe()
	}
	summary := fmt.Sprintf("the %d in the app's command_env_vars", len(p.Vars)-len(p.Added))
	if len(p.Added) > 0 {
		summary += ", plus " + strings.Join(p.Added, ", ")
	}
	if len(p.ReadOnly) > 0 {
		summary += "; read-only " + strings.Join(p.described(sortedKeys(p.ReadOnly)), ", ")
	}
	return summary
}

// described names each of names, with its read-only substitute: "DATABASE_URL (from READ_ONLY_DATABASE_URL)".
func (p Plan) described(names []string) []string {
	described := make([]string, 0, len(names))
	for _, name := range names {
		if source, ok := p.ReadOnly[name]; ok {
			name += " (from " + source + ")"
		}
		described = append(described, name)
	}
	return described
}

func sortedUnique(names []string) []string {
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	unique := sorted[:0]
	for i, name := range sorted {
		if i == 0 || name != sorted[i-1] {
			unique = append(unique, name)
		}
	}
	if len(unique) == 0 {
		return nil
	}
	return unique
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
