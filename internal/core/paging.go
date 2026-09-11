package core

// DefaultMax is the CLI row ceiling from ARSENAL-SPEC §5.
const DefaultMax = 1000

// MCPDefaultMax keeps tool results inside a model's context budget. Larger
// harvests belong in `mef_sync` + `mef_sql`, which the truncation hint says.
const MCPDefaultMax = 200

// PagingParams are appended to every command that walks a paginated source.
func PagingParams() []Param {
	return []Param{
		{Name: "all", Type: TypeBool, Default: false, Help: "Keep paging until the source is exhausted (bounded by max)."},
		{Name: "max", Type: TypeInt, Default: DefaultMax, Min: IntPtr(1), Max: IntPtr(1_000_000),
			Help: "Hard ceiling on rows returned, e.g. 5000. Guards against runaway harvests (MCP default 200)."},
	}
}
