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

package protocol

import (
	"bytes"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEncodeDecode(t *testing.T) {
	var buffer bytes.Buffer

	baseEnvelope := InfoReplyPacket{Name: "hello"}
	enc := NewEncoder(&buffer)
	if err := enc.Encode(baseEnvelope); err != nil {
		t.Fatal(err)
	}

	fmt.Printf("Envelope created: %+v\n", buffer)

	dec := NewDecoder(&buffer)
	if err := dec.Decode(); err != nil {
		if err != io.EOF {
			t.Fatal(err)
		}
	}

	fmt.Printf("Envelope: %+v\n", dec.Payload)

	if dec.Payload.(*InfoReplyPacket).Name != "hello" {
		t.Fatal("invalid packet decoded")
	}

}

func TestSSHConfigRequestPacket_EncodeDecode(t *testing.T) {
	originalPacket := &SSHConfigRequestPacket{
		SSHPort:       2222,
		SSHPublicKeys: []string{"ssh-rsa KEY1", "ssh-ed25519 KEY2"},
	}

	var network bytes.Buffer
	encoder := NewEncoder(&network)
	err := encoder.Encode(originalPacket)
	assert.NoError(t, err, "Encoding SSHConfigRequestPacket failed")

	decoder := NewDecoder(&network)
	err = decoder.Decode()
	assert.NoError(t, err, "Decoding SSHConfigRequestPacket failed")

	decodedPacket, ok := decoder.Payload.(*SSHConfigRequestPacket)
	assert.True(t, ok, "Decoded payload is not of type SSHConfigRequestPacket")

	assert.Equal(t, originalPacket.SSHPort, decodedPacket.SSHPort, "SSHPort mismatch")
	assert.Equal(t, originalPacket.SSHPublicKeys, decodedPacket.SSHPublicKeys, "SSHPublicKeys mismatch")
}

func TestSSHConfigResponsePacket_EncodeDecode(t *testing.T) {
	t.Run("SuccessCase", func(t *testing.T) {
		originalPacket := &SSHConfigResponsePacket{
			Success: true,
			Error:   "",
		}

		var network bytes.Buffer
		encoder := NewEncoder(&network)
		err := encoder.Encode(originalPacket)
		assert.NoError(t, err, "Encoding SSHConfigResponsePacket (success) failed")

		decoder := NewDecoder(&network)
		err = decoder.Decode()
		assert.NoError(t, err, "Decoding SSHConfigResponsePacket (success) failed")

		decodedPacket, ok := decoder.Payload.(*SSHConfigResponsePacket)
		assert.True(t, ok, "Decoded payload is not of type SSHConfigResponsePacket")

		assert.Equal(t, originalPacket.Success, decodedPacket.Success, "Success field mismatch")
		assert.Equal(t, originalPacket.Error, decodedPacket.Error, "Error field mismatch")
	})

	t.Run("FailureCase", func(t *testing.T) {
		originalPacket := &SSHConfigResponsePacket{
			Success: false,
			Error:   "Failed to start SSH server due to port conflict",
		}

		var network bytes.Buffer
		encoder := NewEncoder(&network)
		err := encoder.Encode(originalPacket)
		assert.NoError(t, err, "Encoding SSHConfigResponsePacket (failure) failed")

		decoder := NewDecoder(&network)
		err = decoder.Decode()
		assert.NoError(t, err, "Decoding SSHConfigResponsePacket (failure) failed")

		decodedPacket, ok := decoder.Payload.(*SSHConfigResponsePacket)
		assert.True(t, ok, "Decoded payload is not of type SSHConfigResponsePacket")

		assert.Equal(t, originalPacket.Success, decodedPacket.Success, "Success field mismatch")
		assert.Equal(t, originalPacket.Error, decodedPacket.Error, "Error field mismatch")
	})
}

func BenchmarkEncodeDecode(b *testing.B) {
	for i := 0; i < b.N; i++ {
		var buffer bytes.Buffer
		baseEnvelope := InfoReplyPacket{Name: "hello"}
		enc := NewEncoder(&buffer)
		if err := enc.Encode(baseEnvelope); err != nil {
			b.Fatal(err)
		}

		dec := NewDecoder(&buffer)
		if err := dec.Decode(); err != nil {
			if err != io.EOF {
				b.Fatal(err)
			}
		}

		if dec.Payload.(*InfoReplyPacket).Name != "hello" {
			b.Fatal("invalid packet decoded")
		}
	}
}
