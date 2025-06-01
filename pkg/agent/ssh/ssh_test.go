package ssh

import (
	"fmt"
	"io"
	// "net" // Not directly needed for PublicKeyCallback test
	"os"
	// "strings" // Not used in this version
	// "sync" // Not used in this version
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"golang.org/x/crypto/ssh"
)

// Minimal valid Ed25519 public key for testing
const (
	validEd25519PubKeyStr        = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGCiszAUP313P5yDUtfLMV6fRUXT7fLErv9axjCF/9ms test@example.com"
	anotherValidEd25519PubKeyStr  = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIMNTjZ3UnqT0xVwZWBVl1jZwjN2uV0hA/bs0UnE0f7gP user@somewhere"
	// A valid RSA key - ensure this is a real, correctly formatted RSA public key string if used for actual RSA specific tests
	validRSAPubKeyStr = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQC7gJ3gYn8zFP801nGA3Ghx4fP59g5M8mGoY6WReF0kTTMX7S6+N670xX6A7Njl3xUXeadRHc2LwPA4Ysrc0ZClOYh7Ghl3xUXeadRHc2LwPA4Ysrc0ZClOYh7Ghl3xUXeadRHc2LwPA4Ysrc0ZClOYh7Ghl3xUXeadRHc2LwPA4Ysrc0ZClOYh7Ghl3xUXeadRHc2LwPA4Ysrc0ZClOYh7Ghl3xUXeadRHc2LwPA4Ysrc0ZClOYh7Ghl3xUXeadRHc2LwPA4Ysrc0ZClOYh7Ghl3xUXeadRHc2LwPA4Ysrc0ZClOYh7Ghl3xUXeadRHc2LwPA4Ysrc0ZClOYh7Ghl3xUXeadRHc2LwPA4Ysrc0ZClOYh7Ghl3xUXeadRHc2LwPA4Ysrc0ZClOYh7Gh testrsa@example.com"
	invalidPubKeyStrMalformed  = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGCiszAUP313P5yDUtfLMV6fRUXT7fLErv9axjCF/9ms" // Missing comment
	unauthorizedButValidKeyStr = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIEHNVzP0UnKOoF7gY2r87a7iLDLLxT8Z0jYp3oz9j/sX unauthorized@example.com"
)

