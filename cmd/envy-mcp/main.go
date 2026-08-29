package main

import (
	"fmt"
	"os"

	"github.com/fxckcode/envy/internal/mcpapi"
	"github.com/fxckcode/envy/internal/mcpserver"
	"github.com/fxckcode/envy/internal/project"
	"github.com/mark3labs/mcp-go/server"
)

func main() {
	root := os.Getenv("ENVY_ROOT")
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "envy-mcp: %v\n", err)
			os.Exit(1)
		}
	}
	actor := os.Getenv("ENVY_AGENT")
	if actor == "" {
		actor = "default"
	}

	proj, err := project.Open(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "envy-mcp: open project: %v\n", err)
		os.Exit(1)
	}

	svc := mcpapi.New(proj, actor)
	s := server.NewMCPServer(
		"envy",
		"0.2.0",
		server.WithToolCapabilities(true),
		server.WithRecovery(),
	)
	mcpserver.Register(s, svc)

	if err := server.ServeStdio(s); err != nil {
		fmt.Fprintf(os.Stderr, "envy-mcp: %v\n", err)
		os.Exit(1)
	}
}
