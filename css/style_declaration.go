package css

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// StyleDeclaration owns unique, ordered inline declarations. It shares the
// stylesheet parser, shorthand expansion and specified-value grammar.
type StyleDeclaration struct {
	declarations []Declaration
	index        map[string]int
}

// ParseStyleDeclaration rejects partial input rather than fabricating CSSOM.
func ParseStyleDeclaration(source string) (*StyleDeclaration, error) {
	return parseStyleDeclaration(source, false)
}

// ParseStyleAttribute preserves the native immutable attribute fast path.
// Mutable cssText parsing uses ParseStyleDeclaration instead.
func ParseStyleAttribute(source string) (*StyleDeclaration, error) {
	return parseStyleDeclaration(source, true)
}

func parseStyleDeclaration(source string, attribute bool) (*StyleDeclaration, error) {
	block := ParseDeclarations(source)
	if err := block.Err(); err != nil {
		return nil, err
	}
	result := &StyleDeclaration{index: make(map[string]int)}
	expanded, err := cssExpandedInlineDeclarations(block.Declarations)
	if err != nil {
		return nil, err
	}
	if attribute && (len(expanded) < 2 || len(expanded) == 2 && expanded[0].Property != expanded[1].Property) {
		for _, declaration := range expanded {
			result.put(declaration)
		}
		return result, result.checkBudget()
	}
	winners := make(map[string]int)
	for index, declaration := range expanded {
		previous, exists := winners[declaration.Property]
		if !exists || !expanded[previous].Important || declaration.Important {
			winners[declaration.Property] = index
		}
	}
	// Blink preserves the last winning source position, with important entries
	// after normal entries. Explicit later CSSOM writes keep existing positions.
	for _, important := range []bool{false, true} {
		for index, declaration := range expanded {
			if winners[declaration.Property] == index && declaration.Important == important {
				result.put(declaration)
			}
		}
	}
	if err := result.checkBudget(); err != nil {
		return nil, err
	}
	return result, nil
}

func cssExpandedInlineDeclarations(declarations []Declaration) ([]Declaration, error) {
	var expanded []Declaration
	for _, declaration := range declarations {
		values, err := cssInlineDeclarations(declaration)
		if err != nil {
			return nil, err
		}
		if len(expanded)+len(values) > maxCSSCascadeDeclarations {
			return nil, cssDiagnostic(DiagnosticBudget, declaration.Offset, "inline declaration expansion limit exceeded")
		}
		expanded = append(expanded, values...)
	}
	return expanded, nil
}

func (s *StyleDeclaration) checkBudget() error {
	custom := 0
	for _, declaration := range s.declarations {
		if strings.HasPrefix(declaration.Property, "--") {
			custom++
		}
	}
	if custom > maxCSSVariables {
		return cssDiagnostic(DiagnosticBudget, 0, "inline custom property count limit exceeded")
	}
	return nil
}

func cssInlineDeclarations(declaration Declaration) ([]Declaration, error) {
	var result []Declaration
	for _, current := range expandCSSCascadeDeclaration(declaration) {
		if current.Property == declaration.Property && (current.Property == "padding" || current.Property == "margin" || current.Property == "background") {
			continue
		}
		if len(cssVariableReferences(current.tokens)) == 0 {
			tokens, err := cssShorthandTokens(current, current.Property, current.tokens)
			if err != nil {
				return nil, err
			}
			current.tokens = tokens
			if current.Shorthand == "background" && current.Property != "background-color" && cssWideKeyword(current.tokens) == "" {
				initial, diagnostic := tokenizeCSS("initial")
				if diagnostic != nil {
					return nil, diagnostic
				}
				current.tokens = initial
			}
		}
		value, err := cssSpecifiedValue(current.Property, current.tokens)
		if err != nil {
			return nil, err
		}
		current.Value = value
		result = append(result, current)
	}
	return result, nil
}

func cssPropertyName(property string) string {
	if strings.HasPrefix(property, "--") {
		return property
	}
	return cssASCIILower(property)
}

func (s *StyleDeclaration) put(declaration Declaration) {
	if index, exists := s.index[declaration.Property]; exists {
		s.declarations[index] = declaration
		return
	}
	s.index[declaration.Property] = len(s.declarations)
	s.declarations = append(s.declarations, declaration)
}

// Length is the number of admitted longhand/custom declarations.
func (s *StyleDeclaration) Length() int { return len(s.declarations) }

// Item returns the declaration name at its stable CSSOM index.
func (s *StyleDeclaration) Item(index int) string {
	if index < 0 || index >= len(s.declarations) {
		return ""
	}
	return s.declarations[index].Property
}