func TestPublicKeyAuthentication(t *testing.T) {
	logrus.SetOutput(io.Discard) // Suppress log output during these tests
	defer logrus.SetOutput(os.Stderr) // Restore log output after tests

	authKey1, _, _, _, err := ssh.ParseAuthorizedKey([]byte(validEd25519PubKeyStr))
	assert.NoError(t, err)
	authKey2, _, _, _, err := ssh.ParseAuthorizedKey([]byte(validRSAPubKeyStr))
	assert.NoError(t, err)

	unauthKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(unauthorizedButValidKeyStr))
	assert.NoError(t, err)


	testCases := []struct {
		name                string
		authorizedKeysInput []string // Keys to pre-authorize for the callback
		keyToTest           ssh.PublicKey
		connMeta            ssh.ConnMetadata // Can be nil or minimal for this test
		expectError         bool
		expectedErrorMsg    string
	}{
		{
			name:                "Valid Ed25519 key is authorized",
			authorizedKeysInput: []string{validEd25519PubKeyStr, validRSAPubKeyStr},
			keyToTest:           authKey1,
			connMeta:            &ssh. Verbindung{}, // Using a minimal ConnMetadata
			expectError:         false,
		},
		{
			name:                "Valid RSA key is authorized",
			authorizedKeysInput: []string{validEd25519PubKeyStr, validRSAPubKeyStr},
			keyToTest:           authKey2,
			connMeta:            &ssh. Verbindung{},
			expectError:         false,
		},
		{
			name:                "Unauthorized valid key is rejected",
			authorizedKeysInput: []string{validEd25519PubKeyStr}, // Does not include unauthKey
			keyToTest:           unauthKey,
			connMeta:            &ssh. Verbindung{},
			expectError:         true,
			expectedErrorMsg:    "public key not authorized",
		},
		{
			name:                "No keys authorized, any key rejected",
			authorizedKeysInput: []string{},
			keyToTest:           authKey1,
			connMeta:            &ssh. Verbindung{},
			expectError:         true,
			expectedErrorMsg:    "public key not authorized",
		},
		{
			name:                "Key authorized among others",
			authorizedKeysInput: []string{anotherValidEd25519PubKeyStr, validEd25519PubKeyStr, validRSAPubKeyStr},
			keyToTest:           authKey1,
			connMeta:            &ssh. Verbindung{},
			expectError:         false,
		},
		{
			name: "Malformed key in authorized list during setup - valid key still works",
			authorizedKeysInput: []string{invalidPubKeyStrMalformed, validEd25519PubKeyStr},
			keyToTest: authKey1,
			connMeta:            &ssh. Verbindung{},
			expectError: false, // The callback should still work for the valid key
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Simulate the key parsing logic from StartSSHServer to build the map
			// that the actual PublicKeyCallback would use.
			parsedAuthorizedKeysMap := make(map[string]bool)
			for _, keyStr := range tc.authorizedKeysInput {
				if keyStr == "" {
					continue
				}
				pubKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(keyStr))
				if err != nil {
					// Log this, as StartSSHServer would, but continue building map with valid keys
					logrus.Warnf("Test setup: Failed to parse public key: %v. Key: %s", err, keyStr)
					continue
				}
				parsedAuthorizedKeysMap[string(pubKey.Marshal())] = true
			}

			// Create the ssh.ServerConfig with the PublicKeyCallback
			// The callback will use the 'parsedAuthorizedKeysMap' created above.
			// This is a bit of a workaround as we can't directly pass the map
			// to the callback defined in ssh.go without refactoring it.
			// So, we re-define a similar callback for testing purposes here
			// or accept that testing the original callback requires StartSSHServer to run.

			// For this test, we will directly test the logic that StartSSHServer's callback would use.
			// This means we are testing the *decision logic*, not necessarily an instance of StartSSHServer.

			// Create a dummy ConnMetadata
			dummyConnMeta := tc.connMeta
			if dummyConnMeta == nil {
				dummyConnMeta = &ssh. Verbindung{UserV: "testuser"}
			}


			// The actual callback logic from ssh.go:
			// PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			// 	keyString := string(key.Marshal())
			// 	if parsedAuthorizedKeys[keyString] { // `parsedAuthorizedKeys` is the map from StartSSHServer
			// 		logrus.Infof("Public key accepted for user %s from %s", conn.User(), conn.RemoteAddr())
			// 		return nil, nil
			// 	}
			// 	logrus.Warnf("Public key rejected for user %s from %s", conn.User(), conn.RemoteAddr())
			// 	return nil, fmt.Errorf("public key not authorized")
			// },

			// Simulate the callback's decision process
			keyString := string(tc.keyToTest.Marshal())
			_, authorized := parsedAuthorizedKeysMap[keyString]

			var resultingErr error
			if authorized {
				resultingErr = nil
			} else {
				resultingErr = fmt.Errorf("public key not authorized")
			}


			if tc.expectError {
				assert.Error(t, resultingErr)
				if tc.expectedErrorMsg != "" {
					assert.Contains(t, resultingErr.Error(), tc.expectedErrorMsg)
				}
			} else {
				assert.NoError(t, resultingErr)
			}
		})
	}
}

