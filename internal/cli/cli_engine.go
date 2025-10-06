package cli

import (
	"github.com/DocSpring/rack-gateway/internal/cli/sdk"
	convoxcli "github.com/convox/convox/pkg/cli"
	convoxsdk "github.com/convox/convox/sdk"
	"github.com/convox/stdcli"
)

// Engine wraps stdcli.Engine to inject our gateway client
type Engine struct {
	*stdcli.Engine
}

// HandlerFunc is the signature for command handlers
type HandlerFunc func(convoxsdk.Interface, *stdcli.Context) error

// NewEngine creates a new CLI engine
func NewEngine(name, version string) *Engine {
	e := &Engine{
		Engine: stdcli.New(name, version),
	}

	// Set up color tags like convox does
	e.Writer.Tags["app"] = stdcli.RenderColors(39)
	e.Writer.Tags["command"] = stdcli.RenderColors(244)
	e.Writer.Tags["dir"] = stdcli.RenderColors(246)
	e.Writer.Tags["build"] = stdcli.RenderColors(23)
	e.Writer.Tags["fail"] = stdcli.RenderColors(160)
	e.Writer.Tags["rack"] = stdcli.RenderColors(26)
	e.Writer.Tags["process"] = stdcli.RenderColors(27)
	e.Writer.Tags["release"] = stdcli.RenderColors(24)
	e.Writer.Tags["resource"] = stdcli.RenderColors(33)
	e.Writer.Tags["service"] = stdcli.RenderColors(33)
	e.Writer.Tags["setting"] = stdcli.RenderColors(246)

	return e
}

// Command registers a convox command with gateway client injection
func (e *Engine) Command(command, description string, fn HandlerFunc, opts stdcli.CommandOptions) {
	wfn := func(c *stdcli.Context) error {
		// Get current rack from our config
		rack, err := SelectedRack()
		if err != nil {
			return err
		}

		// Load gateway auth
		gatewayURL, token, err := LoadRackAuth(rack)
		if err != nil {
			return err
		}

		// Create our gateway SDK client
		client := sdk.New(gatewayURL, token)

		// Call the handler with our client
		return fn(client, c)
	}

	e.Engine.Command(command, description, wfn, opts)
}

// RegisterConvoxCommands registers all convox commands using our engine
func (e *Engine) RegisterConvoxCommands() {
	// Register ps command
	var flagApp = stdcli.StringFlag("app", "a", "app name")
	var flagAll = stdcli.BoolFlag("all", "", "show all processes")

	e.Command("ps", "list app processes", convoxcli.Ps, stdcli.CommandOptions{
		Flags:    []stdcli.Flag{flagApp, flagAll},
		Validate: stdcli.Args(0),
	})

	// TODO: Register more commands as we add them
}
