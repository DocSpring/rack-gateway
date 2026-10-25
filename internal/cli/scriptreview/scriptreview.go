// Package scriptreview shows a script to the person running it, before it's sent to a rack, and asks them to
// approve it: in the terminal, or in a macOS dialog when there's no terminal (an AI agent running the CLI). The PIN
// dialog is no place to read a script: it has room for a few lines, and gives up after a few minutes. The review
// dialog can take the security key PIN instead, so approving the script and entering the PIN are one step.
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
	// CheckPIN, when set, has the dialog ask for the security key PIN as well, and checks each PIN entered. A
	// terminal ignores it: the PIN is asked for there when the command runs.
	CheckPIN PINCheck
}

// PINCheck checks a PIN entered in the review dialog. again is a reason to ask for the PIN again (a wrong PIN);
// err ends the review.
type PINCheck func(pin string) (again string, err error)

// ErrDeclined is a script the person didn't approve.
var ErrDeclined = errors.New("the script wasn't approved to run")

// reviewTimeout bounds how long the review dialog waits: long enough to read a script, short enough that a command
// left waiting doesn't run long after it was asked for.
const reviewTimeout = 15 * time.Minute

// Review shows req's script and returns once the person approves it, with the PIN they entered when the dialog
// asked for one (see Request.CheckPIN), or "". A script that isn't approved returns ErrDeclined.
func Review(req Request) (string, error) {
	if _, ok := terminal.StdinFD(); ok {
		color := terminal.StderrIsTerminal() && os.Getenv("NO_COLOR") == ""
		return "", reviewInTerminal(req, os.Stdin, os.Stderr, color)
	}
	if runtime.GOOS != "darwin" {
		return "", errors.New("a script needs approving before it runs, and there's no terminal to show it in: run " +
			"the command in a terminal")
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

// pinInstructions tell the person what the PIN in the review dialog is for.
const pinInstructions = "Enter your security key PIN to run it. You'll touch the key once the command starts."

// enterPIN asks again for a PIN when Run was clicked without one.
const enterPIN = "Enter your security key PIN to run the script."

// reviewInDialog shows the script in a macOS dialog and approves it when the person clicks Run, after entering a
// PIN that req.CheckPIN accepts when it's set. It returns that PIN. A dialog left open for reviewTimeout (all its
// tries together) is closed, and the script isn't approved.
func reviewInDialog(ctx context.Context, run dialogRunner, req Request) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, reviewTimeout)
	defer cancel()
	spans, _ := listing(req.Script)
	listingJSON, err := json.Marshal(spans)
	if err != nil {
		return "", fmt.Errorf("failed to show the script review dialog: %w", err)
	}
	dialog := reviewDialog{run: run, header: Header(req), listing: string(listingJSON)}
	if req.CheckPIN == nil {
		_, err := dialog.show(ctx, "")
		return "", err
	}
	dialog.header += "\n\n" + pinInstructions
	dialog.mode = "pin"
	for problem := ""; ; {
		pin, err := dialog.show(ctx, problem)
		if err != nil {
			return "", err
		}
		if pin == "" {
			problem = enterPIN
			continue
		}
		if problem, err = req.CheckPIN(pin); err != nil {
			return "", err
		}
		if problem == "" {
			return pin, nil
		}
	}
}

// reviewDialog is the review dialog's arguments: the header, the listing (its spans, as JSON) and the mode ("pin"
// to ask for the security key PIN too, or "").
type reviewDialog struct {
	run                   dialogRunner
	header, listing, mode string
}

// show shows the dialog, with problem (why it's shown again, or "") as its title. It returns the PIN entered once
// Run is clicked, or ErrDeclined.
func (d reviewDialog) show(ctx context.Context, problem string) (string, error) {
	stdout, stderr, err := d.run(ctx, dialogScript, d.header, d.listing, d.mode, problem)
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "", fmt.Errorf("%w: the review dialog timed out", ErrDeclined)
	}
	if err != nil {
		return "", fmt.Errorf("failed to show the script review dialog: %w: %s", err, strings.TrimSpace(stderr))
	}
	answer, pin, _ := strings.Cut(strings.TrimSuffix(stdout, "\n"), "\n")
	if answer != "run" {
		return "", ErrDeclined
	}
	return pin, nil
}

func runOsascript(ctx context.Context, script string, args ...string) (string, string, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "osascript", append([]string{"-l", "JavaScript", "-e", script}, args...)...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// dialogScript shows an alert, with a lock icon and the header as its text, and the listing (its spans, as JSON) in
// a scrollable, read-only, monospaced text view, colored by style in the system's colors for the current appearance
// (light or dark). It prints "run" when Run is clicked. Cancel is the default button (Return), so approving takes a
// click. In "pin" mode it has a security key PIN field under the listing, which has the focus, and Run is the
// default button: the PIN is the approval. It then prints "run", a line break and the PIN. A problem (a wrong PIN)
// replaces the title. The texts arrive as arguments, never as part of the script.
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
  // A 48-point lock.shield symbol, regular weight (0), large scale (3), instead of osascript's folder icon.
  alert.icon = $.NSImage.imageWithSystemSymbolNameAccessibilityDescription('lock.shield', 'rack-gateway')
    .imageWithSymbolConfiguration($.NSImageSymbolConfiguration.configurationWithPointSizeWeightScale(48, 0, 3));
  alert.messageText = argv[3] || 'Approve this rack-gateway script?';
  alert.informativeText = argv[0];
  if (argv[2] !== 'pin') {
    alert.accessoryView = scroll;
    alert.addButtonWithTitle('Cancel');
    alert.addButtonWithTitle('Run');
    alert.window.initialFirstResponder = view;
    return alert.runModal == $.NSAlertSecondButtonReturn ? 'run' : 'cancel';
  }

  var content = $.NSView.alloc.initWithFrame($.NSMakeRect(0, 0, 820, 500));
  scroll.setFrameOrigin($.NSMakePoint(0, 40));
  content.addSubview(scroll);
  var label = $.NSTextField.labelWithString('Security key PIN:');
  label.setFrame($.NSMakeRect(0, 9, 120, 18));
  content.addSubview(label);
  var field = $.NSSecureTextField.alloc.initWithFrame($.NSMakeRect(124, 6, 220, 24));
  content.addSubview(field);
  alert.accessoryView = content;
  alert.addButtonWithTitle('Run');
  alert.addButtonWithTitle('Cancel');
  alert.window.initialFirstResponder = field;
  if (alert.runModal != $.NSAlertFirstButtonReturn) {
    return 'cancel';
  }
  return 'run\n' + field.stringValue.js;
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
