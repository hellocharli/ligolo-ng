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

package config

import (
	"bytes"
	"fmt"
	"net"
	"slices"
	"sort"
	"strings"

	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/nicocha30/ligolo-ng/pkg/proxy/netinfo"
)

// interface.name config
type InterfaceConfig struct {
	Routes []string
}

type InterfaceRoute struct {
	Destination string
	Active      bool
}

type InterfaceInfo struct {
	Routes []InterfaceRoute
	Active bool
}

func (i *InterfaceInfo) GetStateString() string {
	var activeRoutes, pendingRoutes int
	var stringBuffer []string
	for _, route := range i.Routes {
		if route.Active {
			activeRoutes++
		} else {
			pendingRoutes++
		}
	}
	if activeRoutes > 0 {
		stringBuffer = append(stringBuffer, text.Colors{text.FgGreen}.Sprintf("Active - %d routes", activeRoutes))
	}
	if pendingRoutes > 0 {
		stringBuffer = append(stringBuffer, text.Colors{text.FgYellow}.Sprintf("Pending - %d routes", pendingRoutes))
	}
	return strings.Join(stringBuffer, " / ")

}

func (i *InterfaceInfo) GetRoutes() (routes []string) {
	for _, route := range i.Routes {
		routes = append(routes, route.Destination)
	}
	return
}

func (i *InterfaceInfo) IsRouteActive(route string) bool {
	for _, routeRoute := range i.Routes {
		if routeRoute.Destination == route {
			return true
		}
	}
	return false
}

func (i *InterfaceInfo) GetRouteString() string {
	var stringBuffer []string
	for _, route := range i.Routes {
		if route.Active {
			stringBuffer = append(stringBuffer, text.Colors{text.FgGreen}.Sprintf(route.Destination))
		} else {
			stringBuffer = append(stringBuffer, text.Colors{text.FgYellow}.Sprintf(route.Destination))
		}
	}
	return strings.Join(stringBuffer, ",")
}

func GetInterfaceConfigState() (map[string]InterfaceInfo, error) {
	tuntaps, err := netinfo.GetTunTaps()
	if err != nil {
		return nil, err
	}

	interfaces := make(map[string]InterfaceInfo)
	// Read currently existing tuntaps on the system
	for _, tuntap := range tuntaps {
		ifInfo := InterfaceInfo{
			Active: true,
		}
		for _, route := range tuntap.Routes() {
			ifInfo.Routes = append(ifInfo.Routes, InterfaceRoute{
				Destination: route.Dst,
				Active:      true,
			})
		}
		interfaces[tuntap.Name()] = ifInfo
	}

	// Read interfaces from the configuration file
	var ifaceInfo map[string]InterfaceConfig
	Config.UnmarshalKey("interface", &ifaceInfo)

	for ifaceName, pendingTaps := range ifaceInfo {
		ifInfo := InterfaceInfo{
			Active: false,
		}
		// If interface already exist, but has pending routes
		if cInterface, ok := interfaces[ifaceName]; ok {
			ifInfo = cInterface
		}

		for _, route := range pendingTaps.Routes {
			if !ifInfo.IsRouteActive(route) {
				ifInfo.Routes = append(ifInfo.Routes, InterfaceRoute{
					Destination: route,
					Active:      false,
				})
			}
		}
		interfaces[ifaceName] = ifInfo
	}
	return interfaces, nil
}

func AddRouteConfig(ifName string, routeCidr string) error {
	// Validate CIDR format first
	if _, _, err := net.ParseCIDR(routeCidr); err != nil {
		return fmt.Errorf("invalid CIDR format for route %q: %w", routeCidr, err)
	}

	var ifaceInfo map[string]InterfaceConfig
	// Unmarshal current interfaces config
	Config.UnmarshalKey("interface", &ifaceInfo)

	// Sanity check
	if _, ok := ifaceInfo[ifName]; !ok {
		return fmt.Errorf("interface %s not found", ifName)
	}
	if slices.Contains(ifaceInfo[ifName].Routes, routeCidr) {
		// Route already exists
		return fmt.Errorf("route %s already exists", routeCidr)
	}
	// Add an entry
	ifaceInfo[ifName] = InterfaceConfig{
		Routes: append(ifaceInfo[ifName].Routes, routeCidr),
	}
	// Update the config
	Config.Set("interface", ifaceInfo)
	if err := Config.WriteConfig(); err != nil {
		return err
	}
	return nil
}

// ipNetContains checks if network a fully contains network b.
// It assumes a and b are valid non-nil net.IPNet objects.
func ipNetContains(a, b *net.IPNet) bool {
	// Check if IPs are of the same type (IPv4 or IPv6)
	// This is important because an IPv4-mapped IPv6 address could cause issues.
	// However, net.IPNet.Contains method handles this correctly.
	// If a.IP is 4-byte and b.IP is 16-byte, a.Contains(b.IP) can be true if b.IP is an IPv4 mapped address in a's range.
	// And vice-versa. For route sanitization, we typically deal with distinct IPv4 and IPv6 routes.
	// Let's assume for now that ParseCIDR and IPNet operations give us comparable types,
	// or that mixed-type containment implies distinct routing entries anyway.

	aMaskVal, _ := a.Mask.Size()
	bMaskVal, _ := b.Mask.Size()

	// A network 'a' can only contain 'b' if 'a' has a smaller or equal mask length (is broader or same specificity)
	// and b's network address falls within the range of 'a'.
	if aMaskVal > bMaskVal {
		return false
	}

	// Check if b's network address (IP masked with b's mask) is contained in a.
	// This is the most accurate check for network containment.
	return a.Contains(b.IP.Mask(b.Mask))
}

