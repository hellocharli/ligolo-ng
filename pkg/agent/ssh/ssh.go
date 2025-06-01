package ssh

import (
	"fmt"
	"io"
	"net"
	"os/exec"
	"os/user"
	"sync" // Included as per instruction, may remove if unused

	"errors"
	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh"
	"os" // Added for os.Environ
)

var (
	sshServerStarted   sync.Once
	isSSHServerRunning bool // Potentially useful for status checks later
	// mu                 sync.Mutex // To protect isSSHServerRunning if accessed concurrently outside of sync.Once
)

// StartSSHServer starts an SSH server on the given port with the provided authorized keys.
// This function is intended to be called as a goroutine and will block, running the SSH server.
// It uses sync.Once to ensure the server core logic is only started once per agent lifecycle (or until reset).
func StartSSHServer(listenPort int, authorizedKeys []string) error {
	var startErr error
	var serverConfigRef *ssh.ServerConfig // To pass the config to the accept loop goroutine

	sshServerStarted.Do(func() {
		logrus.Info("sshServerStarted.Do() called.")
		if len(authorizedKeys) == 0 {
			logrus.Warnf("SSH server starting without any authorized keys. No one will be able to connect.")
		}

		parsedAuthorizedKeys := make(map[string]bool)
		for _, keyStr := range authorizedKeys {
			if keyStr == "" { // Skip empty key strings
				continue
			}
			pubKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(keyStr))
			if err != nil {
				logrus.Warnf("Failed to parse public key: %v. Key: %s", err, keyStr)
				continue
			}
			parsedAuthorizedKeys[string(pubKey.Marshal())] = true
			logrus.Debugf("Successfully parsed and added an SSH authorized key.")
		}

		if len(parsedAuthorizedKeys) == 0 {
			startErr = fmt.Errorf("no valid SSH public keys provided, SSH server will not start")
			logrus.Error(startErr)
			// Resetting sync.Once here allows another attempt if a new config comes
			sshServerStarted = sync.Once{}
			return
		}

		serverConfig := &ssh.ServerConfig{
			PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
				keyString := string(key.Marshal())
				if parsedAuthorizedKeys[keyString] {
					logrus.Infof("Public key accepted for user %s from %s", conn.User(), conn.RemoteAddr())
					return nil, nil // Accept key
				}
				logrus.Warnf("Public key rejected for user %s from %s", conn.User(), conn.RemoteAddr())
				return nil, fmt.Errorf("public key not authorized")
			},
		}
		serverConfigRef = serverConfig // Assign to outer scope variable

		listener, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", listenPort))
		if err != nil {
			startErr = fmt.Errorf("failed to start SSH listener on port %d: %w", listenPort, err)
			logrus.Error(startErr)
			// Reset sync.Once here to allow another attempt if a new config comes
			sshServerStarted = sync.Once{}
			return
		}

		logrus.Infof("SSH Server listening on 0.0.0.0:%d", listenPort)
		// mu.Lock()
		isSSHServerRunning = true
		// mu.Unlock()

		defer func() {
			listener.Close()
			// mu.Lock()
			isSSHServerRunning = false
			// mu.Unlock()
			logrus.Info("SSH Server stopped and listener closed.")
			// Reset sshServerStarted to allow a new server to start if StartSSHServer is called again
			// (e.g. after agent reconnects or receives a new config).
			sshServerStarted = sync.Once{}
			logrus.Info("sshServerStarted has been reset. SSH server can be started again.")
		}()

		for {
			nConn, err := listener.Accept()
			if err != nil {
				if errors.Is(err, net.ErrClosed) {
					logrus.Info("SSH listener closed, server shutting down gracefully.")
				} else {
					logrus.Errorf("Error accepting SSH connection: %v", err)
				}
				// mu.Lock()
				isSSHServerRunning = false // Ensure flag is cleared on error/shutdown
				// mu.Unlock()
				// startErr might be shadowed here if we assign to it, better to just log and break
				break // Exit loop if listener is closed or errors
			}
			logrus.Debugf("Accepted new SSH connection from %s", nConn.RemoteAddr())
			// Pass serverConfigRef, not serverConfig from the outer scope directly if it changes
			go handleSSHConnection(nConn, serverConfigRef)
		}
	})

	// This error is from the sshServerStarted.Do() block, specifically if it fails early (e.g. port bind)
	return startErr
}

// IsRunning can be used to check if the server's accept loop is active.
// Needs mutex protection if accessed from multiple goroutines.
// func IsSSHServerRunning() bool {
// 	mu.Lock()
// 	defer mu.Unlock()
// 	return isSSHServerRunning
// }