// GetPropertyValue excludes priority and preserves pending shorthand behavior.
func (s *StyleDeclaration) GetPropertyValue(property string) (string, error) {
	property = cssPropertyName(property)
	if longhands := cssInlineLonghands(property); len(longhands) > 0 {
		return s.shorthand(property, longhands), nil
	}
	if !strings.HasPrefix(property, "--") && !cssSpecifiedProperty(property) {
		return "", cssDiagnostic(DiagnosticUnsupported, 0, "CSSOM property read lacks admitted grammar semantics")
	}
	index, exists := s.index[property]
	if !exists {
		return "", nil
	}
	declaration := s.declarations[index]
	if declaration.Shorthand != "" && len(cssVariableReferences(declaration.tokens)) > 0 {
		return "", nil
	}
	return declaration.Value, nil
}

// GetPropertyPriority returns only the canonical important marker or empty.
func (s *StyleDeclaration) GetPropertyPriority(property string) (string, error) {
	property = cssPropertyName(property)
	longhands := cssInlineLonghands(property)
	if len(longhands) == 0 && !strings.HasPrefix(property, "--") && !cssSpecifiedProperty(property) {
		return "", cssDiagnostic(DiagnosticUnsupported, 0, "CSSOM priority read lacks admitted grammar semantics")
	}
	if len(longhands) == 0 {
		longhands = []string{property}
	}
	for _, name := range longhands {
		index, exists := s.index[name]
		if !exists || !s.declarations[index].Important {
			return "", nil
		}
	}
	return "important", nil
}

func cssInlineLonghands(property string) []string {
	if property == "background" {
		return cssBackgroundLonghands[:]
	}
	if property != "padding" && property != "margin" {
		return nil
	}
	result := make([]string, 0, 4)
	for _, side := range cssBoxSides {
		result = append(result, property+"-"+side)
	}
	return result
}

func (s *StyleDeclaration) shorthand(property string, names []string) string {
	values := s.shorthandDeclarations(names)
	if len(values) == 0 {
		return ""
	}
	return cssInlineShorthandValue(property, names, values)
}

func (s *StyleDeclaration) shorthandDeclarations(names []string) []Declaration {
	values := make([]Declaration, 0, len(names))
	for _, name := range names {
		index, exists := s.index[name]
		if !exists {
			return nil
		}
		declaration := s.declarations[index]
		if len(values) > 0 && declaration.Important != values[0].Important {
			return nil
		}
		values = append(values, declaration)
	}
	return values
}

func cssInlineShorthandValue(property string, names []string, values []Declaration) string {
	for _, declaration := range values {
		pending := len(cssVariableReferences(declaration.tokens)) > 0
		if property == "background" && declaration.Property != "background-color" && declaration.Shorthand == "background" && cssWideKeyword(declaration.tokens) == "initial" {
			continue
		}
		if pending && declaration.Shorthand != property {
			return ""
		}
		if pending || cssWideKeyword(declaration.tokens) != "" {
			for _, other := range values {
				if other.Value != declaration.Value || pending && other.Shorthand != declaration.Shorthand {
					return ""
				}
			}
			return declaration.Value
		}
	}
	if property == "background" {
		color := ""
		for index, declaration := range values {
			if names[index] == "background-color" {
				color = declaration.Value
				continue
			}
			if declaration.Value != "initial" && declaration.Value != cssBackgroundDefault(names[index]) {
				return ""
			}
		}
		return color
	}
	return cssBoxShorthandValue([4]string{values[0].Value, values[1].Value, values[2].Value, values[3].Value})
}

// CSSString serializes admitted declarations without alphabetic reordering.
// Incomplete pending shorthands reject instead of losing retained values.
func (s *StyleDeclaration) CSSString() (string, error) {
	if err := s.checkBudget(); err != nil {
		return "", err
	}
	var result strings.Builder
	emitted := make(map[string]bool)
	for _, declaration := range s.declarations {
		if emitted[declaration.Property] {
			continue
		}
		property, value := s.serializationValue(declaration, emitted)
		if property == declaration.Property && declaration.Shorthand != "" && len(cssVariableReferences(declaration.tokens)) > 0 {
			return "", cssDiagnostic(DiagnosticUnsupported, declaration.Offset, "partial pending shorthand cannot be serialized without loss")
		}
		if result.Len() > 0 {
			result.WriteByte(' ')
		}
		name, err := cssSerializePropertyName(property)
		if err != nil {
			return "", err
		}
		result.WriteString(name + ": " + value)
		if declaration.Important {
			result.WriteString(" !important")
		}
		result.WriteByte(';')
		if result.Len() > MaxStylesheetBytes {
			return "", cssDiagnostic(DiagnosticBudget, 0, "serialized inline CSS byte limit exceeded")
		}
	}
	return result.String(), nil
}

