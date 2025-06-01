// Ligolo-ng
// Copyright (C) 2025 Nicolas Chatelain (nicocha30)

// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.

// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package controller

import (
	"encoding/json"
	"fmt"
	"net"

	"github.com/hashicorp/yamux"
	"github.com/nicocha30/ligolo-ng/cmd/proxy/config" // Import proxy config
	"github.com/nicocha30/ligolo-ng/pkg/protocol"
	"github.com/nicocha30/ligolo-ng/pkg/proxy"
	"github.com/sirupsen/logrus" // Import logrus
)

type LigoloAgent struct {
	Name      string
	Network   []protocol.NetInterface
	Session   *yamux.Session
	SessionID string
	CloseChan chan bool `json:"-"`
	Interface string
	Running   bool
	Listeners []*proxy.LigoloListener
}

func (la *LigoloAgent) Alive() bool {
	if la.Session != nil && !la.Session.IsClosed() {
		return true
	}
	return false
}

func (la *LigoloAgent) Kill() error {
	// Open a new Yamux Session
	conn, err := la.Session.Open()
	if err != nil {
		return err
	}
	defer conn.Close()

	ligoloProtocol := protocol.NewEncoderDecoder(conn)

	// Request to kill the agent
	if err := ligoloProtocol.Encode(protocol.AgentKillRequestPacket{}); err != nil {
		return err
	}
	return nil
}

func (la *LigoloAgent) AddListener(addr string, network string, to string) (*proxy.LigoloListener, error) {
	if _, _, err := net.SplitHostPort(addr); err != nil {
		return nil, fmt.Errorf("invalid listener addr: %v", err)
	}
	if _, _, err := net.SplitHostPort(to); err != nil {
		return nil, fmt.Errorf("invalid redirect addr: %v", err)
	}
	proxyListener, err := proxy.NewListener(la.Session, addr, network, to)
	if err != nil {
		return nil, err
	}
	la.Listeners = append(la.Listeners, &proxyListener)
	return &proxyListener, nil
}

func (la *LigoloAgent) GetListener(id int) *proxy.LigoloListener {
	for _, listener := range la.Listeners {
		if listener.ID == int32(id) {
			return listener
		}
	}
	return nil
}

func (la *LigoloAgent) DeleteListener(id int) {
	for i, listener := range la.Listeners {
		if listener.ID == int32(id) {
			la.Listeners = append(la.Listeners[:i], la.Listeners[i+1:]...)
		}
	}
}

func (la *LigoloAgent) String() string {
	raddr := "[Offline]"
	if la.Session != nil {
		raddr = la.Session.RemoteAddr().String()
	}

	return fmt.Sprintf("%s - %s - %s", la.Name, raddr, la.SessionID)
}

func (la *LigoloAgent) MarshalJSON() ([]byte, error) {
	type Session struct {
		Name       string
		Network    []protocol.NetInterface
		SessionID  string
		RemoteAddr string
		Interface  string
		Running    bool
		Listeners  []*proxy.LigoloListener
	}

	return json.Marshal(Session{
		Name:       la.Name,
		Running:    la.Running,
		Listeners:  la.Listeners,
		Network:    la.Network,
		Interface:  la.Interface,
		SessionID:  la.SessionID,
		RemoteAddr: la.Session.RemoteAddr().String(),
	})
}

func NewAgent(session *yamux.Session) (*LigoloAgent, error) {
	yamuxConnectionSession, err := session.Open()
	if err != nil {
		return nil, fmt.Errorf("failed to open stream for agent info: %w", err)
	}
	defer yamuxConnectionSession.Close() // Ensure stream is closed

	// Initial Info Request
	infoRequest := protocol.InfoRequestPacket{}
	protocolEncoder := protocol.NewEncoder(yamuxConnectionSession)
	protocolDecoder := protocol.NewDecoder(yamuxConnectionSession)

	logrus.Debugf("Sending InfoRequestPacket to new agent on stream %d", yamuxConnectionSession.StreamID())
	if err := protocolEncoder.Encode(infoRequest); err != nil {
		return nil, fmt.Errorf("failed to send InfoRequestPacket: %w", err)
	}

	logrus.Debugf("Waiting for InfoReplyPacket from new agent on stream %d", yamuxConnectionSession.StreamID())
	if err := protocolDecoder.Decode(); err != nil {
		return nil, fmt.Errorf("failed to decode InfoReplyPacket: %w", err)
	}

	reply, ok := protocolDecoder.Payload.(*protocol.InfoReplyPacket)
	if !ok {
		return nil, fmt.Errorf("received unexpected packet type instead of InfoReplyPacket")
	}
	logrus.Infof("Received InfoReplyPacket from agent: %s (SessionID: %s)", reply.Name, reply.SessionID)

	// SSH Configuration Request
	sshPort := config.Config.GetInt("ssh.port")
	sshPublicKeys := config.Config.GetStringSlice("ssh.publickeys")

	if sshPort <= 0 {
		logrus.Infof("SSH configuration skipped for agent %s: SSH port (%d) not configured or disabled.", reply.Name, sshPort)
	} else if len(sshPublicKeys) == 0 {
		logrus.Infof("SSH configuration skipped for agent %s: No SSH public keys configured.", reply.Name)
	} else {
		logrus.Debugf("Sending SSHConfigRequestPacket to agent %s (Port: %d, Keys: %d) on stream %d", reply.Name, sshPort, len(sshPublicKeys), yamuxConnectionSession.StreamID())
		sshConfigPacket := protocol.SSHConfigRequestPacket{
			SSHPort:       sshPort,
			SSHPublicKeys: sshPublicKeys,
		}
		// Reuse the same encoder and decoder with the stream
		if err := protocolEncoder.Encode(sshConfigPacket); err != nil {
			// Log error but proceed to create agent object. SSH functionality might be impaired.
			logrus.Errorf("Failed to send SSHConfigRequestPacket to agent %s: %v", reply.Name, err)
		} else {
			logrus.Debugf("Waiting for SSHConfigResponsePacket from agent %s on stream %d", reply.Name, yamuxConnectionSession.StreamID())
			// Important: Decode expects to read from the stream.
			if err := protocolDecoder.Decode(); err != nil {
				logrus.Errorf("Failed to decode SSHConfigResponsePacket from agent %s: %v", reply.Name, err)
			} else {
				sshResponse, ok := protocolDecoder.Payload.(*protocol.SSHConfigResponsePacket)
				if !ok {
					logrus.Errorf("Received unexpected packet type from agent %s instead of SSHConfigResponsePacket", reply.Name)
				} else {
					if sshResponse.Success {
						logrus.Infof("SSH configuration successfully applied by agent %s.", reply.Name)
					} else {
						logrus.Errorf("Agent %s failed to apply SSH configuration: %s", reply.Name, sshResponse.Error)
					}
				}
			}
		}
	}

	return &LigoloAgent{
		Name:      reply.Name,
		Network:   reply.Interfaces,
		Session:   session,
		SessionID: reply.SessionID,
		CloseChan: make(chan bool),
	}, nil
}
