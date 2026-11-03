package cli

import (
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		address string
		want    string
	}{
		{
			name:    "bare host defaults to https",
			address: "headscale.example.com:50443",
			want:    "https://headscale.example.com:50443",
		},
		{
			name:    "explicit https scheme is kept",
			address: "https://headscale.example.com",
			want:    "https://headscale.example.com",
		},
		{
			name:    "explicit http scheme is kept",
			address: "http://localhost:8080",
			want:    "http://localhost:8080",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clientBaseURL(tt.address); got != tt.want {
				t.Errorf("clientBaseURL(%q) = %q, want %q", tt.address, got, tt.want)
			}
		})
	}
}

func TestIsMachineOutputFormat(t *testing.T) {
	tests := []struct {
		name         string
		outputFormat string
		want         bool
	}{
		{name: "empty is human-readable", outputFormat: "", want: false},
		{name: "json", outputFormat: outputFormatJSON, want: true},
		{name: "json-line", outputFormat: outputFormatJSONLine, want: true},
		{name: "yaml", outputFormat: outputFormatYAML, want: true},
		{name: "unknown format", outputFormat: "text", want: false},
		{name: "json with whitespace is not json", outputFormat: " json", want: false},
		{name: "uppercase JSON is not json", outputFormat: "JSON", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isMachineOutputFormat(tt.outputFormat); got != tt.want {
				t.Errorf("isMachineOutputFormat(%q) = %v, want %v", tt.outputFormat, got, tt.want)
			}
		})
	}
}

// TestHasMachineOutputFlagSpellings verifies that hasMachineOutputFlag detects
// every valid cobra/pflag spelling of the --output/-o flag, not just the
// spaced "--output json" form. The previous os.Args-based scan missed the
// single-token spellings (--output=json, -ojson), leaking logs into stdout.
func TestHasMachineOutputFlagSpellings(t *testing.T) {
	tests := []struct {
		name string
		args []string // parsed through rootCmd.PersistentFlags() like cobra does
		want bool
	}{
		{name: "no flag", args: nil, want: false},
		{name: "spaced long json", args: []string{"--output", "json"}, want: true},
		{name: "glued long json", args: []string{"--output=json"}, want: true},
		{name: "spaced short json", args: []string{"-o", "json"}, want: true},
		{name: "glued short json", args: []string{"-ojson"}, want: true},
		{name: "spaced long json-line", args: []string{"--output", "json-line"}, want: true},
		{name: "glued long json-line", args: []string{"--output=json-line"}, want: true},
		{name: "spaced short json-line", args: []string{"-o", "json-line"}, want: true},
		{name: "glued short json-line", args: []string{"-ojson-line"}, want: true},
		{name: "spaced long yaml", args: []string{"--output", "yaml"}, want: true},
		{name: "glued long yaml", args: []string{"--output=yaml"}, want: true},
		{name: "spaced short yaml", args: []string{"-o", "yaml"}, want: true},
		{name: "glued short yaml", args: []string{"-oyaml"}, want: true},
		{name: "empty value", args: []string{"--output", ""}, want: false},
		{name: "unknown value", args: []string{"--output", "text"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NoError(t, rootCmd.PersistentFlags().Set("output", ""))

			if tt.args != nil {
				require.NoError(t, rootCmd.PersistentFlags().Parse(tt.args))
			}

			if got := hasMachineOutputFlag(); got != tt.want {
				t.Errorf("hasMachineOutputFlag() for %v = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

// TestHasMachineOutputFlagIgnoresPositionalArgs verifies that a positional
// argument equal to a format name (e.g. `headscale users destroy json`) does
// not trigger machine-output mode. The previous os.Args scan matched any bare
// token, wrongly disabling logging for unrelated commands.
func TestHasMachineOutputFlagIgnoresPositionalArgs(t *testing.T) {
	oldArgs := os.Args

	t.Cleanup(func() { os.Args = oldArgs })

	// Simulate `headscale users destroy json`: the bare "json" token is a
	// positional argument, not the value of --output/-o.
	os.Args = []string{"headscale", "users", "destroy", "json"}

	require.NoError(t, rootCmd.PersistentFlags().Set("output", ""))

	assert.False(t, hasMachineOutputFlag(),
		"a positional arg equal to a format name must not enable machine-output mode")
}

// TestMachineOutputSuppressesZerolog is an end-to-end regression test: with
// log.format=json and --output=json, initConfig must detect the machine-output
// request (via the parsed flag) and disable zerolog so log lines never reach
// stdout and corrupt the command's JSON. The previous os.Args scan missed the
// glued spelling, leaving zerolog enabled and routing logs to stdout.
func TestMachineOutputSuppressesZerolog(t *testing.T) {
	// Route zerolog to stdout (as log.format=json would) and skip the update
	// check so the test never makes a network call.
	t.Setenv("HEADSCALE_LOG_FORMAT", "json")
	t.Setenv("HEADSCALE_DISABLE_CHECK_UPDATES", "true")

	// Save and restore global state mutated by initConfig / Execute.
	oldLevel := zerolog.GlobalLevel()

	t.Cleanup(func() { zerolog.SetGlobalLevel(oldLevel) })

	t.Cleanup(func() { rootCmd.SetArgs(nil) })

	require.NoError(t, rootCmd.PersistentFlags().Set("output", ""))
	t.Cleanup(func() {
		require.NoError(t, rootCmd.PersistentFlags().Set("output", ""))
	})

	// Capture stdout to assert the command's output stays valid JSON.
	stdoutR, stdoutW, err := os.Pipe()
	require.NoError(t, err)

	oldStdout := os.Stdout
	os.Stdout = stdoutW

	t.Cleanup(func() {
		os.Stdout = oldStdout

		stdoutW.Close()
		stdoutR.Close()
	})

	rootCmd.SetArgs([]string{"generate", "private-key", "--output=json"})
	require.NoError(t, rootCmd.Execute())

	// Restore stdout and close the write end so ReadAll reaches EOF.
	os.Stdout = oldStdout

	stdoutW.Close()

	out, err := io.ReadAll(stdoutR)
	require.NoError(t, err)

	// With the fix, hasMachineOutputFlag reads the parsed --output flag and
	// initConfig disables zerolog; without the fix the glued spelling was
	// missed and zerolog stayed enabled.
	assert.Equal(t, zerolog.Disabled, zerolog.GlobalLevel(),
		"machine-output spelling --output=json must disable zerolog")

	// stdout must be a single, parseable JSON document (no log lines).
	var decoded map[string]string
	require.NoError(t, json.Unmarshal(out, &decoded), "stdout must be valid JSON, got: %q", out)
	assert.NotEmpty(t, decoded["private_key"], "private_key field should be present")
}
