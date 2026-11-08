package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/pflag"
)

const rackFlagHelp = "Rack to use. Deploy approval commands take several: repeat --rack, " +
	"give a comma-separated list, or all"

// rackFlagValue is the global --rack flag. A repeated flag adds to the list instead of replacing it, so
// `--rack us --rack eu` selects the same racks as `--rack us,eu`. Deploy approval commands act on each rack
// (resolveRacks), and commands that use one rack refuse a list (SelectedRack) instead of quietly using the last.
type rackFlagValue struct {
	value *string
	set   bool
}

// addRackFlag registers --rack (-r) on flags, storing the selection in target.
func addRackFlag(flags *pflag.FlagSet, target *string) {
	*target = ""
	flags.VarP(&rackFlagValue{value: target}, "rack", "r", rackFlagHelp)
}

func (r *rackFlagValue) String() string {
	if r.value == nil {
		return ""
	}
	return *r.value
}

func (r *rackFlagValue) Set(value string) error {
	if r.set {
		*r.value += "," + value
	} else {
		*r.value = value
		r.set = true
	}
	return nil
}

func (*rackFlagValue) Type() string {
	return "string"
}

// parseRackList splits a rack selection on commas, dropping blanks and repeats and keeping the order given.
func parseRackList(raw string) ([]string, error) {
	parts := strings.Split(raw, ",")
	racks := make([]string, 0, len(parts))
	seen := make(map[string]bool, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value != "" && !seen[value] {
			seen[value] = true
			racks = append(racks, value)
		}
	}
	if len(racks) == 0 {
		return nil, fmt.Errorf("no valid rack names provided")
	}
	return racks, nil
}
