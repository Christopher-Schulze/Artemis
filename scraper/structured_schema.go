package scraper

// StructuredSchema describes fields for structured extraction codegen.
type StructuredSchema struct {
	Fields []string
}

func (s StructuredSchema) Required() []string {
	out := make([]string, 0, len(s.Fields))
	for _, f := range s.Fields {
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}
