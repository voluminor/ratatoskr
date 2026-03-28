package main

// // // // // // // // // //

// FlagObj represents a single CLI flag extracted from the YAML schema.
type FlagObj struct {
	Name    string
	Type    string
	Value   any
	Usage   string
	Enum    []string
	IsArray bool
	IsEnum  bool
	IsMap   bool

	// Resolved Go-level fields (populated during resolution phase)
	GoType       string // e.g. "string", "LogFormatEnum", "[]string"
	GoDefault    string // Go literal for the default value
	FlagAccessor string // dot path into Obj, e.g. "Log.Format"
	EnumType     string // e.g. "LogFormatEnum"; empty when not an enum

	// Trigger flags are CLI-only bools excluded from config file serialization
	IsTrigger bool

	// Group is the top-level branch key used for help display grouping
	Group string
}

// //

// EnumObj describes a generated enum type with its constants and parse function.
type EnumObj struct {
	TypeName  string   // e.g. "LogFormatEnum"
	Values    []string // e.g. ["text", "json"]
	GoConsts  []string // e.g. ["LogFormatText", "LogFormatJson"]
	ParseFunc string   // e.g. "ParseLogFormatEnum"
	NamesVar  string   // e.g. "logFormatEnumNames"
}

// //

// TreeLeafObj is a node in the settings struct tree.
// Branches have a non-nil Branch map; leaves represent individual fields.
type TreeLeafObj struct {
	Name      string
	Type      string
	Key       string // original YAML key
	Usage     string
	Branch    map[string]*TreeLeafObj
	IsEnum    bool
	IsArray   bool
	IsMap     bool
	IsTrigger bool
	EnumType  string

	GenInterface  bool
	InterfaceType string // e.g. "YggdrasilInterface"
}

// //

// TemplateObj is the root data structure passed to all templates.
type TemplateObj struct {
	GenerationTime  string
	Path            string
	Flags           []FlagObj
	Enums           []EnumObj
	Tree            map[string]*TreeLeafObj
	TypesImports    []string
	FlagsImports    []string
	DefaultsImports []string
	HasCustomFlags  bool
	HasEnums        bool
	HasTriggerFlags bool
	HelpText        string
}
