// Package mcpserver registers Envy MCP tools on an MCP server instance.
package mcpserver

import (
	"context"
	"fmt"

	"github.com/fxckcode/envy/internal/mcpapi"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Register attaches all env_* tools to s, backed by svc.
func Register(s *server.MCPServer, svc *mcpapi.Service) {
	add := func(tool mcp.Tool, fn func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
		s.AddTool(tool, fn)
	}

	add(mcp.NewTool("env_list",
		mcp.WithDescription("List environment keys and metadata; secret values are redacted"),
		mcp.WithString("environment", mcp.Description("Environment name (default: development)")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		env, _ := req.GetArguments()["environment"].(string)
		res, err := svc.List(env)
		return jsonResult(res, err)
	})

	add(mcp.NewTool("env_list_environments",
		mcp.WithDescription("List environment names and sources without secret payloads"),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return jsonResult(svc.ListEnvironments(), nil)
	})

	add(mcp.NewTool("env_get_schema",
		mcp.WithDescription("Return schema types, required flags, defaults, and secret markers"),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return jsonResult(svc.GetSchema(), nil)
	})

	add(mcp.NewTool("env_check",
		mcp.WithDescription("Validate an environment; returns missing/invalid without secret values"),
		mcp.WithString("environment", mcp.Description("Environment name (default: development)")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		env, _ := req.GetArguments()["environment"].(string)
		res, err := svc.Check(env)
		return jsonResult(res, err)
	})

	add(mcp.NewTool("env_diff",
		mcp.WithDescription("Compare two environments; secrets redacted, non-secret diffs include values"),
		mcp.WithString("left", mcp.Required(), mcp.Description("Left environment")),
		mcp.WithString("right", mcp.Required(), mcp.Description("Right environment")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		left, err := req.RequireString("left")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		right, err := req.RequireString("right")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		res, err := svc.Diff(left, right)
		return jsonResult(res, err)
	})

	add(mcp.NewTool("env_exists",
		mcp.WithDescription("Return whether a key exists without revealing its value"),
		mcp.WithString("environment", mcp.Description("Environment name")),
		mcp.WithString("key", mcp.Required(), mcp.Description("Variable key")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		key, err := req.RequireString("key")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		env, _ := req.GetArguments()["environment"].(string)
		res, err := svc.Exists(env, key)
		return jsonResult(res, err)
	})

	add(mcp.NewTool("env_metadata",
		mcp.WithDescription("Return status, type, secret flag, source, and redacted value placeholder"),
		mcp.WithString("environment", mcp.Description("Environment name")),
		mcp.WithString("key", mcp.Required(), mcp.Description("Variable key")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		key, err := req.RequireString("key")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		env, _ := req.GetArguments()["environment"].(string)
		res, err := svc.Metadata(env, key)
		return jsonResult(res, err)
	})

	add(mcp.NewTool("env_read",
		mcp.WithDescription("Read a plaintext value when read_values is granted; secrets are denied by default"),
		mcp.WithString("environment", mcp.Description("Environment name")),
		mcp.WithString("key", mcp.Required(), mcp.Description("Variable key")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		key, err := req.RequireString("key")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		env, _ := req.GetArguments()["environment"].(string)
		val, err := svc.ReadValue(env, key)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return jsonResult(map[string]string{"key": key, "environment": envOrDev(env), "value": val}, nil)
	})

	add(mcp.NewTool("env_set",
		mcp.WithDescription("Set a key when permitted; secrets are never echoed in the response"),
		mcp.WithString("environment", mcp.Description("Environment name")),
		mcp.WithString("key", mcp.Required(), mcp.Description("Variable key")),
		mcp.WithString("value", mcp.Required(), mcp.Description("New value")),
		mcp.WithString("reason", mcp.Description("Reason for the change (for approval audit)")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		key, err := req.RequireString("key")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		value, err := req.RequireString("value")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		env, _ := req.GetArguments()["environment"].(string)
		reason, _ := req.GetArguments()["reason"].(string)
		res, err := svc.Set(env, key, value, reason)
		return mutationResult(res, err)
	})

	add(mcp.NewTool("env_delete",
		mcp.WithDescription("Delete a key when permitted"),
		mcp.WithString("environment", mcp.Description("Environment name")),
		mcp.WithString("key", mcp.Required(), mcp.Description("Variable key")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		key, err := req.RequireString("key")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		env, _ := req.GetArguments()["environment"].(string)
		res, err := svc.Delete(env, key)
		return mutationResult(res, err)
	})

	add(mcp.NewTool("env_copy",
		mcp.WithDescription("Copy a key between environments without exposing secret plaintext"),
		mcp.WithString("from", mcp.Required(), mcp.Description("Source environment")),
		mcp.WithString("to", mcp.Required(), mcp.Description("Target environment")),
		mcp.WithString("key", mcp.Required(), mcp.Description("Variable key")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		from, err := req.RequireString("from")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		to, err := req.RequireString("to")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		key, err := req.RequireString("key")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		res, err := svc.Copy(from, to, key)
		return mutationResult(res, err)
	})

	add(mcp.NewTool("env_generate_example",
		mcp.WithDescription("Generate example env payload with empty/default non-secret placeholders"),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return jsonResult(svc.GenerateExample(), nil)
	})

	add(mcp.NewTool("env_doctor",
		mcp.WithDescription("Run project health checks; leaked-secret findings name keys only"),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return jsonResult(svc.Doctor(), nil)
	})
}

func jsonResult(v any, err error) (*mcp.CallToolResult, error) {
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	text, mErr := mcpapi.Marshal(v)
	if mErr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("marshal: %v", mErr)), nil
	}
	return mcp.NewToolResultText(text), nil
}

func envOrDev(env string) string {
	if env == "" {
		return "development"
	}
	return env
}

func mutationResult(res mcpapi.MutationResult, err error) (*mcp.CallToolResult, error) {
	text, mErr := mcpapi.Marshal(res)
	if mErr != nil {
		return mcp.NewToolResultError(mErr.Error()), nil
	}
	if err != nil && res.Status == "" {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(text), nil
}
