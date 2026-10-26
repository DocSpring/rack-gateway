package pinentry

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/DocSpring/rack-gateway/internal/cli/macdialog"
)

// fakeDialog returns a dialog runner that records the message it was shown and replies with the given output.
func fakeDialog(shown *string, stdout, stderr string, err error) macdialog.Runner {
	return func(_ context.Context, _ string, args ...string) (string, string, error) {
		*shown = args[0]
		return stdout, stderr, err
	}
}

func TestReadFromDialogReturnsEnteredPIN(t *testing.T) {
	var shown string

	pin, err := readFromDialog(context.Background(), fakeDialog(&shown, "approve\n12 34\n", "", nil), Request{})
	if err != nil {
		t.Fatalf("readFromDialog: %v", err)
	}
	if pin != "12 34" {
		t.Fatalf("pin = %q, want %q", pin, "12 34")
	}
}

func TestReadFromDialogShowsGatewayAndMaskedCommand(t *testing.T) {
	var shown string
	req := Request{
		Gateway: "https://gateway-us.example.com",
		Args:    []string{"/usr/local/bin/rack-gateway", "env", "set", "SECRET=hunter2", "-r", "us"},
	}

	if _, err := readFromDialog(context.Background(), fakeDialog(&shown, "approve\n1234\n", "", nil), req); err != nil {
		t.Fatalf("readFromDialog: %v", err)
	}

	for _, want := range []string{"Gateway: gateway-us.example.com", "Command: rack-gateway env set SECRET=*** -r us"} {
		if !strings.Contains(shown, want) {
			t.Errorf("dialog message %q does not contain %q", shown, want)
		}
	}
	if strings.Contains(shown, "hunter2") {
		t.Errorf("dialog message shows a secret value: %q", shown)
	}
}

func TestReadFromDialogCanceled(t *testing.T) {
	var shown string
	run := fakeDialog(&shown, "cancel\n", "", nil)

	_, err := readFromDialog(context.Background(), run, Request{})
	if !errors.Is(err, ErrCanceled) {
		t.Fatalf("err = %v, want ErrCanceled", err)
	}
}

func TestReadFromDialogFailure(t *testing.T) {
	var shown string
	run := fakeDialog(&shown, "", "syntax error", errors.New("exit status 1"))

	_, err := readFromDialog(context.Background(), run, Request{})
	if err == nil || errors.Is(err, ErrCanceled) {
		t.Fatalf("err = %v, want a dialog failure", err)
	}
	if !strings.Contains(err.Error(), "syntax error") {
		t.Errorf("err = %v, want the osascript error output", err)
	}
}

func TestReadFromDialogEmptyPIN(t *testing.T) {
	var shown string

	run := fakeDialog(&shown, "approve\n\n", "", nil)
	if _, err := readFromDialog(context.Background(), run, Request{}); err == nil {
		t.Fatal("expected an error for an empty PIN")
	}
}

// The warnings reach the dialog as its second argument, one per line, each kept to one line.
func TestReadFromDialogShowsTheWarnings(t *testing.T) {
	var warnings string
	run := func(_ context.Context, _ string, args ...string) (string, string, error) {
		warnings = args[1]
		return "approve\n1234\n", "", nil
	}
	req := Request{Warnings: []string{"This command can change data.", "exec reads\nevery env var."}}
	if _, err := readFromDialog(context.Background(), run, req); err != nil {
		t.Fatalf("readFromDialog: %v", err)
	}
	if want := "⚠ This command can change data.\n⚠ exec reads?every env var."; warnings != want {
		t.Fatalf("warnings = %q, want %q", warnings, want)
	}
}

func TestDescribeCommandMasksCredentialFlags(t *testing.T) {
	args := []string{"rg", "deploy-approval", "approve", "--api-token", "tok_123", "--mfa-code", "999999"}

	got := describeCommand(args)
	want := "rack-gateway deploy-approval approve --api-token *** --mfa-code ***"
	if got != want {
		t.Fatalf("describeCommand = %q, want %q", got, want)
	}
}

func TestDescribeCommandShowsValuesOfNonSecretFlags(t *testing.T) {
	commit := "0123456789abcdef0123456789abcdef01234567"
	args := []string{
		"rack-gateway", "deploy-approval", "approve", "--app=web,api", "--rack=staging", "--commit=" + commit,
		"-r", "us", "--notes", "LGTM",
	}

	got := describeCommand(args)
	want := "rack-gateway deploy-approval approve --app=web,api --rack=staging --commit=" + commit +
		" -r us --notes LGTM"
	if got != want {
		t.Fatalf("describeCommand = %q, want %q", got, want)
	}
}