func (s *StyleDeclaration) serializationValue(declaration Declaration, emitted map[string]bool) (string, string) {
	property, value := declaration.Property, declaration.Value
	for _, shorthand := range []string{"margin", "padding", "background"} {
		for _, longhand := range cssInlineLonghands(shorthand) {
			if property != longhand {
				continue
			}
			if combined := s.shorthand(shorthand, cssInlineLonghands(shorthand)); combined != "" {
				property, value = shorthand, combined
				for _, name := range cssInlineLonghands(shorthand) {
					emitted[name] = true
				}
			}
		}
	}
	return property, value
}

func cssSerializePropertyName(property string) (string, error) {
	if len(property) > maxCSSValueBytes || !utf8.ValidString(property) || strings.ContainsRune(property, 0) {
		return "", cssDiagnostic(DiagnosticSyntax, 0, "bounded NUL-free UTF-8 property name required")
	}
	var result strings.Builder
	for _, character := range property {
		switch {
		case character < 0x20 || character == 0x7f:
			result.WriteString("\\" + strconv.FormatInt(int64(character), 16) + " ")
		case character >= 0x80 || character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_':
			result.WriteRune(character)
		default:
			result.WriteByte('\\')
			result.WriteRune(character)
		}
	}
	if result.Len() > maxCSSValueBytes {
		return "", cssDiagnostic(DiagnosticBudget, 0, "serialized CSS property name byte limit exceeded")
	}
	return result.String(), nil
}

// Clone copies declaration ownership while sharing immutable parsed tokens.
func (s *StyleDeclaration) Clone() *StyleDeclaration {
	result := &StyleDeclaration{declarations: append([]Declaration(nil), s.declarations...), index: make(map[string]int, len(s.index))}
	for property, index := range s.index {
		result.index[property] = index
	}
	return result
}

// SetProperty validates and serializes a complete candidate before mutation.
// Invalid priority is a native CSSOM no-op, including writes with empty values.
func (s *StyleDeclaration) SetProperty(property, value, priority string) (bool, error) {
	property = cssPropertyName(property)
	if priority != "" && cssASCIILower(priority) != "important" {
		return false, nil
	}
	if value == "" {
		_, changed, err := s.RemoveProperty(property)
		return changed, err
	}
	if len(value) > maxCSSValueBytes {
		return false, cssDiagnostic(DiagnosticBudget, 0, "inline value byte limit exceeded")
	}
	values, err := cssInlineSetDeclarations(property, value, priority)
	if err != nil {
		return false, err
	}
	candidate := s.Clone()
	for _, declaration := range values {
		candidate.put(declaration)
	}
	serialized, err := candidate.CSSString()
	if err != nil {
		return false, err
	}
	if previous, err := s.CSSString(); err == nil && previous == serialized {
		return false, nil
	}
	*s = *candidate
	return true, nil
}

func cssInlineSetDeclarations(property, value, priority string) ([]Declaration, error) {
	name, err := cssSerializePropertyName(property)
	if err != nil {
		return nil, err
	}
	tokens, diagnostic := tokenizeCSS(value)
	if diagnostic != nil {
		return nil, diagnostic
	}
	if diagnostic := validateCSSDeclarationValue(tokens); diagnostic != nil {
		return nil, diagnostic
	}
	declarationTokens, diagnostic := tokenizeCSS(name + ":" + value)
	if diagnostic != nil {
		return nil, diagnostic
	}
	declaration, diagnostic := parseCSSDeclaration(declarationTokens)
	if diagnostic != nil {
		return nil, diagnostic
	}
	declaration.Important = priority != ""
	return cssInlineDeclarations(declaration)
}

// RemoveProperty commits only lossless removal. Native shorthand removal
// returns empty text even when the shorthand getter has a serialized value.
func (s *StyleDeclaration) RemoveProperty(property string) (string, bool, error) {
	property = cssPropertyName(property)
	previous, err := s.GetPropertyValue(property)
	if err != nil {
		return "", false, err
	}
	if len(cssInlineLonghands(property)) > 0 {
		previous = ""
	}
	names := cssInlineLonghands(property)
	if len(names) == 0 {
		names = []string{property}
	}
	removed := make(map[string]bool, len(names))
	for _, name := range names {
		removed[name] = true
	}
	candidate := &StyleDeclaration{index: make(map[string]int)}
	changed := false
	for _, declaration := range s.declarations {
		if removed[declaration.Property] {
			changed = true
			continue
		}
		candidate.put(declaration)
	}
	if !changed {
		return previous, false, nil
	}
	if _, err := candidate.CSSString(); err != nil {
		return "", false, err
	}
	*s = *candidate
	return previous, true, nil
}
