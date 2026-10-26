// Package macdialog runs the CLI's macOS dialogs: JavaScript for Automation (JXA) scripts run with osascript. A
// script gets its texts as arguments, never inside the script, so nothing shown can change what the script does.
package macdialog

import (
	"bytes"
	"context"
	"os/exec"
)

// Runner runs a JXA script with osascript, passing args to its run handler, and returns what it printed.
type Runner func(ctx context.Context, script string, args ...string) (stdout, stderr string, err error)

// Run runs script with osascript (a Runner).
func Run(ctx context.Context, script string, args ...string) (string, string, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "osascript", append([]string{"-l", "JavaScript", "-e", script}, args...)...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// Helpers is JXA the dialog scripts include (after ObjC.import('Cocoa')):
//
//   - alertIcon(warning): the alert's 48-point icon, a lock.shield symbol, or a red exclamation mark triangle when
//     warning is set, instead of osascript's folder icon.
//   - warningsView(text, width): a label showing text (the warnings, one per line) in bold red, wrapped to width
//     points, with its height fitted to the text; null when text is empty.
//   - stacked(views): a view holding views from the bottom up, 12 points apart, skipping nulls (an alert's
//     accessory view).
const Helpers = `
function alertIcon(warning) {
  var config = $.NSImageSymbolConfiguration.configurationWithPointSizeWeightScale(48, 0, 3);
  var name = 'lock.shield';
  if (warning) {
    name = 'exclamationmark.triangle.fill';
    config = config.configurationByApplyingConfiguration(
      $.NSImageSymbolConfiguration.configurationWithHierarchicalColor($.NSColor.systemRedColor));
  }
  return $.NSImage.imageWithSystemSymbolNameAccessibilityDescription(name, 'rack-gateway')
    .imageWithSymbolConfiguration(config);
}

function warningsView(text, width) {
  if (!text) {
    return null;
  }
  var label = $.NSTextField.wrappingLabelWithString(text);
  label.textColor = $.NSColor.systemRedColor;
  label.font = $.NSFont.boldSystemFontOfSize(14);
  label.preferredMaxLayoutWidth = width;
  label.setFrame($.NSMakeRect(0, 0, width, label.fittingSize.height));
  return label;
}

function stacked(views) {
  var container = $.NSView.alloc.initWithFrame($.NSMakeRect(0, 0, 0, 0));
  var width = 0;
  var y = 0;
  views.forEach(function (view) {
    if (!view) {
      return;
    }
    if (y > 0) {
      y += 12;
    }
    view.setFrameOrigin($.NSMakePoint(0, y));
    container.addSubview(view);
    y += view.frame.size.height;
    width = Math.max(width, view.frame.size.width);
  });
  container.setFrameSize($.NSMakeSize(width, y));
  return container;
}
`
