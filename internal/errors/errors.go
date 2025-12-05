package errors

import (
	"fmt"
	"strings"
)

// Severity indicates the seriousness of the diagnostic.
type Severity int

const (
	SeverityInfo Severity = iota
	SeverityWarning
	SeverityError
)

func (s Severity) String() string {
	switch s {
	case SeverityInfo:
		return "INFO"
	case SeverityWarning:
		return "WARNING"
	case SeverityError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// Position represents a location in a source file.
type Position struct {
	File string
	Line int
	Col  int
}

func (p Position) String() string {
	return fmt.Sprintf("%s:%d:%d", p.File, p.Line, p.Col)
}

// Highlight represents a range of text to underline in the context.
type Highlight struct {
	StartCol int
	EndCol   int // Exclusive
}

// Diagnostic represents a single compiler message.
type Diagnostic struct {
	Severity   Severity
	Code       string
	Message    string
	Pos        Position
	Context    string      // The source line content
	Highlights []Highlight // Specific ranges to underline
	Suggestion string      // Optional fix suggestion
}

func (d Diagnostic) String() string {
	var sb strings.Builder

	// Header: [ERROR] E1001: undefined variable 'count'
	codeStr := ""
	if d.Code != "" {
		codeStr = " " + d.Code
	}
	sb.WriteString(fmt.Sprintf("[%s]%s: %s\n", d.Severity, codeStr, d.Message))

	// Location: --> src/main.lum:10:5
	sb.WriteString(fmt.Sprintf("  --> %s\n", d.Pos))

	if d.Context != "" {
		lineNumStr := fmt.Sprintf("%d", d.Pos.Line)
		padding := strings.Repeat(" ", len(lineNumStr))

		sb.WriteString(fmt.Sprintf("   %s |\n", padding))
		sb.WriteString(fmt.Sprintf(" %s | %s\n", lineNumStr, d.Context))

		// Highlights
		sb.WriteString(fmt.Sprintf("   %s | ", padding))
		if len(d.Highlights) > 0 {
			// Find max end col to size the buffer
			maxCol := 0
			for _, h := range d.Highlights {
				if h.EndCol > maxCol {
					maxCol = h.EndCol
				}
			}

			// Create underline buffer
			underline := make([]byte, maxCol)
			for i := range underline {
				underline[i] = ' '
			}

			for _, h := range d.Highlights {
				start := h.StartCol
				end := h.EndCol
				// Assume 1-based columns
				if start < 1 {
					start = 1
				}

				for i := start; i < end; i++ {
					if i-1 < len(underline) {
						underline[i-1] = '^'
					}
				}
			}
			sb.Write(underline)
		} else if d.Pos.Col > 0 {
			// Fallback: point to the start column
			if d.Pos.Col > 1 {
				sb.WriteString(strings.Repeat(" ", d.Pos.Col-1))
			}
			sb.WriteString("^")
		}
		sb.WriteString("\n")
	}

	if d.Suggestion != "" {
		lineNumStr := fmt.Sprintf("%d", d.Pos.Line)
		padding := strings.Repeat(" ", len(lineNumStr))
		sb.WriteString(fmt.Sprintf("   %s |\n", padding))
		sb.WriteString(fmt.Sprintf("   %s = Suggestion: %s\n", padding, d.Suggestion))
	}
	return sb.String()
}

// Handler collects diagnostics during compilation.
type Handler struct {
	Diagnostics []Diagnostic
}

func NewHandler() *Handler {
	return &Handler{
		Diagnostics: make([]Diagnostic, 0),
	}
}

func (h *Handler) Add(d Diagnostic) {
	h.Diagnostics = append(h.Diagnostics, d)
}

// ReportError adds an error diagnostic with a code.
func (h *Handler) ReportError(pos Position, code, msg string) {
	h.Add(Diagnostic{
		Severity: SeverityError,
		Code:     code,
		Message:  msg,
		Pos:      pos,
	})
}

func (h *Handler) Error(pos Position, msg string) {
	h.Add(Diagnostic{
		Severity: SeverityError,
		Message:  msg,
		Pos:      pos,
	})
}

func (h *Handler) HasErrors() bool {
	for _, d := range h.Diagnostics {
		if d.Severity == SeverityError {
			return true
		}
	}
	return false
}
