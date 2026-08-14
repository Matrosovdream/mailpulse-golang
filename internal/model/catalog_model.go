package model

// Schema describes a plug-in's configuration to the SPA so the form that
// configures an event or a notifier is rendered from the registry rather than
// hard-coded in the front end. Adding a handler needs no SPA release.
type Schema struct {
	Fields []SchemaField `json:"fields"`
}

type SchemaField struct {
	Name        string   `json:"name"`
	Label       string   `json:"label"`
	Type        string   `json:"type"` // string, text, int, bool, enum, secret
	Required    bool     `json:"required"`
	Options     []string `json:"options,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
	Help        string   `json:"help,omitempty"`
	Secret      bool     `json:"secret,omitempty"`
}

type EventTypeResponse struct {
	Type          string `json:"type"`
	Label         string `json:"label"`
	Description   string `json:"description"`
	UsesNotifiers bool   `json:"uses_notifiers"`
	ConfigSchema  Schema `json:"config_schema"`
}

type NotifierTypeResponse struct {
	Type                 string `json:"type"`
	Label                string `json:"label"`
	Description          string `json:"description"`
	RequiresVerification bool   `json:"requires_verification"`
	ConfigSchema         Schema `json:"config_schema"`
}

type FilterFieldResponse struct {
	Field     string   `json:"field"`
	Label     string   `json:"label"`
	ValueType string   `json:"value_type"`
	Operators []string `json:"operators"`
	Help      string   `json:"help,omitempty"`
}
