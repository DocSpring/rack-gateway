package scriptreview

import (
	"bytes"
	"context"
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
		err := reviewInTerminal(req, strings.NewReader(answer), &out)
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
	listing, hidden := Listing("ok = true\r\nif ok \u202e} {\u2066\r\nputs\u200b 1\rputs 2\n")
	require.Equal(t, 4, hidden)
	require.Equal(t,
		"1 | ok = true\n2 | if ok <U+202E>} {<U+2066>\n3 | puts<U+200B> 1<U+000D>puts 2", listing)
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
		err := reviewInDialog(context.Background(), run, req)
		if approved {
			require.NoError(t, err)
		} else {
			require.ErrorIs(t, err, ErrDeclined)
		}
		require.Equal(t, dialogScript, gotScript)
		require.Equal(t, []string{Header(req), "1 | puts 'run'"}, gotArgs)
	}

	failing := func(context.Context, string, ...string) (string, string, error) {
		return "", "execution error", errors.New("exit status 1")
	}
	require.ErrorContains(t, reviewInDialog(context.Background(), failing, req), "execution error")
}

// A dialog nobody answers is closed when it times out, and the script isn't approved.
func TestReviewInDialogTimesOut(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	waiting := func(ctx context.Context, _ string, _ ...string) (string, string, error) {
		<-ctx.Done()
		return "", "", errors.New("signal: killed")
	}
	err := reviewInDialog(ctx, waiting, testRequest(t, "puts 1\n"))
	require.ErrorIs(t, err, ErrDeclined)
	require.ErrorContains(t, err, "the review dialog timed out")
}
