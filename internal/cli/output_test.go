package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestPrinterInfo(t *testing.T) {
	var stderr bytes.Buffer
	var stdout bytes.Buffer
	p := &Printer{
		Stderr: &stderr,
		Stdout: &stdout,
	}

	p.Info("hello %s", "world")

	got := stderr.String()
	if !strings.Contains(got, "hello world") {
		t.Errorf("Info() wrote %q to stderr, want it to contain %q", got, "hello world")
	}
	if stdout.Len() != 0 {
		t.Errorf("Info() should not write to stdout, got %q", stdout.String())
	}
}

func TestPrinterQuiet(t *testing.T) {
	var stderr bytes.Buffer
	var stdout bytes.Buffer
	p := &Printer{
		Quiet:  true,
		Stderr: &stderr,
		Stdout: &stdout,
	}

	p.Info("should be suppressed")

	if stderr.Len() != 0 {
		t.Errorf("Info() in quiet mode should suppress output, got %q", stderr.String())
	}
}

func TestPrinterError(t *testing.T) {
	var stderr bytes.Buffer
	p := &Printer{
		Quiet:  true,
		Stderr: &stderr,
		Stdout: &bytes.Buffer{},
	}

	p.Error("something broke: %d", 42)

	got := stderr.String()
	if !strings.Contains(got, "something broke: 42") {
		t.Errorf("Error() wrote %q, want it to contain %q", got, "something broke: 42")
	}
}

func TestPrinterSuccess(t *testing.T) {
	var stderr bytes.Buffer
	p := &Printer{
		Stderr: &stderr,
		Stdout: &bytes.Buffer{},
	}

	p.Success("done: %s", "ok")

	got := stderr.String()
	if !strings.Contains(got, "done: ok") {
		t.Errorf("Success() wrote %q, want it to contain %q", got, "done: ok")
	}
}

func TestPrinterJSON(t *testing.T) {
	var stderr bytes.Buffer
	var stdout bytes.Buffer
	p := &Printer{
		Stderr: &stderr,
		Stdout: &stdout,
	}

	data := map[string]string{"key": "value"}
	if err := p.Data(data); err != nil {
		t.Fatalf("Data() error: %v", err)
	}

	// Should write to stdout, not stderr.
	if stderr.Len() != 0 {
		t.Errorf("Data() should not write to stderr, got %q", stderr.String())
	}

	var got map[string]string
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("Data() output is not valid JSON: %v\nraw: %q", err, stdout.String())
	}

	if got["key"] != "value" {
		t.Errorf("Data() key = %q, want %q", got["key"], "value")
	}
}

func TestPrinterNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	p := NewPrinter()
	if !p.NoColor {
		t.Error("NewPrinter() should set NoColor=true when NO_COLOR env is set")
	}
}

func TestPrinterCI(t *testing.T) {
	t.Setenv("CI", "true")

	p := NewPrinter()
	if !p.IsCI {
		t.Error("NewPrinter() should set IsCI=true when CI=true")
	}
}

func TestPrinterNewPrinterDefaults(t *testing.T) {
	// Clear env vars that could affect detection.
	t.Setenv("NO_COLOR", "")
	t.Setenv("CI", "")

	p := NewPrinter()

	if p.Stderr == nil {
		t.Error("NewPrinter() Stderr should not be nil")
	}
	if p.Stdout == nil {
		t.Error("NewPrinter() Stdout should not be nil")
	}
	if p.Quiet {
		t.Error("NewPrinter() Quiet should default to false")
	}
	if p.Verbose {
		t.Error("NewPrinter() Verbose should default to false")
	}
	if p.JSON {
		t.Error("NewPrinter() JSON should default to false")
	}
}
