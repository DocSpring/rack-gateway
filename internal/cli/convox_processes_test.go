package cli

import (
	"strings"
	"testing"

	"github.com/convox/stdcli"
	"github.com/spf13/cobra"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
)

func TestScaleCommandUseIncludesService(t *testing.T) {
	cmd := ScaleCommand()

	if cmd.Use != "scale <service>" {
		t.Fatalf("want Use %q, got %q", "scale <service>", cmd.Use)
	}
}

func TestScaleCommandArgsValidation(t *testing.T) {
	tests := []struct {
		name    string
		flags   map[string]string
		args    []string
		wantErr bool
	}{
		{
			name:    "allows listing services without args",
			args:    nil,
			wantErr: false,
		},
		{
			name:    "rejects service arg without scaling flags",
			args:    []string{"web"},
			wantErr: true,
		},
		{
			name:    "requires service when count is set",
			flags:   map[string]string{"count": "1"},
			args:    nil,
			wantErr: true,
		},
		{
			name:    "accepts service when count is set",
			flags:   map[string]string{"count": "1"},
			args:    []string{"web"},
			wantErr: false,
		},
		{
			name:    "rejects extra args when scaling",
			flags:   map[string]string{"memory": "256"},
			args:    []string{"web", "worker"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := ScaleCommand()
			for name, value := range tt.flags {
				if err := cmd.Flags().Set(name, value); err != nil {
					t.Fatalf("set flag %s: %v", name, err)
				}
			}

			err := cmd.Args(cmd, tt.args)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Args() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCollectAllNonGlobalFlagsIncludesZeroValueInts(t *testing.T) {
	cmd := ScaleCommand()
	if err := cmd.Flags().Set("count", "0"); err != nil {
		t.Fatalf("set count flag: %v", err)
	}

	flags := collectAllNonGlobalFlags(cmd)
	if len(flags) != 1 {
		t.Fatalf("want 1 forwarded flag, got %d", len(flags))
	}

	assertFlag(t, flags[0], "count", 0)
}

func assertFlag(t *testing.T, flag *stdcli.Flag, name string, value interface{}) {
	t.Helper()

	if flag == nil {
		t.Fatal("flag should not be nil")
	}
	if flag.Name != name {
		t.Fatalf("want flag name %q, got %q", name, flag.Name)
	}
	if flag.Value != value {
		t.Fatalf("want flag value %v, got %v", value, flag.Value)
	}
}

// A command too long for the gateway's audit log is refused with the reason before any MFA prompt or process start.
func TestProcessCommandsRefuseCommandsTooLongToAudit(t *testing.T) {
	long := strings.Repeat("x", audit.MaxRecordedCommandBytes)
	for _, cmd := range []*cobra.Command{ExecCommand(), RunCommand()} {
		if err := cmd.Flags().Set("app", "myapp"); err != nil {
			t.Fatal(err)
		}
		_, _, err := setupProcessCommand(t, cmd, []string{"web", "echo", long}, cmd.Name())
		if err == nil || !strings.Contains(err.Error(), "too long for the audit log") {
			t.Fatalf("%s: want the command refused as too long, got %v", cmd.Name(), err)
		}
	}
}

// The command is measured as the gateway receives it: a header value, with line breaks as spaces and no ends.
func TestSentCommandIsTheCommandHeaderValue(t *testing.T) {
	got := sentCommand([]string{"rails", "runner", "'puts 1\nputs 2\r\n'\n"})
	if want := "rails runner 'puts 1 puts 2  '"; got != want {
		t.Fatalf("want %q, got %q", want, got)
	}
}
