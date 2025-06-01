package app_test

import (
	"bytes"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/desertbit/grumble"
	"github.com/jm96441n/ligolo-ng/cmd/proxy/app"
	"github.com/jm96441n/ligolo-ng/cmd/proxy/config"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/AlecAivazis/survey/v2/terminal"
	"github.com/AlecAivazis/survey/v2/core"
)

// testStdio is a helper to temporarily replace stdin/stdout/stderr
// and restore them.
type testStdio struct {
	origStdin  *os.File
	origStdout *os.File
	origStderr *os.File
	rStdin     *os.File
	wStdin     *os.File
	rStdout    *os.File
	wStdout    *os.File
	rStderr    *os.File
	wStderr    *os.File
}

func (s *testStdio) Capture() {
	s.origStdin = os.Stdin
	s.origStdout = os.Stdout
	s.origStderr = os.Stderr

	s.rStdin, s.wStdin, _ = os.Pipe()
	s.rStdout, s.wStdout, _ = os.Pipe()
	s.rStderr, s.wStderr, _ = os.Pipe()

	os.Stdin = s.rStdin
	os.Stdout = s.wStdout
	os.Stderr = s.wStderr

	// For survey
	terminal.DefaultStdio = terminal.Stdio{In: s.rStdin, Out: s.wStdout, Err: s.wStderr}

}

func (s *testStdio) Restore() {
	s.wStdin.Close()
	s.wStdout.Close()
	s.wStderr.Close()

	os.Stdin = s.origStdin
	os.Stdout = s.origStdout
	os.Stderr = s.origStderr

	// Restore survey's default stdio
	terminal.DefaultStdio = terminal.Stdio{In: os.Stdin, Out: os.Stdout, Err: os.Stderr}

}

func (s *testStdio) ReadStdout() string {
	s.wStdout.Close() // Close writer to signal EOF for reader
	out, _ := io.ReadAll(s.rStdout)
	s.rStdout.Close()
	return string(out)
}

func (s *testStdio) ReadStderr() string {
	s.wStderr.Close()
	out, _ := io.ReadAll(s.rStderr)
	s.rStderr.Close()
	return string(out)
}

func (s *testStdio)WriteStringToStdin(input string) {
	s.wStdin.WriteString(input)
	// s.wStdin.Close() // Close stdin to signal EOF if the command expects it
}


// setupTestConfig initializes a new Viper instance for testing
// and points config.Config to it.
func setupTestConfig(t *testing.T) *viper.Viper {
	testViper := viper.New()
	testViper.Set("ssh.port", 0) // Ensure defaults are not from some other state
	testViper.Set("ssh.publickeys", []string{})

	originalConfig := config.Config
	config.Config = testViper
	t.Cleanup(func() {
		config.Config = originalConfig // Restore original config
	})
	return testViper
}

// executeGrumbleCommand is a helper to run Grumble commands for testing.
// It's a simplified version and might need adjustments based on how commands are structured.
func executeGrumbleCommand(t *testing.T, cmd *grumble.Command, args map[string]interface{}, flags map[string]interface{}) error {
	testApp := grumble.New(&grumble.Config{Name: "testapp"})
	testApp.AddCommand(cmd) // Add the command to a temporary app instance

	context := &grumble.Context{
		App:  testApp, // Use the temporary app
		Command: cmd,
		Args: grumble.Args{Data: args},
		Flags: grumble.Flags{Data: flags},
	}
	if cmd.Run == nil {
		t.Fatal("Command has no Run function")
	}
	return cmd.Run(context)
}


func TestSSHPortCommand(t *testing.T) {
	// Find the sshport command. Grumble app initialization is in app.init()
	// We need to ensure app.App is initialized.
	// The app.App variable is global, so we can access its commands.
	// This assumes `app.App` has been initialized by the time tests run (e.g. via package import)
	var sshPortCmd *grumble.Command
	for _, cmd := range app.App.Commands() {
		if cmd.Name == "sshport" {
			sshPortCmd = cmd
			break
		}
	}
	if sshPortCmd == nil {
		t.Fatal("sshport command not found")
	}

	t.Run("SetValidPort", func(t *testing.T) {
		testViper := setupTestConfig(t)
		stdio := &testStdio{}
		stdio.Capture()
		defer stdio.Restore()


		err := executeGrumbleCommand(t, sshPortCmd, map[string]interface{}{"port": 2223}, nil)
		assert.NoError(t, err)
		assert.Equal(t, 2223, testViper.GetInt("ssh.port"))
		output := stdio.ReadStdout()
		assert.Contains(t, output, "SSH port set to 2223 successfully.")
	})

	t.Run("SetInvalidPort_TooLow", func(t *testing.T) {
		testViper := setupTestConfig(t)
		stdio := &testStdio{}
		stdio.Capture()
		defer stdio.Restore()

		originalPort := 1234 // Set an initial port
		testViper.Set("ssh.port", originalPort)

		err := executeGrumbleCommand(t, sshPortCmd, map[string]interface{}{"port": 0}, nil)
		assert.NoError(t, err) // Command itself doesn't error, it prints to App.PrintError
		assert.Equal(t, originalPort, testViper.GetInt("ssh.port")) // Port should not change

		// Grumble's PrintError goes to its own error writer, which is os.Stderr by default
		// Need to capture app's error stream if possible, or check stdout if it prints there.
		// For now, we assume it prints to stderr, which our stdio captures.
		errOutput := stdio.ReadStderr()
		assert.Contains(t, errOutput, "invalid port number")
	})

	t.Run("SetInvalidPort_TooHigh", func(t *testing.T) {
		testViper := setupTestConfig(t)
		stdio := &testStdio{}
		stdio.Capture()
		defer stdio.Restore()
		originalPort := 1234
		testViper.Set("ssh.port", originalPort)

		err := executeGrumbleCommand(t, sshPortCmd, map[string]interface{}{"port": 70000}, nil)
		assert.NoError(t, err)
		assert.Equal(t, originalPort, testViper.GetInt("ssh.port"))
		errOutput := stdio.ReadStderr()
		assert.Contains(t, errOutput, "invalid port number")

	})

	// Note: Testing non-integer input ("abc") for an Int arg in Grumble
	// is usually handled by Grumble's argument parsing before Run is called.
	// Such a test would be more of a Grumble framework test unless custom validation is added.
}

