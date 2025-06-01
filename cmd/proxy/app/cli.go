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

package app

import (
	"fmt"
	"strconv"

	"github.com/AlecAivazis/survey/v2"
	"github.com/desertbit/grumble"
	"github.com/nicocha30/ligolo-ng/cmd/proxy/config"
)

// App is used to register the grumble
var App = grumble.New(&grumble.Config{
	Name:                  "ligolo-ng",
	Description:           "Ligolo-ng - An advanced, yet simple tunneling tool",
	HelpHeadlineUnderline: true,
	HelpSubCommands:       true,
	HistoryFile:           "ligolo-ng.history",
})

func init() {
	App.AddCommand(sshKeyCmd)
	App.AddCommand(sshPortCmd)
}

func ask(question string) bool {
	result := false
	prompt := &survey.Confirm{
		Message: question,
	}
	survey.AskOne(prompt, &result)
	return result
}

// SSH Key Management
var sshKeyCmd = &grumble.Command{
	Name:     "sshkey",
	Help:     "manage ssh authorized public keys for agent SSH access",
	LongHelp: "Manages SSH authorized public keys for agent SSH access. Allows adding, deleting, and listing keys that agents will use to authorize SSH connections.",
	HelpFlag: false, // Disable automatic help flag
	// Aliases:  []string{"sk"}, // Example if you want aliases
}

var sshKeyAddCmd = &grumble.Command{
	Name: "add",
	Help: "add a new ssh public key",
	Args: func(a *grumble.Args) {
		a.String("publickey", "The public key string to add")
	},
	Run: func(c *grumble.Context) error {
		publicKey := c.Args.String("publickey")
		if publicKey == "" {
			c.App.PrintError(fmt.Errorf("public key cannot be empty"))
			return nil
		}

		keys := config.Config.GetStringSlice("ssh.publickeys")
		for _, k := range keys {
			if k == publicKey {
				c.App.PrintError(fmt.Errorf("public key already exists"))
				return nil
			}
		}
		keys = append(keys, publicKey)
		config.Config.Set("ssh.publickeys", keys)
		if err := config.Config.WriteConfig(); err != nil {
			c.App.PrintError(fmt.Errorf("failed to save config: %v", err))
			return nil
		}
		c.App.Println("SSH public key added successfully.")
		return nil
	},
}

var sshKeyDelCmd = &grumble.Command{
	Name: "del",
	Help: "delete an ssh public key",
	Run: func(c *grumble.Context) error {
		keys := config.Config.GetStringSlice("ssh.publickeys")
		if len(keys) == 0 {
			c.App.Println("No SSH keys to delete.")
			return nil
		}

		c.App.Println("Current SSH public keys:")
		for i, key := range keys {
			c.App.Printf("%d: %s\n", i+1, key)
		}

		var toDeleteStr string
		prompt := &survey.Input{
			Message: "Enter the number of the key to delete:",
		}
		survey.AskOne(prompt, &toDeleteStr, survey.WithValidator(survey.Required))

		toDelete, err := strconv.Atoi(toDeleteStr)
		if err != nil || toDelete < 1 || toDelete > len(keys) {
			c.App.PrintError(fmt.Errorf("invalid selection"))
			return nil
		}

		keys = append(keys[:toDelete-1], keys[toDelete:]...)
		config.Config.Set("ssh.publickeys", keys)
		if err := config.Config.WriteConfig(); err != nil {
			c.App.PrintError(fmt.Errorf("failed to save config: %v", err))
			return nil
		}
		c.App.Println("SSH public key deleted successfully.")
		return nil
	},
}

var sshKeyListCmd = &grumble.Command{
	Name: "list",
	Help: "list configured ssh public keys",
	Run: func(c *grumble.Context) error {
		keys := config.Config.GetStringSlice("ssh.publickeys")
		if len(keys) == 0 {
			c.App.Println("No SSH keys configured.")
			return nil
		}
		c.App.Println("Configured SSH public keys:")
		for _, key := range keys {
			c.App.Println(key)
		}
		return nil
	},
}

// SSH Port Management
var sshPortCmd = &grumble.Command{
	Name: "sshport",
	Help: "manage the ssh port for agents",
	LongHelp: "Sets the SSH port that agents will use for the built-in SSH server. This port will be communicated to agents when they connect.",
	HelpFlag: false, // Disable automatic help flag
	Args: func(a *grumble.Args) {
		a.Int("port", "The SSH port number (1-65535)")
	},
	Run: func(c *grumble.Context) error {
		port := c.Args.Int("port")
		if port < 1 || port > 65535 {
			c.App.PrintError(fmt.Errorf("invalid port number. Must be between 1 and 65535"))
			return nil
		}
		config.Config.Set("ssh.port", port)
		if err := config.Config.WriteConfig(); err != nil {
			c.App.PrintError(fmt.Errorf("failed to save config: %v", err))
			return nil
		}
		c.App.Printf("SSH port set to %d successfully.\n", port)
		return nil
	},
}

func init() {
	sshKeyCmd.AddCommand(sshKeyAddCmd)
	sshKeyCmd.AddCommand(sshKeyDelCmd)
	sshKeyCmd.AddCommand(sshKeyListCmd)
	App.AddCommand(sshKeyCmd)
	App.AddCommand(sshPortCmd)
}