func TestDescribeCommandMasksValuesOfSecretFlags(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"--api-token=tok_123"}, "--api-token=***"},
		{[]string{"--mfa-code=999999"}, "--mfa-code=***"},
		{[]string{"--password", "tok_123"}, "--password ***"},
		{[]string{"--client-secret=tok_123"}, "--client-secret=***"},
		{[]string{"--ssh-key", "tok_123", "--app", "web"}, "--ssh-key *** --app web"},
		{[]string{"--TOKEN=tok_123"}, "--TOKEN=***"},
	}
	for _, tt := range tests {
		got := describeCommand(append([]string{"rack-gateway"}, tt.args...))
		if got != "rack-gateway "+tt.want {
			t.Errorf("describeCommand(%q) = %q, want %q", tt.args, got, "rack-gateway "+tt.want)
		}
		if strings.Contains(got, "tok_123") || strings.Contains(got, "999999") {
			t.Errorf("describeCommand(%q) shows a secret value: %q", tt.args, got)
		}
	}
}

func TestDescribeCommandMasksAssignments(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"env", "set", "DB_PASSWORD=hunter2", "PORT=80"}, "env set DB_PASSWORD=*** PORT=***"},
		{[]string{"run", "web", "--env", "DB_PASSWORD=hunter2"}, "run web --env DB_PASSWORD=***"},
		{[]string{"run", "web", "--env=DB_PASSWORD=hunter2"}, "run web --env=DB_PASSWORD=***"},
		{[]string{"env", "set", "--replace=DB_PASSWORD=hunter2"}, "env set --replace=DB_PASSWORD=***"},
		{
			[]string{"run", "web", "--", "curl", "--url=https://u:hunter2@example.com", "-H", "X"},
			"run web -- curl --url=*** -H X",
		},
	}
	for _, tt := range tests {
		got := describeCommand(append([]string{"rack-gateway"}, tt.args...))
		if got != "rack-gateway "+tt.want {
			t.Errorf("describeCommand(%q) = %q, want %q", tt.args, got, "rack-gateway "+tt.want)
		}
		if strings.Contains(got, "hunter2") {
			t.Errorf("describeCommand(%q) shows a secret value: %q", tt.args, got)
		}
	}
}

func TestDescribeCommandEscapesControlCharacters(t *testing.T) {
	args := []string{
		"rack-gateway", "deploy-approval", "approve", "--notes", "ok\nGateway: evil.example.com",
		"\x1b[2J", "\u202egnp.exe", "a b",
	}

	got := describeCommand(args)
	want := `rack-gateway deploy-approval approve --notes "ok\nGateway: evil.example.com" "\x1b[2J" "\u202egnp.exe" ` +
		`"a\u2028b"`
	if got != want {
		t.Fatalf("describeCommand = %q, want %q", got, want)
	}
}

// An argument with spaces must not pass for several: "X --app staging-web" is one key to unset, not a key and an
// --app flag, so the dialog can't suggest a different app from the one the command changes.
func TestDescribeCommandQuotesArgumentsThatCouldReadAsSeveral(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{
			[]string{"env", "unset", "X --app staging-web", "--allow-protected", "-a", "prod-web"},
			`env unset "X --app staging-web" --allow-protected -a prod-web`,
		},
		{[]string{"env", "set", "X --app staging-web=hunter2"}, `env set "X --app staging-web=***"`},
		{[]string{"run", "web", "--", "rails", "runner", "puts 'hi'"}, `run web -- rails runner "puts 'hi'"`},
		{[]string{"deploy-approval", "approve", "--notes", `say "ok"`}, `deploy-approval approve --notes "say \"ok\""`},
		{[]string{"deploy-approval", "approve", "--notes", ""}, `deploy-approval approve --notes ""`},
		{[]string{"ps", "-a", "web\u00a0api"}, `ps -a "web\u00a0api"`},
		// U+2800 is printable but renders blank, so it could pass for a space between arguments.
		{[]string{"env", "unset", "X\u2800--app\u2800staging-web"}, `env unset "X\u2800--app\u2800staging-web"`},
		{[]string{"ps", "-a", "café"}, `ps -a "caf\u00e9"`},
	}
	for _, tt := range tests {
		got := describeCommand(append([]string{"rack-gateway"}, tt.args...))
		if got != "rack-gateway "+tt.want {
			t.Errorf("describeCommand(%q) = %q, want %q", tt.args, got, "rack-gateway "+tt.want)
		}
		if strings.Contains(got, "hunter2") {
			t.Errorf("describeCommand(%q) shows a secret value: %q", tt.args, got)
		}
	}
}

