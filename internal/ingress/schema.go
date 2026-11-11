package ingress

import (
	"github.com/invopop/jsonschema"
)

// Schema descriptions for the module's own types.
//
// Core reflects the config struct into the schema that drives the REST body, the
// UI form and the MCP tool definition (design.md §3.2 rule 3). Core can tell
// that these marshal as strings; only this package knows *which* strings are
// legal, so the vocabulary is declared next to the parser that enforces it.
//
// Value receivers, because that is what invopop detects.

// JSONSchema publishes the upstream scheme vocabulary as an enum.
func (Scheme) JSONSchema() *jsonschema.Schema {
	// The empty string is legal and means SchemeHTTP (Scheme.Valid), so it
	// belongs in the enum. Omitting it would make the schema reject a document
	// the module itself accepts.
	values := []any{""}
	for _, s := range Schemes() {
		values = append(values, string(s))
	}

	return &jsonschema.Schema{
		Type:  "string",
		Title: "Upstream scheme",
		Description: "How olr speaks to the service being published. http is " +
			"almost always right: the connection runs over your own LAN to a " +
			"client you named, and the HTTPS a browser sees is terminated here. " +
			"Use https only when the service refuses plain HTTP — many NAS and " +
			"hypervisor UIs do — in which case its own certificate is not " +
			"checked, because those are self-signed and demanding a valid one " +
			"would make the case this option exists for impossible. " +
			"Empty means http.",
		Enum:    values,
		Default: string(SchemeHTTP),
	}
}

// providerSchema describes the DNS provider field, and publishes **no enum**.
//
// The legal set is whatever the operator's proxy binary was built with
// (providers.go), which is not knowable at reflection time. An enum here could
// therefore only ever be a guess, and a guess that a form renders as a closed
// dropdown is worse than no list at all: it would offer names the binary does
// not have and hide the one it does.
//
// The live list is a request away — GET /api/ingress/providers, or `olr ingress
// show providers` — and a form that wants a dropdown should fetch it.
func providerSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Type:  "string",
		Title: "DNS provider",
		Description: "Who hosts the DNS for your domain. olr writes a temporary " +
			"record through their API to prove the domain is yours, which is the " +
			"only way to get a certificate for a name that does not resolve from " +
			"the internet. The names your proxy supports are listed by " +
			"`olr ingress show providers`.",
	}
}

// JSONSchema describes the certificate section, and exists mainly to keep the
// credential from being presented as an ordinary field.
func (Certificate) JSONSchema() *jsonschema.Schema {
	props := jsonschema.NewProperties()
	props.Set("provider", providerSchema())
	props.Set("provider_token", &jsonschema.Schema{
		Type:  "string",
		Title: "Provider API token",
		Description: "An API credential for the DNS provider. Scope it to this " +
			"one zone if the provider allows it: it is stored on the router and " +
			"can change your DNS. It is never shown again after it is set.",
		// Carried for any surface that renders a form. A credential typed into
		// a visible field is a credential on somebody's screen share.
		Extras: map[string]any{"writeOnly": true, "format": "password"},
	})
	props.Set("acme_email", &jsonschema.Schema{
		Type:        "string",
		Title:       "Contact address",
		Description: "Optional. Used by the certificate authority to warn you before a certificate expires.",
	})
	props.Set("resolvers", &jsonschema.Schema{
		Type:  "array",
		Items: &jsonschema.Schema{Type: "string"},
		Title: "Propagation-check resolvers",
		Description: "Public DNS servers used only to confirm the challenge " +
			"record has published. Kept away from this router on purpose: a " +
			"blocking rule or a forwarding quirk that hid the record would look " +
			"exactly like a certificate that never arrives. Leave empty for " +
			"sensible defaults.",
	})

	return &jsonschema.Schema{
		Type:       "object",
		Title:      "Certificate",
		Properties: props,
		Description: "How the one wildcard certificate that serves every " +
			"published name is obtained.",
	}
}
