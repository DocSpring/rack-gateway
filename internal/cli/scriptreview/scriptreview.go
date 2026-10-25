// Package scriptreview shows a script to the person running it, before it's sent to a rack, and asks them to
// approve it: in the terminal, or in a macOS dialog when there's no terminal (an AI agent running the CLI). The PIN
// dialog is no place to read a script: it has room for a few lines, and gives up after a few minutes.
package scriptreview

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/DocSpring/rack-gateway/internal/cli/terminal"
	"github.com/DocSpring/rack-gateway/internal/scriptrun"
	"github.com/DocSpring/rack-gateway/internal/util/textcheck"
)

// Request describes the script to review.
type Request struct {
	// Where names the app and rack it runs on.
	Where string
	// Justification says why it's run.
	Justification string
	// Script is the script and the command that runs it, as the gateway will record them.
	Script scriptrun.Script
}

// ErrDeclined is a script the person didn't approve.
var ErrDeclined = errors.New("the script wasn't approved to run")

// reviewTimeout bounds how long the review dialog waits: long enough to read a script, short enough that a command
// left waiting doesn't run long after it was asked for.
const reviewTimeout = 15 * time.Minute

// Review shows req's script and returns nil once the person approves it, or ErrDeclined.
func Review(req Request) error {
	if _, ok := terminal.StdinFD(); ok {
		color := terminal.StderrIsTerminal() && os.Getenv("NO_COLOR") == ""
		return reviewInTerminal(req, os.Stdin, os.Stderr, color)
	}
	if runtime.GOOS != "darwin" {
		return errors.New("a script needs approving before it runs, and there's no terminal to show it in: run the " +
			"command in a terminal")
	}
	_, _ = fmt.Fprintln(os.Stderr, "Waiting for the script to be approved in the rack-gateway dialog...")
	return reviewInDialog(context.Background(), runOsascript, req)
}

// reviewInTerminal prints the script's listing to out, in color when color is set, and approves it when the
// answer read from in is y or yes.
func reviewInTerminal(req Request, in io.Reader, out io.Writer, color bool) error {
	var text strings.Builder
	text.WriteString(Header(req) + "\n\n")
	spans, _ := listing(req.Script)
	if color {
		text.WriteString(ansiText(spans))
	} else {
		text.WriteString(plainText(spans))
	}
	text.WriteString("\n\nRun this script? [y/N] ")
	if _, err := io.WriteString(out, text.String()); err != nil {
		return err
	}
	answer, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("failed to read the answer: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return nil
	default:
		return ErrDeclined
	}
}

// Header describes the script run: where, the command, the justification and the script's name, size and
// SHA-256, one item per line. Text that comes from the command line can't add lines or reorder what's shown.
func Header(req Request) string {
	script := req.Script
	lines := []string{
		"Review this script before it runs on " + oneLine(req.Where) + ".",
		"Command: " + oneLine(script.Command()),
	}
	if req.Justification != "" {
		lines = append(lines, "Justification: "+oneLine(req.Justification))
	}
	lines = append(lines, fmt.Sprintf("Script: %s, %d lines, SHA-256 %s", script.Name, script.Lines(), script.SHA256))
	if hidden := hiddenCount(script.Text); hidden > 0 {
		lines = append(lines, fmt.Sprintf("Warning: the script has %d invisible or direction-changing characters, "+
			"shown as <U+XXXX>. Check what each one is for.", hidden))
	}
	return strings.Join(lines, "\n")
}

// oneLine makes text from the command line safe for one line: hidden characters (see textcheck.Hidden), line
// breaks and tabs become '?'.
func oneLine(text string) string {
	return strings.Map(func(r rune) rune {
		if textcheck.Hidden(r) || r == '\n' || r == '\t' {
			return '?'
		}
		return r
	}, text)
}

// dialogRunner runs a JavaScript for Automation script with osascript, passing args to its run handler.
type dialogRunner func(ctx context.Context, script string, args ...string) (stdout, stderr string, err error)