func TestDescribeCommandTruncatesLongCommands(t *testing.T) {
	args := []string{"rack-gateway", strings.Repeat("x", maxCommandLength*2)}

	got := describeCommand(args)
	if len(got) != maxCommandLength+len("...") || !strings.HasSuffix(got, "...") {
		t.Fatalf("describeCommand length = %d, want %d ending in ...", len(got), maxCommandLength+3)
	}
}

func TestDescribeCommandTruncatesOnCharacterBoundaries(t *testing.T) {
	got := describeCommand([]string{"rack-gateway", strings.Repeat("é", maxCommandLength*2)})

	if !utf8.ValidString(got) {
		t.Fatalf("describeCommand cut a character in half: %q", got)
	}
	if n := utf8.RuneCountInString(got); n != maxCommandLength+len("...") {
		t.Fatalf("describeCommand length = %d characters, want %d", n, maxCommandLength+3)
	}
}

func TestDialogMessageListsTheSummaryAboveTheCommand(t *testing.T) {
	req := Request{
		Gateway: "https://gateway-us.example.com",
		Args:    []string{"rack-gateway", "deploy-approval", "approve", "--app", "web,api"},
		Summary: []string{
			`web on us, commit 0123456789ab: "Deploy web"`,
			"api on us, commit 0123456789ab: \"Deploy\nGateway: evil.example.com\x1b[2J\"",
		},
	}

	got := dialogMessage(req)
	want := "Approving:\n" +
		"• web on us, commit 0123456789ab: \"Deploy web\"\n" +
		"• api on us, commit 0123456789ab: \"Deploy?Gateway: evil.example.com?[2J\"\n\n" +
		"Gateway: gateway-us.example.com\n" +
		"Command: rack-gateway deploy-approval approve --app web,api"
	if !strings.HasSuffix(got, want) {
		t.Fatalf("dialog message = %q, want it to end with %q", got, want)
	}
	if n := strings.Count(got, "\nGateway: "); n != 1 {
		t.Fatalf("dialog message has %d Gateway lines, want 1: %q", n, got)
	}
}

func TestDialogMessageWithoutSummary(t *testing.T) {
	got := dialogMessage(Request{Gateway: "https://gateway-us.example.com", Args: []string{"rack-gateway", "ps"}})

	if strings.Contains(got, "Approving:") {
		t.Fatalf("dialog message lists an empty summary: %q", got)
	}
}

func TestDescribeSummaryCutsLongLines(t *testing.T) {
	got := describeSummary([]string{strings.Repeat("a", maxLineLength*3)})

	want := "• " + strings.Repeat("a", maxLineLength) + "..."
	if got != want {
		t.Fatalf("describeSummary = %q, want %q", got, want)
	}
}

func TestDescribeSummaryCountsLinesPastTheLimit(t *testing.T) {
	lines := make([]string, 0, maxSummaryLines+8)
	for i := 0; i < maxSummaryLines+8; i++ {
		lines = append(lines, fmt.Sprintf("approval %d", i+1))
	}

	got := strings.Split(describeSummary(lines), "\n")
	if len(got) != maxSummaryLines {
		t.Fatalf("describeSummary has %d lines, want %d: %q", len(got), maxSummaryLines, got)
	}
	if last := got[len(got)-1]; last != "• ...and 9 more" {
		t.Fatalf("last summary line = %q, want the number of hidden lines", last)
	}

	exact := strings.Split(describeSummary(lines[:maxSummaryLines]), "\n")
	if len(exact) != maxSummaryLines || strings.Contains(exact[len(exact)-1], "more") {
		t.Fatalf("a summary of exactly %d lines must be shown in full: %q", maxSummaryLines, exact)
	}
}

func TestDialogScriptActivatesBeforeShowingDialog(t *testing.T) {
	activate := strings.Index(dialogScript, "app.activateIgnoringOtherApps(true);")
	dialog := strings.Index(dialogScript, "alert.runModal")
	if activate < 0 || activate > dialog {
		t.Fatalf("dialog script must activate osascript before the dialog so the PIN field has focus:\n%s",
			dialogScript)
	}
}