func handleSSHConnection(nConn net.Conn, config *ssh.ServerConfig) {
// Placeholder for potential future use or if other parts of the agent need it.
// For now, this package is self-contained for starting the SSH server.
var (
	_ = io.EOF       // Example to use io if not otherwise used
	_ = sync.Mutex{} // Example to use sync if not otherwise used
	_ = os.Environ   // Ensure os is used
)
	defer nConn.Close()
	sconn, chans, reqs, err := ssh.NewServerConn(nConn, config)
	if err != nil {
		logrus.Warnf("Failed SSH handshake with %s: %v", nConn.RemoteAddr(), err)
		return
	}
	defer sconn.Close()

	logrus.Infof("New SSH connection from %s (%s)", sconn.RemoteAddr(), sconn.User())
	go ssh.DiscardRequests(reqs) // Discard out-of-band requests

	for newChannel := range chans {
		logrus.Debugf("Incoming channel type: %s from %s", newChannel.ChannelType(), sconn.RemoteAddr())
		if newChannel.ChannelType() != "session" {
			newChannel.Reject(ssh.UnknownChannelType, "unknown channel type")
			logrus.Debugf("Rejected unknown channel type %s", newChannel.ChannelType())
			continue
		}

		channel, requests, err := newChannel.Accept()
		if err != nil {
			logrus.Warnf("Could not accept channel from %s: %v", sconn.RemoteAddr(), err)
			continue
		}
		logrus.Debugf("Accepted 'session' channel from %s", sconn.RemoteAddr())
		go handleSession(channel, requests)
	}
	logrus.Infof("SSH connection closed for %s (%s)", sconn.RemoteAddr(), sconn.User())
}

func handleSession(channel ssh.Channel, requests <-chan *ssh.Request) {
	defer func() {
		logrus.Debugf("Closing session channel.")
		channel.Close()
	}()

	// Get current user information to set HOME and USER environment variables
	currentUser, err := user.Current()
	var env []string
	if err != nil {
		logrus.Warnf("Failed to get current user: %v. Shell environment might be incomplete.", err)
		env = []string{} // Keep default environment
	} else {
		env = append(os.Environ(), "HOME="+currentUser.HomeDir, "USER="+currentUser.Username, "LOGNAME="+currentUser.Username)
		// PS1 might be nice for some shells, but not essential
		// env = append(env, "PS1=[ligolo-agent]$ ")
	}


	for req := range requests {
		logrus.Debugf("Received request type: %s, WantReply: %t", req.Type, req.WantReply)
		switch req.Type {
		case "shell":
			// For now, we only support "shell"
			// You can also support "exec" for non-interactive commands
			cmd := exec.Command("/bin/sh") // Assuming Linux/macOS. For Windows: "cmd.exe"
			cmd.Env = env

			cmd.Stdout = channel
			cmd.Stderr = channel
			cmd.Stdin = channel

			// We don't handle PTY requests specifically yet for simplicity,
			// but this setup should provide a basic shell.
			// More advanced shells might require PTY handling.

			err := cmd.Start()
			if err != nil {
				logrus.Errorf("Failed to start shell for SSH session: %v", err)
				if req.WantReply {
					req.Reply(false, []byte(fmt.Sprintf("failed to start shell: %v", err)))
				}
				return // Close session on error
			}

			if req.WantReply {
				req.Reply(true, nil) // Signal success
				logrus.Debug("Shell request replied true.")
			}

			// Goroutine to wait for the command to finish and then close the channel
			go func() {
				waitErr := cmd.Wait()
				if waitErr != nil {
					logrus.Warnf("Shell command exited with error: %v", waitErr)
				} else {
					logrus.Debug("Shell command exited cleanly.")
				}
				channel.Close() // Ensure channel is closed when shell exits
				logrus.Debug("Shell goroutine finished, channel closed.")
			}()
			// Important: After starting a shell, we don't process further requests on this session.
			// The shell now "owns" the channel.
			return

		case "pty-req":
			// Acknowledge PTY request to make interactive shells work better.
			// We don't actually allocate a PTY here, but replying true helps.
			// Proper PTY handling involves parsing payload (term, w, h, wpx, hpx).
			if req.WantReply {
				req.Reply(true, nil)
				logrus.Debug("PTY request replied true (basic acknowledgment).")
			}

		case "env":
			// Client might request to set environment variables.
			// We are already setting some basic ones.
			// For simplicity, just acknowledge.
			// Proper handling would parse req.Payload.
			// type envRequestPayload struct { Name string; Value string }
			// var payload envRequestPayload
			// if err := ssh.Unmarshal(req.Payload, &payload); err == nil {
			//    log.Printf("Received env request: %s=%s", payload.Name, payload.Value)
			// }
			if req.WantReply {
				req.Reply(true, nil)
				logrus.Debug("Env request replied true (basic acknowledgment).")
			}

		case "subsystem":
			// Example: sftp
			// For now, reject subsystem requests
			logrus.Warnf("Subsystem request type '%s' not supported", string(req.Payload))
			if req.WantReply {
				req.Reply(false, []byte("subsystem not supported"))
			}

		default:
			logrus.Warnf("Unsupported request type: %s", req.Type)
			if req.WantReply {
				req.Reply(false, nil)
			}
		}
	}
}

// Placeholder for potential future use or if other parts of the agent need it.
// For now, this package is self-contained for starting the SSH server.
var (
	_ = io.EOF // Example to use io if not otherwise used
	_ = sync.Mutex{} // Example to use sync if not otherwise used
)
