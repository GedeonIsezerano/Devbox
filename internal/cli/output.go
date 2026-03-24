package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// Printer controls how the CLI writes user-facing output.
// Info/Success go to Stderr; Data (JSON) goes to Stdout.
type Printer struct {
	Quiet   bool
	Verbose bool
	JSON    bool
	NoColor bool
	IsCI    bool
	Stderr  io.Writer // defaults to os.Stderr
	Stdout  io.Writer // defaults to os.Stdout
}

// NewPrinter creates a Printer with sensible defaults, auto-detecting
// environment settings such as NO_COLOR and CI.
func NewPrinter() *Printer {
	p := &Printer{
		Stderr: os.Stderr,
		Stdout: os.Stdout,
	}

	// Respect the NO_COLOR convention (https://no-color.org/).
	if v := os.Getenv("NO_COLOR"); v != "" {
		p.NoColor = true
	}

	// Detect CI environments.
	if os.Getenv("CI") == "true" {
		p.IsCI = true
		p.NoColor = true // CI typically has no color support
	}

	return p
}

// Info writes an informational message to stderr. It is suppressed in Quiet mode.
func (p *Printer) Info(format string, args ...any) {
	if p.Quiet {
		return
	}
	fmt.Fprintf(p.Stderr, format+"\n", args...)
}

// Error writes an error message to stderr. It is always shown, even in Quiet mode.
func (p *Printer) Error(format string, args ...any) {
	fmt.Fprintf(p.Stderr, format+"\n", args...)
}

// Success writes a success message to stderr. It is suppressed in Quiet mode.
func (p *Printer) Success(format string, args ...any) {
	if p.Quiet {
		return
	}
	fmt.Fprintf(p.Stderr, format+"\n", args...)
}

// Data writes v as JSON to stdout. This is used for structured output
// (e.g., --format json).
func (p *Printer) Data(v any) error {
	enc := json.NewEncoder(p.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