// reviewInDialog shows the script in a macOS dialog and approves it when the person clicks Run. A dialog left
// open for reviewTimeout is closed, and the script isn't approved.
func reviewInDialog(ctx context.Context, run dialogRunner, req Request) error {
	ctx, cancel := context.WithTimeout(ctx, reviewTimeout)
	defer cancel()
	spans, _ := listing(req.Script)
	listingJSON, err := json.Marshal(spans)
	if err != nil {
		return fmt.Errorf("failed to show the script review dialog: %w", err)
	}
	stdout, stderr, err := run(ctx, dialogScript, Header(req), string(listingJSON))
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("%w: the review dialog timed out", ErrDeclined)
	}
	if err != nil {
		return fmt.Errorf("failed to show the script review dialog: %w: %s", err, strings.TrimSpace(stderr))
	}
	if strings.TrimSpace(stdout) != "run" {
		return ErrDeclined
	}
	return nil
}

func runOsascript(ctx context.Context, script string, args ...string) (string, string, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "osascript", append([]string{"-l", "JavaScript", "-e", script}, args...)...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// dialogScript shows an alert with the header as its text and the listing (its spans, as JSON) in a scrollable,
// read-only, monospaced text view, colored by style in the system's colors for the current appearance (light or
// dark). It prints "run" when Run is clicked. Cancel is the default button (Return), so approving takes a click.
// The texts arrive as arguments, never as part of the script.
const dialogScript = `
ObjC.import('Cocoa');
function run(argv) {
  var app = $.NSApplication.sharedApplication;
  app.setActivationPolicy($.NSApplicationActivationPolicyAccessory);
  app.activateIgnoringOtherApps(true);

  var scroll = $.NSScrollView.alloc.initWithFrame($.NSMakeRect(0, 0, 820, 460));
  scroll.hasVerticalScroller = true;
  scroll.hasHorizontalScroller = true;
  scroll.borderType = $.NSBezelBorder;

  var view = $.NSTextView.alloc.initWithFrame($.NSMakeRect(0, 0, 820, 460));
  view.editable = false;
  view.selectable = true;
  view.horizontallyResizable = true;
  view.maxSize = $.NSMakeSize(1e7, 1e7);
  view.textContainer.widthTracksTextView = false;
  view.textContainer.containerSize = $.NSMakeSize(1e7, 1e7);
  view.textStorage.setAttributedString(listing(JSON.parse(argv[1])));
  scroll.documentView = view;

  var alert = $.NSAlert.alloc.init;
  alert.messageText = 'Approve this rack-gateway script?';
  alert.informativeText = argv[0];
  alert.accessoryView = scroll;
  alert.addButtonWithTitle('Cancel');
  alert.addButtonWithTitle('Run');
  alert.window.initialFirstResponder = view;
  return alert.runModal == $.NSAlertSecondButtonReturn ? 'run' : 'cancel';
}

function listing(spans) {
  var font = $.NSFont.monospacedSystemFontOfSizeWeight(12, 0);
  var bold = $.NSFont.monospacedSystemFontOfSizeWeight(12, 0.4);
  var colors = {
    line: $.NSColor.tertiaryLabelColor,
    comment: $.NSColor.secondaryLabelColor,
    keyword: $.NSColor.systemPinkColor,
    string: $.NSColor.systemOrangeColor,
    symbol: $.NSColor.systemGreenColor,
    number: $.NSColor.systemPurpleColor,
    type: $.NSColor.systemTealColor,
    function: $.NSColor.systemBlueColor,
    hidden: $.NSColor.whiteColor
  };
  var text = $.NSMutableAttributedString.alloc.init;
  spans.forEach(function (span) {
    var attributes = $.NSMutableDictionary.dictionary;
    attributes.setObjectForKey(span.s === 'hidden' ? bold : font, $.NSFontAttributeName);
    attributes.setObjectForKey(colors[span.s] || $.NSColor.labelColor, $.NSForegroundColorAttributeName);
    if (span.s === 'hidden') {
      attributes.setObjectForKey($.NSColor.systemRedColor, $.NSBackgroundColorAttributeName);
    }
    // JXA doesn't bridge initWithString:attributes:, so each piece is filled in, then styled.
    var piece = $.NSMutableAttributedString.alloc.init;
    piece.replaceCharactersInRangeWithString($.NSMakeRange(0, 0), span.t);
    piece.setAttributesRange(attributes, $.NSMakeRange(0, piece.length));
    text.appendAttributedString(piece);
  });
  return text;
}
`
