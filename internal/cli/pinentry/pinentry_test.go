package pinentry

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeDialog returns a dialogRunner that records the message it was shown and replies with the given output.
func fakeDialog(shown *string, stdout, stderr string, err error) dialogRunner {
	return func(_ context.Context, _, message string) (string, string, error) {
		*shown = message
		return stdout, stderr, err
	}
}

func TestReadFromDialogReturnsEnteredPIN(t *testing.T) {
	var shown string

	pin, err := readFromDialog(context.Background(), fakeDialog(&shown, "12 34\n", "", nil), Request{})
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

	if _, err := readFromDialog(context.Background(), fakeDialog(&shown, "1234\n", "", nil), req); err != nil {
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
	run := fakeDialog(&shown, "", "execution error: User canceled. (-128)", errors.New("exit status 1"))

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

	if _, err := readFromDialog(context.Background(), fakeDialog(&shown, "\n", "", nil), Request{}); err == nil {
		t.Fatal("expected an error for an empty PIN")
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

func TestDescribeCommandTruncatesLongCommands(t *testing.T) {
	args := []string{"rack-gateway", strings.Repeat("x", maxCommandLength*2)}

	got := describeCommand(args)
	if len(got) != maxCommandLength+len("...") || !strings.HasSuffix(got, "...") {
		t.Fatalf("describeCommand length = %d, want %d ending in ...", len(got), maxCommandLength+3)
	}
}

func TestDialogScriptActivatesBeforeShowingDialog(t *testing.T) {
	activate := strings.Index(dialogScript, "\tactivate\n")
	dialog := strings.Index(dialogScript, "display dialog")
	if activate < 0 || activate > dialog {
		t.Fatalf("dialog script must activate osascript before the dialog so the PIN field has focus:\n%s",
			dialogScript)
	}
}
