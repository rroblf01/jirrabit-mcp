package schema

// GetTarget satisfies the interface the tool layer uses to resolve which
// jirrabit instance a call is about. It is defined once on Target and promoted
// to every argument struct through embedding, so adding a new tool automatically
// supports multi-instance operation.
func (t Target) GetTarget() (instanceURL, apiKey string) {
	return t.InstanceURL, t.APIKey
}
