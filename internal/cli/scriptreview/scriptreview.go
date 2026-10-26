// Package scriptreview shows a script to the person running it, before it's sent to a rack, and asks them to
// approve it: in the terminal, or in a macOS dialog when there's no terminal (an AI agent running the CLI). The PIN
// dialog is no place to read a script: it has room for a few lines, and gives up after a few minutes. The review
// dialog can take the security key PIN instead, so approving the script and entering the PIN are one step.
package scriptreview

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/DocSpring/rack-gateway/internal/cli/macdialog"
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
	// Warnings say what the command can do that the person approving it must notice (change data, read every env
	// var). They show in red.
	Warnings []string
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
	return reviewInDialog(context.Background(), macdialog.Run, req)
}

// reviewInTerminal prints the script's listing to out, in color when color is set, and approves it when the
// answer read from in is y or yes.
func reviewInTerminal(req Request, in io.Reader, out io.Writer, color bool) error {
	var text strings.Builder
	for _, warning := range req.Warnings {
		line := "WARNING: " + oneLine(warning)
		if color {
			line = "\x1b[1;31m" + line + "\x1b[0m" // bold red
		}
		text.WriteString(line + "\n")
	}
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

// pinInstructions tell the person what the PIN in the review dialog is for.
const pinInstructions = "Enter your security key PIN to run it, then touch the key."

// enterPIN asks again for a PIN when Run was clicked without one.
const enterPIN = "Enter your security key PIN to run the script."

// reviewInDialog shows the script in a macOS dialog and approves it when the person clicks Run, after entering a
// PIN that req.CheckPIN accepts when it's set. It returns that PIN. A dialog left open for reviewTimeout (all its
// tries together) is closed, and the script isn't approved.
func reviewInDialog(ctx context.Context, run macdialog.Runner, req Request) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, reviewTimeout)
	defer cancel()
	spans, _ := listing(req.Script)
	listingJSON, err := json.Marshal(spans)
	if err != nil {
		return "", fmt.Errorf("failed to show the script review dialog: %w", err)
	}
	dialog := reviewDialog{run: run, header: Header(req), listing: string(listingJSON), warnings: warningLines(req)}
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

// reviewDialog is the review dialog's arguments: the header, the listing (its spans, as JSON), the mode ("pin" to
// ask for the security key PIN too, or "") and the warnings (one per line).
type reviewDialog struct {
	run                             macdialog.Runner
	header, listing, mode, warnings string
}

// warningLines are req's warnings for the dialog, one per line, each made safe for one line (see oneLine).
func warningLines(req Request) string {
	lines := make([]string, 0, len(req.Warnings))
	for _, warning := range req.Warnings {
		lines = append(lines, "⚠ "+oneLine(warning))
	}
	return strings.Join(lines, "\n")
}

// show shows the dialog, with problem (why it's shown again, or "") as its title. It returns the PIN entered once
// Run is clicked, or ErrDeclined.
func (d reviewDialog) show(ctx context.Context, problem string) (string, error) {
	stdout, stderr, err := d.run(ctx, dialogScript, d.header, d.listing, d.mode, problem, d.warnings)
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

// dialogScript shows an alert, with a lock icon and the header as its text, and the listing (its spans, as JSON) in
// a scrollable, read-only, monospaced text view, colored by style in the system's colors for the current appearance
// (light or dark). It prints "run" when Run is clicked. Cancel is the default button (Return), so approving takes a
// click. In "pin" mode it has a security key PIN field under the listing, which has the focus, and Run is the
// default button: the PIN is the approval. It then prints "run", a line break and the PIN. A problem (a wrong PIN)
// replaces the title. The warnings (one per line) show in red above the listing, with a red warning icon. The
// texts arrive as arguments, never as part of the script.
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

  var warnings = warningsView(argv[4], 820);
  var alert = $.NSAlert.alloc.init;
  alert.icon = alertIcon(!!warnings);
  alert.messageText = argv[3] || 'Approve this rack-gateway script?';
  alert.informativeText = argv[0];
  if (argv[2] !== 'pin') {
    alert.accessoryView = stacked([scroll, warnings]);
    alert.addButtonWithTitle('Cancel');
    alert.addButtonWithTitle('Run');
    alert.window.initialFirstResponder = view;
    return alert.runModal == $.NSAlertSecondButtonReturn ? 'run' : 'cancel';
  }

  var pinRow = $.NSView.alloc.initWithFrame($.NSMakeRect(0, 0, 820, 30));
  var label = $.NSTextField.labelWithString('Security key PIN:');
  label.setFrame($.NSMakeRect(0, 9, 120, 18));
  pinRow.addSubview(label);
  var field = $.NSSecureTextField.alloc.initWithFrame($.NSMakeRect(124, 6, 220, 24));
  pinRow.addSubview(field);
  alert.accessoryView = stacked([pinRow, scroll, warnings]);
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
` + macdialog.Helpers