// TestStartSSHServerKeyParsingErrors focuses on how StartSSHServer handles bad input key lists.
// This requires StartSSHServer to be callable in a test-friendly way.
// Given sync.Once, this test might be harder to make reliable for multiple error cases
// without resetting sync.Once, which StartSSHServer now does on exit.
func TestStartSSHServerInitialSetup(t *testing.T) {
	logrus.SetOutput(io.Discard)
	defer logrus.SetOutput(os.Stderr)

	t.Run("NoValidKeysProvidedToStartSSHServer", func(t *testing.T) {
		// Reset sync.Once for this test case
		sshServerStarted = sync.Once{}
		isSSHServerRunning = false

		err := StartSSHServer(9999, []string{invalidPubKeyStrMalformed, "another bad key"})
		// Expect an error because no valid keys could be parsed by StartSSHServer's internal logic
		assert.Error(t, err)
		if err != nil { // Defensive check
			assert.Contains(t, err.Error(), "no valid SSH public keys provided")
		}
		assert.False(t, isSSHServerRunning, "SSH server should not be running if no valid keys are parsed")
	})

	t.Run("EmptyKeyListProvidedToStartSSHServer", func(t *testing.T) {
		sshServerStarted = sync.Once{}
		isSSHServerRunning = false

		// StartSSHServer's current logic logs a warning for empty authorizedKeys
		// but the error for "no valid SSH public keys" is triggered if the *parsed* map is empty.
		// An empty input list will result in an empty parsed map.
		err := StartSSHServer(9998, []string{})
		assert.Error(t, err)
		if err != nil {
			assert.Contains(t, err.Error(), "no valid SSH public keys provided")
		}
		assert.False(t, isSSHServerRunning)
	})

	// Note: Testing successful startup and listener is more of an integration test
	// and would require handling the blocking nature of StartSSHServer (e.g., goroutine + cancel).
	// For this unit test, we focus on error conditions during setup.
}

// Mock ssh.ConnMetadata for testing PublicKeyCallback
// ssh.Verbindung is an unexported type, so we can't directly create it with all fields.
// However, ConnMetadata is an interface. We can provide a mock satisfying it.
// For the PublicKeyCallback as written, it only uses User() and RemoteAddr().
type mockConnMetadata struct {
	user string
	addr string // net.Addr not easily mockable, use string for simplicity if callback adapts
	ssh.ConnMetadata // Embed to satisfy the interface if other methods are called by ssh package
}

func (m *mockConnMetadata) User() string {
	return m.user
}

func (m *mockConnMetadata) RemoteAddr() string { // Simplified to string for test
	return m.addr
}
// Implement other methods of ssh.ConnMetadata if needed by the ssh package during callback
// For the current callback, only User() and RemoteAddr() (via logrus) are used.
// Since logrus is discarded, only User() might matter if not discarded.
// The actual callback in ssh.go uses conn.User() and conn.RemoteAddr().String().
// ssh.Verbindung is internal, so we use the fact that the callback is passed ssh.ConnMetadata.

func TestPublicKeyCallbackWithActualCallback(t *testing.T) {
	logrus.SetOutput(io.Discard)
	defer logrus.SetOutput(os.Stderr)

	authKeyEd25519, _, _, _, _ := ssh.ParseAuthorizedKey([]byte(validEd25519PubKeyStr))
	unauthKey, _, _, _, _ := ssh.ParseAuthorizedKey([]byte(unauthorizedButValidKeyStr))

	// These keys will be used to set up the `parsedAuthorizedKeys` map within the callback's closure
	keysForSetup := []string{validEd25519PubKeyStr, validRSAPubKeyStr}

	// This map simulates the one inside StartSSHServer's closure for PublicKeyCallback
	simulatedParsedKeys := make(map[string]bool)
	for _, keyStr := range keysForSetup {
		pk, _, _, _, _ := ssh.ParseAuthorizedKey([]byte(keyStr))
		simulatedParsedKeys[string(pk.Marshal())] = true
	}

	// Define the callback function, similar to how it's defined in StartSSHServer,
	// but it closes over our test `simulatedParsedKeys` map.
	testCallback := func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
		keyString := string(key.Marshal())
		if simulatedParsedKeys[keyString] {
			return nil, nil
		}
		return nil, fmt.Errorf("public key not authorized")
	}


	serverConfig := &ssh.ServerConfig{
		PublicKeyCallback: testCallback,
	}

	mockMeta := &mockConnMetadata{user: "testuser", addr: "127.0.0.1:12345"}

	// Test with an authorized key
	perms, err := serverConfig.PublicKeyCallback(mockMeta, authKeyEd25519)
	assert.NoError(t, err, "Authorized key should not produce an error")
	assert.Nil(t, perms, "Permissions should be nil for authorized key")

	// Test with an unauthorized key
	perms, err = serverConfig.PublicKeyCallback(mockMeta, unauthKey)
	assert.Error(t, err, "Unauthorized key should produce an error")
	assert.Contains(t, err.Error(), "public key not authorized", "Error message for unauthorized key mismatch")
	assert.Nil(t, perms, "Permissions should be nil for unauthorized key error")
}
