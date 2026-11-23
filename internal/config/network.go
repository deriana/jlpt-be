package config

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// GetWindowsHostIP detects the Windows host gateway IP when running inside WSL2
// by reading /proc/net/route or /etc/resolv.conf. Falls back to 127.0.0.1.
func GetWindowsHostIP() string {
	// 1. Check /proc/net/route default gateway (00000000)
	if file, err := os.Open("/proc/net/route"); err == nil {
		defer file.Close()
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) > 2 && fields[1] == "00000000" {
				if gwHex, err := strconv.ParseUint(fields[2], 16, 32); err == nil {
					ipBytes := make([]byte, 4)
					binary.LittleEndian.PutUint32(ipBytes, uint32(gwHex))
					ip := net.IP(ipBytes).String()
					if ip != "" && ip != "0.0.0.0" {
						return ip
					}
				}
			}
		}
	}

	// 2. Fallback to /etc/resolv.conf nameserver
	if file, err := os.Open("/etc/resolv.conf"); err == nil {
		defer file.Close()
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "nameserver") {
				parts := strings.Fields(line)
				if len(parts) >= 2 {
					return parts[1]
				}
			}
		}
	}

	return "127.0.0.1"
}

// ResolveServiceURL probes localhost/127.0.0.1 first, and if running under WSL2 where
// services like VOICEVOX (Windows port 50021) or Ollama (Windows port 11434) live on the
// Windows host, automatically resolves to the Windows host gateway IP.
func ResolveServiceURL(rawURL string, probePath string, timeout time.Duration) (resolvedURL string, isReachable bool, hostIP string) {
	hostIP = GetWindowsHostIP()
	cleanURL := strings.TrimRight(strings.TrimSpace(rawURL), "/")
	if cleanURL == "" {
		return cleanURL, false, hostIP
	}

	client := &http.Client{Timeout: timeout}
	buildProbe := func(base string) string {
		if probePath == "" {
			return base
		}
		return fmt.Sprintf("%s/%s", strings.TrimRight(base, "/"), strings.TrimLeft(probePath, "/"))
	}

	// Probe configured URL directly first
	if resp, err := client.Get(buildProbe(cleanURL)); err == nil {
		resp.Body.Close()
		if resp.StatusCode < 500 {
			return cleanURL, true, hostIP
		}
	}

	// If URL uses localhost or 127.0.0.1 and we have a distinct Windows host IP in WSL2, probe Windows host
	if (strings.Contains(cleanURL, "127.0.0.1") || strings.Contains(cleanURL, "localhost")) && hostIP != "" && hostIP != "127.0.0.1" {
		winURL := strings.ReplaceAll(cleanURL, "127.0.0.1", hostIP)
		winURL = strings.ReplaceAll(winURL, "localhost", hostIP)
		if resp, err := client.Get(buildProbe(winURL)); err == nil {
			resp.Body.Close()
			if resp.StatusCode < 500 {
				return winURL, true, hostIP
			}
		}
		// Return the Windows-mapped URL so the user can see the auto-resolved WSL2 gateway target
		return winURL, false, hostIP
	}

	return cleanURL, false, hostIP
}
