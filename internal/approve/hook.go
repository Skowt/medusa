package approve

import (
	"encoding/json"
	"fmt"
	"io"
)

// HookInput is the JSON structure Claude Code sends to PreToolUse hooks on stdin.
type HookInput struct {
	ToolName  string    `json:"tool_name"`
	ToolInput ToolInput `json:"tool_input"`
}

// ToolInput contains the tool-specific input fields.
type ToolInput struct {
	Command string `json:"command"`
}

type hookOutput struct {
	HookSpecificOutput hookSpecificOutput `json:"hookSpecificOutput"`
}

type hookSpecificOutput struct {
	HookEventName      string `json:"hookEventName"`
	PermissionDecision string `json:"permissionDecision"`
}

type denyOutput struct {
	HookSpecificOutput hookSpecificOutput `json:"hookSpecificOutput"`
	SystemMessage      string             `json:"systemMessage"`
}

// WriteAllow writes the allow JSON to w.
func WriteAllow(w io.Writer) error {
	out := hookOutput{
		HookSpecificOutput: hookSpecificOutput{
			HookEventName:      "PreToolUse",
			PermissionDecision: "allow",
		},
	}
	return json.NewEncoder(w).Encode(out)
}

// WriteDeny writes the deny JSON to w.
func WriteDeny(w io.Writer, reason string) error {
	out := denyOutput{
		HookSpecificOutput: hookSpecificOutput{
			HookEventName:      "PreToolUse",
			PermissionDecision: "deny",
		},
		SystemMessage: reason,
	}
	return json.NewEncoder(w).Encode(out)
}

// ParseHookInput parses the JSON hook input from stdin.
func ParseHookInput(data []byte) (*HookInput, error) {
	var input HookInput
	if err := json.Unmarshal(data, &input); err != nil {
		return nil, fmt.Errorf("parse hook input: %w", err)
	}
	return &input, nil
}