func findSubCommand(parent *grumble.Command, name string) *grumble.Command {
	for _, cmd := range parent.Commands() {
		if cmd.Name == name {
			return cmd
		}
	}
	return nil
}


func TestSSHKeyCommands(t *testing.T) {
	var sshKeyCmd, sshKeyAddCmd, sshKeyListCmd, sshKeyDelCmd *grumble.Command
	for _, cmd := range app.App.Commands() {
		if cmd.Name == "sshkey" {
			sshKeyCmd = cmd
			sshKeyAddCmd = findSubCommand(sshKeyCmd, "add")
			sshKeyListCmd = findSubCommand(sshKeyCmd, "list")
			sshKeyDelCmd = findSubCommand(sshKeyCmd, "del")
			break
		}
	}
	if sshKeyCmd == nil || sshKeyAddCmd == nil || sshKeyListCmd == nil || sshKeyDelCmd == nil {
		t.Fatalf("sshkey subcommands not found (sshKeyCmd: %v, sshKeyAddCmd: %v, sshKeyListCmd: %v, sshKeyDelCmd: %v)", sshKeyCmd, sshKeyAddCmd, sshKeyListCmd, sshKeyDelCmd)
	}

	// For survey, ensure that core.DisableColor is true for consistent output
	core.DisableColor = true


	t.Run("Add_NewKey", func(t *testing.T) {
		testViper := setupTestConfig(t)
		stdio := &testStdio{}
		stdio.Capture()
		defer stdio.Restore()

		testKey := "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQD..."
		err := executeGrumbleCommand(t, sshKeyAddCmd, map[string]interface{}{"publickey": testKey}, nil)
		assert.NoError(t, err)
		keys := testViper.GetStringSlice("ssh.publickeys")
		assert.Contains(t, keys, testKey)
		output := stdio.ReadStdout()
		assert.Contains(t, output, "SSH public key added successfully.")
	})

	t.Run("Add_DuplicateKey", func(t *testing.T) {
		testViper := setupTestConfig(t)
		stdio := &testStdio{}
		stdio.Capture()
		defer stdio.Restore()

		testKey := "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQD..."
		testViper.Set("ssh.publickeys", []string{testKey}) // Pre-add the key

		err := executeGrumbleCommand(t, sshKeyAddCmd, map[string]interface{}{"publickey": testKey}, nil)
		assert.NoError(t, err) // Command prints error, doesn't return it
		keys := testViper.GetStringSlice("ssh.publickeys")
		assert.Equal(t, 1, len(keys)) // Should not add duplicate
		errOutput := stdio.ReadStderr()
		assert.Contains(t, errOutput, "public key already exists")
	})

	t.Run("Add_EmptyKey", func(t *testing.T) {
		testViper := setupTestConfig(t)
		stdio := &testStdio{}
		stdio.Capture()
		defer stdio.Restore()

		err := executeGrumbleCommand(t, sshKeyAddCmd, map[string]interface{}{"publickey": ""}, nil)
		assert.NoError(t, err) // Command prints error
		keys := testViper.GetStringSlice("ssh.publickeys")
		assert.Empty(t, keys) // No key should be added
		errOutput := stdio.ReadStderr()
		assert.Contains(t, errOutput, "public key cannot be empty")
	})

	t.Run("List_NoKeys", func(t *testing.T) {
		setupTestConfig(t) // Ensures keys are empty
		stdio := &testStdio{}
		stdio.Capture()
		defer stdio.Restore()

		err := executeGrumbleCommand(t, sshKeyListCmd, nil, nil)
		assert.NoError(t, err)
		output := stdio.ReadStdout()
		assert.Contains(t, output, "No SSH keys configured.")
	})

	t.Run("List_WithKeys", func(t *testing.T) {
		testViper := setupTestConfig(t)
		stdio := &testStdio{}
		stdio.Capture()
		defer stdio.Restore()

		key1 := "ssh-rsa KEY1"
		key2 := "ssh-ed25519 KEY2"
		testViper.Set("ssh.publickeys", []string{key1, key2})

		err := executeGrumbleCommand(t, sshKeyListCmd, nil, nil)
		assert.NoError(t, err)
		output := stdio.ReadStdout()
		assert.Contains(t, output, key1)
		assert.Contains(t, output, key2)
	})

	t.Run("Del_NoKeys", func(t *testing.T) {
		setupTestConfig(t)
		stdio := &testStdio{}
		stdio.Capture()
		defer stdio.Restore()

		err := executeGrumbleCommand(t, sshKeyDelCmd, nil, nil)
		assert.NoError(t, err)
		output := stdio.ReadStdout()
		assert.Contains(t, output, "No SSH keys to delete.")
	})

	t.Run("Del_ExistingKey", func(t *testing.T) {
		testViper := setupTestConfig(t)
		stdio := &testStdio{}
		stdio.Capture() // Capture before any goroutines that might write to stdout

		key1 := "ssh-rsa KEY1_TO_DELETE"
		key2 := "ssh-ed25519 KEY2_TO_KEEP"
		testViper.Set("ssh.publickeys", []string{key1, key2})

		// Simulate user input "1" for the first key
		// The input needs to be written to the pipe before the command reads it.
		go func() {
			// Wait briefly for the prompt to be ready (not ideal, but survey is tricky)
			// time.Sleep(50 * time.Millisecond)
			stdio.WriteStringToStdin("1\n")
		}()

		err := executeGrumbleCommand(t, sshKeyDelCmd, nil, nil)
		assert.NoError(t, err)

		stdio.Restore() // Restore stdio to read the output
		output := stdio.ReadStdout() // This now includes prompt and confirmation

		assert.Contains(t, output, "Current SSH public keys:")
		assert.Contains(t, output, "1: "+key1)
		assert.Contains(t, output, "2: "+key2)
		assert.Contains(t, output, "Enter the number of the key to delete:")
		assert.Contains(t, output, "SSH public key deleted successfully.")

		keys := testViper.GetStringSlice("ssh.publickeys")
		assert.Equal(t, 1, len(keys))
		assert.Equal(t, key2, keys[0])
	})

	t.Run("Del_InvalidSelection_NonNumeric", func(t *testing.T) {
		testViper := setupTestConfig(t)
		stdio := &testStdio{}
		stdio.Capture()

		key1 := "ssh-rsa KEY1"
		testViper.Set("ssh.publickeys", []string{key1})

		go func() {
			stdio.WriteStringToStdin("abc\n")
		}()

		err := executeGrumbleCommand(t, sshKeyDelCmd, nil, nil)
		assert.NoError(t, err) // Command itself doesn't error

		stdio.Restore()
		errOutput := stdio.ReadStderr() // Survey prints validation errors to Stderr

		// Depending on survey's exact behavior, the error might be in stdout from the app.PrintError
		// or directly in stderr from survey. Let's check both if one fails.
		// The current app.PrintError in sshKeyDelCmd writes to c.App.PrintError which goes to Grumble's error writer (default Stderr)
		assert.Contains(t, errOutput, "invalid selection")

		keys := testViper.GetStringSlice("ssh.publickeys")
		assert.Equal(t, 1, len(keys)) // Key should not be deleted
	})

	t.Run("Del_InvalidSelection_OutOfRange", func(t *testing.T) {
		testViper := setupTestConfig(t)
		stdio := &testStdio{}
		stdio.Capture()

		key1 := "ssh-rsa KEY1"
		testViper.Set("ssh.publickeys", []string{key1})

		go func() {
			stdio.WriteStringToStdin("3\n") // Input 3 when only 1 key exists
		}()

		err := executeGrumbleCommand(t, sshKeyDelCmd, nil, nil)
		assert.NoError(t, err)

		stdio.Restore()
		errOutput := stdio.ReadStderr()
		assert.Contains(t, errOutput, "invalid selection")

		keys := testViper.GetStringSlice("ssh.publickeys")
		assert.Equal(t, 1, len(keys)) // Key should not be deleted
	})
}

func init() {
	// This is a bit of a hack to ensure the app.App is initialized
	// and its commands are registered before tests try to access them.
	// In a real scenario, you might have an explicit Init() for the app
	// or rely on package import order if app.init() is in app/app.go.
	// For this test setup, directly calling a function that uses app.App
	// or just importing the app package should trigger its init().
	// If app.App.Commands() is empty, it means init() in cli.go didn't run.
	// We can manually add them if needed for a more hermetic test environment.
	 if len(app.App.Commands()) == 0 {
		// This indicates that the app's init() didn't run.
		// This can happen depending on test execution context.
		// For robust testing, one might need to explicitly call an init function
		// or structure command registration differently.
		// However, `import _ "github.com/jm96441n/ligolo-ng/cmd/proxy/app"` in the test file
		// should typically trigger the init() function of the app package.
		// The current structure of tests assumes that app.App is populated.
	 }

	 // Disable color for survey to make output matching easier
	 core.DisableColor = true

}