// SanitizeRoutes takes a list of CIDR strings, parses them, and returns a minimal
// list of unique, non-overlapping (or minimally overlapping, favoring broader) CIDRs.
// Routes that are invalid CIDRs will result in an error.
func SanitizeRoutes(routes []string) ([]string, error) {
	if len(routes) == 0 {
		return []string{}, nil
	}

	parsedRoutes := make([]*net.IPNet, 0, len(routes))
	for _, rStr := range routes {
		_, ipNet, err := net.ParseCIDR(rStr)
		if err != nil {
			return nil, fmt.Errorf("invalid CIDR string %q: %w", rStr, err)
		}
		parsedRoutes = append(parsedRoutes, ipNet)
	}

	// Sort routes:
	// 1. Broader networks first (smaller mask size).
	// 2. Then by IP address.
	sort.SliceStable(parsedRoutes, func(i, j int) bool {
		iMask, _ := parsedRoutes[i].Mask.Size()
		jMask, _ := parsedRoutes[j].Mask.Size()
		if iMask != jMask {
			return iMask < jMask // Smaller mask size (broader network) comes first
		}
		// If masks are equal, sort by IP address
		// IP.Mask ensures we are comparing network addresses for IPs that might not be network addresses themselves.
		return bytes.Compare(parsedRoutes[i].IP.Mask(parsedRoutes[i].Mask), parsedRoutes[j].IP.Mask(parsedRoutes[j].Mask)) < 0
	})

	if len(parsedRoutes) == 0 { // Should be caught by the first check, but good for safety.
		return []string{}, nil
	}

	sanitizedList := make([]*net.IPNet, 0)
	for _, currentNet := range parsedRoutes {
		isRedundant := false
		// Check if currentNet is contained by any network already in sanitizedList
		// Since broader networks come first due to sorting, currentNet (if not broader or equal)
		// could be contained by an existing one.
		for _, existingNet := range sanitizedList {
			if ipNetContains(existingNet, currentNet) {
				isRedundant = true
				break
			}
		}

		if !isRedundant {
			// Remove any networks from sanitizedList that are strictly contained by currentNet.
			// This is important if due to IP sorting, a narrower network was added before a broader one
			// that has a "larger" IP address but smaller mask. (e.g. 10.1.0.0/16 then 10.0.0.0/8)
			// With current sort (mask first), this path should be less common for strict containment,
			// as broader networks (like 10.0.0.0/8) would be processed before narrower ones (10.1.0.0/16).
			tempList := make([]*net.IPNet, 0, len(sanitizedList))
			for _, existingNet := range sanitizedList {
				if !ipNetContains(currentNet, existingNet) {
					tempList = append(tempList, existingNet)
				} else {
					// currentNet contains existingNet, so existingNet is redundant
				}
			}
			sanitizedList = tempList
			sanitizedList = append(sanitizedList, currentNet)
		}
	}

	// Convert back to string representations
	// Also, ensure no simple string duplicates if different original strings parsed to same net.IPNet.String()
	// (e.g. "10.0.0.1/8" and "10.0.0.2/8" both become "10.0.0.0/8" effectively if .String() normalizes)
	// net.IPNet.String() returns the CIDR notation (e.g. "192.168.1.0/24").
	finalRouteStrings := make([]string, 0, len(sanitizedList))
	seen := make(map[string]bool)
	for _, ipNet := range sanitizedList {
		str := ipNet.String()
		if !seen[str] {
			finalRouteStrings = append(finalRouteStrings, str)
			seen[str] = true
		}
	}
	return finalRouteStrings, nil
}

func DeleteRouteConfig(ifName string, routeCidr string) error {
	var ifaceInfo map[string]InterfaceConfig
	// Unmarshal current interfaces config
	Config.UnmarshalKey("interface", &ifaceInfo)
	// Sanity check
	if _, ok := ifaceInfo[ifName]; !ok {
		return fmt.Errorf("interface %s not found", ifName)
	}
	// Build the new routing table
	var newRouteTable []string
	for _, route := range ifaceInfo[ifName].Routes {
		if route != routeCidr {
			newRouteTable = append(newRouteTable, route)
		}
	}
	ifaceInfo[ifName] = InterfaceConfig{Routes: newRouteTable}
	// Update the config
	Config.Set("interface", ifaceInfo)
	if err := Config.WriteConfig(); err != nil {
		return err
	}
	return nil
}

func AddInterfaceConfig(ifName string) error {
	var ifaceInfo map[string]InterfaceConfig
	// Unmarshal current interfaces config
	Config.UnmarshalKey("interface", &ifaceInfo)
	// Check if empty interface
	if ifaceInfo == nil {
		ifaceInfo = make(map[string]InterfaceConfig)
	}
	// Add an entry
	ifaceInfo[ifName] = InterfaceConfig{
		Routes: nil,
	}
	// Update the config
	Config.Set("interface", ifaceInfo)
	if err := Config.WriteConfig(); err != nil {
		return err
	}
	return nil
}

func GetInterfaceConfig(ifName string) *InterfaceConfig {
	var ifaceInfo map[string]InterfaceConfig
	// Unmarshal current interfaces config
	Config.UnmarshalKey("interface", &ifaceInfo)
	// Check if empty interface
	if iface, ok := ifaceInfo[ifName]; ok {
		return &iface
	}
	return nil
}

func DeleteInterfaceConfig(ifName string) error {
	var ifaceInfo map[string]InterfaceConfig
	Config.UnmarshalKey("interface", &ifaceInfo)
	delete(ifaceInfo, ifName)
	Config.Set("interface", ifaceInfo)
	if err := Config.WriteConfig(); err != nil {
		return err
	}
	return nil
}
