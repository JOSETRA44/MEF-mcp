package core

// InputSchema derives the MCP tool JSON Schema from the parameter
// declarations. CLI-only parameters are omitted: a model cannot pick a local
// output path, and offering it one would only invite misuse.
func (c *Command) InputSchema() map[string]any {
	props := map[string]any{}
	var required []string
	for _, p := range c.Params {
		if p.CLIOnly {
			continue
		}
		s := map[string]any{"type": string(p.Type), "description": p.Help}
		if p.Type == TypeStringList {
			s["items"] = map[string]any{"type": "string"}
		}
		if len(p.Enum) > 0 {
			s["enum"] = p.Enum
		}
		if p.Default != nil {
			s["default"] = p.Default
		}
		if p.Min != nil {
			s["minimum"] = *p.Min
		}
		if p.Max != nil {
			s["maximum"] = *p.Max
		}
		props[p.Name] = s
		if p.Required {
			required = append(required, p.Name)
		}
	}
	schema := map[string]any{
		"type":                 "object",
		"properties":           props,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

// MCPParams returns the parameters visible to MCP.
func (c *Command) MCPParams() []Param {
	var out []Param
	for _, p := range c.Params {
		if !p.CLIOnly {
			out = append(out, p)
		}
	}
	return out
}
