package clients

import (
	"github.com/invopop/jsonschema"
)

// JSONSchema publishes the category vocabulary as an enum.
//
// Core reflects the config struct into the schema that drives the REST body,
// the UI form and the MCP tool definition (design.md §3.2 rule 3). Core can see
// that Category marshals as a string, but only this package knows *which*
// strings are legal — so the vocabulary is declared next to the validator that
// enforces it rather than guessed at by the reflector.
//
// This is what makes the category a typed union in TypeScript rather than a
// bare string, which in turn is what makes the UI's category → label map
// exhaustive at compile time: add a value to categories and the web build fails
// until it has a label. A hand-kept list on the TypeScript side would instead
// fail silently at runtime, on the one screen nobody tests.
//
// The value receiver is what invopop detects.
func (Category) JSONSchema() *jsonschema.Schema {
	// The empty string is a legal value meaning "unset, detection may answer"
	// (see Category.Valid), so it belongs in the enum. Omitting it would make
	// the schema reject a document the module itself accepts.
	values := []any{""}
	for _, c := range categories {
		values = append(values, string(c))
	}

	return &jsonschema.Schema{
		Type:  "string",
		Title: "Client category",
		Description: "What kind of device this is. It selects the picture " +
			"shown in the device list, and an operator-set value always beats " +
			"a detected one. Empty means nothing has been set, so detection " +
			"may answer; \"unknown\" means the device was looked at and could " +
			"not be placed.",
		Enum:    values,
		Default: "",
	}
}
