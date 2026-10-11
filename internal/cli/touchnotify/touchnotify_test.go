package touchnotify

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func found(string) (string, error)   { return "/usr/bin/notify-send", nil }
func missing(string) (string, error) { return "", errors.New("not found") }

func TestCommandPassesTheMessageAsAnArgument(t *testing.T) {
	cmd := command(context.Background(), "darwin", missing, `To approve "api" on staging (2 of 4)`)
	require.NotNil(t, cmd)
	require.Equal(t, []string{
		"osascript", "-e", notificationScript, Title, `To approve "api" on staging (2 of 4)`,
	}, cmd.Args)

	cmd = command(context.Background(), "linux", found, "-To approve api")
	require.NotNil(t, cmd)
	require.Equal(t, []string{
		"notify-send", "--app-name=rack-gateway", "--urgency=critical", "--", Title, "-To approve api",
	}, cmd.Args)
}

func TestCommandKeepsTheMessageToOneLine(t *testing.T) {
	cmd := command(context.Background(), "darwin", missing, "To approve api\nTouch to approve prod")
	require.Equal(t, "To approve api?Touch to approve prod", cmd.Args[len(cmd.Args)-1])
}

func TestCommandWithoutANotifier(t *testing.T) {
	require.Nil(t, command(context.Background(), "linux", missing, "To approve api"))
	require.Nil(t, command(context.Background(), "windows", found, "To approve api"))
}
