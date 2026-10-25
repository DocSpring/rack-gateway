package scriptreview

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/scriptrun"
)

func testRequest(t *testing.T, text string) Request {
	t.Helper()
	script, ok := scriptrun.Parse(scriptrun.Build("count.rb", []byte(text), "rails runner"))
	require.True(t, ok)
	return Request{
		Where:         "app myapp (rack us)",
		Justification: "Count pro accounts for the pricing review",
		Script:        script,
	}
}

// The terminal shows where the script runs, the command, the justification, the script's size and SHA-256, and
// every line, numbered. Only y or yes approves it.
func TestReviewInTerminal(t *testing.T) {
	req := testRequest(t, "accounts = Account.where(plan: 'pro')\nputs accounts.count\n")
	for answer, approved := range map[string]bool{
		"y\n": true, "YES\n": true, "\n": false, "n\n": false, "yes please\n": false, "": false,
	} {
		var out bytes.Buffer
		err := reviewInTerminal(req, strings.NewReader(answer), &out, false)
		if approved {
			require.NoError(t, err, answer)
		} else {
			require.ErrorIs(t, err, ErrDeclined, answer)
		}
		require.Contains(t, out.String(), "Review this script before it runs on app myapp (rack us).")
		require.Contains(t, out.String(), "Command: rails runner count.rb")
		require.Contains(t, out.String(), "Justification: Count pro accounts for the pricing review")
		require.Contains(t, out.String(), "Script: count.rb, 2 lines, SHA-256 "+req.Script.SHA256)
		require.Contains(t, out.String(), "1 | accounts = Account.where(plan: 'pro')\n2 | puts accounts.count")
		require.True(t, strings.HasSuffix(out.String(), "Run this script? [y/N] "))
	}
}

// Characters that would make the script read differently from what runs (a right-to-left override, a zero-width
// space, a lone carriage return) are written out and counted, with a warning. CRLF line endings aren't.
func TestListingShowsHiddenCharacters(t *testing.T) {
	spans, hidden := listing(scriptrun.Script{
		Name: "check.rb", Text: "ok = true\r\nif ok \u202e} {\u2066\r\nputs\u200b 1\rputs 2\n",
	})
	require.Equal(t, 4, hidden)
	require.Equal(t,
		"1 | ok = true\n2 | if ok <U+202E>} {<U+2066>\n3 | puts<U+200B> 1<U+000D>puts 2", plainText(spans))
	require.Contains(t, spans, span{Text: "<U+202E>", Style: styleHidden})
	require.Contains(t, Header(testRequest(t, "puts\u200b 1\n")),
		"Warning: the script has 1 invisible or direction-changing characters, shown as <U+XXXX>.")
}

// The justification comes from the command line: it can't add lines to the review.
func TestHeaderKeepsCommandLineTextToOneLine(t *testing.T) {
	req := testRequest(t, "puts 1\n")
	req.Justification = "Count accounts\nCommand: rails runner harmless.rb"
	require.Contains(t, Header(req), "Justification: Count accounts?Command: rails runner harmless.rb")
}

// The dialog gets the header and the listing as arguments, never inside its script, and approves the script only
// when Run is clicked.
func TestReviewInDialog(t *testing.T) {
	req := testRequest(t, "puts 'run'\n")
	for stdout, approved := range map[string]bool{"run\n": true, "cancel\n": false, "": false} {
		var gotScript string
		var gotArgs []string
		run := func(_ context.Context, script string, args ...string) (string, string, error) {
			gotScript, gotArgs = script, args
			return stdout, "", nil
		}
		pin, err := reviewInDialog(context.Background(), run, req)
		if approved {
			require.NoError(t, err)
		} else {
			require.ErrorIs(t, err, ErrDeclined)
		}
		require.Empty(t, pin)
		require.Equal(t, dialogScript, gotScript)
		require.Equal(t, []string{Header(req), gotArgs[1], "", ""}, gotArgs)
		var spans []span
		require.NoError(t, json.Unmarshal([]byte(gotArgs[1]), &spans))
		require.Equal(t, "1 | puts 'run'", plainText(spans))
	}

	failing := func(context.Context, string, ...string) (string, string, error) {
		return "", "execution error", errors.New("exit status 1")
	}
	_, err := reviewInDialog(context.Background(), failing, req)
	require.ErrorContains(t, err, "execution error")
}

// answers is a dialog that gives each of its answers in turn, and records the problem each was asked with.
func answers(t *testing.T, stdouts ...string) (dialogRunner, *[]string) {
	t.Helper()
	var problems []string
	return func(_ context.Context, _ string, args ...string) (string, string, error) {
		require.Len(t, args, 4)
		require.Equal(t, "pin", args[2])
		require.Contains(t, args[0], pinInstructions)
		problems = append(problems, args[3])
		require.LessOrEqual(t, len(problems), len(stdouts), "the dialog was shown too often")
		return stdouts[len(problems)-1], "", nil
	}, &problems
}

// With CheckPIN, the dialog asks for the security key PIN too: Run without a PIN, or with one CheckPIN rejects,
// shows it again with the reason. The PIN it accepts is returned exactly as typed.
func TestReviewInDialogAsksForThePIN(t *testing.T) {
	req := testRequest(t, "puts 1\n")
	var checked []string
	req.CheckPIN = func(pin string) (string, error) {
		checked = append(checked, pin)
		if pin != " 1234 " {
			return "wrong security key PIN: 7 tries left before the key locks", nil
		}
		return "", nil
	}
	run, problems := answers(t, "run\n\n", "run\n123456\n", "run\n 1234 \n")
	pin, err := reviewInDialog(context.Background(), run, req)
	require.NoError(t, err)
	require.Equal(t, " 1234 ", pin)
	require.Equal(t, []string{"123456", " 1234 "}, checked)
	require.Equal(t, []string{"", enterPIN, "wrong security key PIN: 7 tries left before the key locks"}, *problems)
}

// Cancel declines the script whatever was typed, and an error from CheckPIN (a locked key) ends the review.
func TestReviewInDialogWithPINDeclinedOrFailed(t *testing.T) {
	req := testRequest(t, "puts 1\n")
	req.CheckPIN = func(string) (string, error) { return "", errors.New("the key is locked") }

	run, _ := answers(t, "cancel\n")
	pin, err := reviewInDialog(context.Background(), run, req)
	require.ErrorIs(t, err, ErrDeclined)
	require.Empty(t, pin)

	run, _ = answers(t, "run\n123456\n")
	pin, err = reviewInDialog(context.Background(), run, req)
	require.EqualError(t, err, "the key is locked")
	require.Empty(t, pin)
}

// A dialog nobody answers is closed when it times out, and the script isn't approved.
func TestReviewInDialogTimesOut(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	waiting := func(ctx context.Context, _ string, _ ...string) (string, string, error) {
		<-ctx.Done()
		return "", "", errors.New("signal: killed")
	}
	_, err := reviewInDialog(ctx, waiting, testRequest(t, "puts 1\n"))
	require.ErrorIs(t, err, ErrDeclined)
	require.ErrorContains(t, err, "the review dialog timed out")
}
