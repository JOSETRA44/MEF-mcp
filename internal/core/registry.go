package core

import (
	"fmt"
	"sort"
)

// Registry holds every module and its commands, built once. Modules may
// construct fresh Command values on each Commands() call, so the registry
// snapshots them: pointer identity is then stable for the process lifetime.
type Registry struct {
	modules  []Module
	commands []*Command
	moduleOf map[*Command]string
}

// NewRegistry validates that CLI paths and tool names are unique. A duplicate
// would silently shadow a command on one surface only, so it panics at start.
func NewRegistry(modules []Module) *Registry {
	r := &Registry{modules: modules, moduleOf: map[*Command]string{}}
	paths := map[string]bool{}
	tools := map[string]bool{}
	for _, m := range modules {
		for _, c := range m.Commands() {
			if paths[c.Name()] {
				panic(fmt.Sprintf("duplicate command path %q", c.Name()))
			}
			paths[c.Name()] = true
			if c.Tool != "" {
				if tools[c.Tool] {
					panic(fmt.Sprintf("duplicate tool name %q", c.Tool))
				}
				tools[c.Tool] = true
			}
			r.commands = append(r.commands, c)
			r.moduleOf[c] = m.ID()
		}
	}
	return r
}

// Modules returns the registered modules.
func (r *Registry) Modules() []Module { return r.modules }

// Commands returns every command in registration order.
func (r *Registry) Commands() []*Command { return r.commands }

// ModuleOf returns the id of the module that owns c, or "mef" for builtins.
func (r *Registry) ModuleOf(c *Command) string {
	if id, ok := r.moduleOf[c]; ok {
		return id
	}
	return "mef"
}

// Tools returns the commands exposed over MCP, sorted by tool name.
func (r *Registry) Tools() []*Command {
	var out []*Command
	for _, c := range r.commands {
		if c.Tool != "" {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Tool < out[j].Tool })
	return out
}
