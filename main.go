package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	"os"
	"strings"
)

func randRead(b []byte) { rand.Read(b) }
func hexEncode(b []byte) string { return hex.EncodeToString(b) }
func randInt63n(n int64) int64 {
	if n <= 0 {
		return 0
	}
	v, err := rand.Int(rand.Reader, bigFromInt64(n))
	if err != nil {
		return 0
	}
	return v.Int64()
}

func bigFromInt64(n int64) *big.Int {
	return big.NewInt(n)
}

// main dispatches subcommands: http, tcp, enroll, devices, server, login, token, config.
func usage() {
	fmt.Fprintf(os.Stderr, `dreamTunnel — instant public tunnels

Usage:
  dt http <local-port> [--sub name]   expose a local HTTP service
  dt tcp <local-port> [--sub name]    expose a raw TCP service
  dt enroll [--server a:b]            register this device, save its own token
  dt devices [list]                   list devices enrolled on this server
  dt devices revoke <id> [--ban-ip]   revoke a device (and ban its IP)
  dt devices restore <id>             give a device its token back
  dt login                            write server+token into config
  dt token <value>                    print the obfuscated token form
  dt config                           show current config
  dt server [--config /etc/dreamtunnel/server.conf]   run the relay server

Options for "dt http" / "dt tcp":
  --sub <name>     requested subdomain label
  --server a:b     control endpoint (default tunnel.swag.best:444)
  --json           emit NDJSON events on stdout (ready/enrolled/disconnected/...)

Environment:
  DT_TOKEN         access token (this device's own token, issued automatically)
  DT_SERVER        control endpoint
  DT_SUB           requested subdomain label
  DT_CONFIG        config file path (default ~/.dreamtunnel/config.env)

Server environment (dt server):
  DT_ENROLL_TOKEN  shared token that lets a device enroll
  DT_REQUIRE_DEVICE_TOKENS=1   refuse the shared token as a tunnel credential
  DT_DEVICES_FILE  device store (default /var/lib/dreamtunnel/devices.json)
`)
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	var err error
	cmd, args := os.Args[1], os.Args[2:]
	switch cmd {
	case "http", "tcp":
		err = runTunnel(cmd, args)
	case "enroll":
		err = runEnroll(args)
	case "devices":
		err = runDevices(args)
	case "server":
		err = runServer(args)
	case "login":
		err = runLogin(args)
	case "token":
		err = runToken(args)
	case "config":
		err = runConfigShow()
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
		usage()
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "[dt] error: %v\n", err)
		os.Exit(1)
	}
}

// runLogin stores DT_SERVER / DT_TOKEN (accepts obfuscated hex token).
func runLogin(args []string) error {
	var server, token string
	fmt.Print("server [tunnel.swag.best:444]: ")
	fmt.Scanln(&server)
	if server == "" {
		server = "tunnel.swag.best:444"
	}
	fmt.Print("token: ")
	fmt.Scanln(&token)
	if token == "" {
		return fmt.Errorf("token is required")
	}
	cfg := loadClientConfig()
	cfg.Server = server
	cfg.Token = token
	if cfg.Sess == "" {
		b := make([]byte, 8)
		randRead(b)
		cfg.Sess = hexEncode(b)
	}
	if err := saveClientConfig(cfg); err != nil {
		return err
	}
	fmt.Println("[dt] saved to", clientConfigPath())
	return nil
}

// runToken prints the obfuscated form of a master token.
func runToken(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: dt token <value>")
	}
	ob, err := tokenObfuscate(ObfuscationPass, args[0])
	if err != nil {
		return err
	}
	fmt.Println(ob)
	return nil
}

func runConfigShow() error {
	cfg := loadClientConfig()
	fmt.Println("server:", cfg.Server)
	switch {
	case cfg.Token != "":
		if id, ok := deviceIDFromToken(cfg.Token); ok {
			fmt.Println("token: токен этого устройства, id", id)
		} else {
			fmt.Println("token: <set, len", len(cfg.Token), ">")
		}
	case DefaultPublicToken != "":
		fmt.Println("token: <встроенный токен — при первом запуске будет выдан свой>")
	default:
		fmt.Println("token: <not set>")
	}
	fmt.Println("sub:", cfg.Sub)
	fmt.Println("session:", cfg.Sess)
	fmt.Println("config:", clientConfigPath())
	return nil
}

// deviceIDFromToken reports whether a stored token is a per-device one and
// returns its id, so `dt config` can say which device this is.
func deviceIDFromToken(tok string) (string, bool) {
	if isHex(tok) {
		if de, err := tokenDeobfuscate(ObfuscationPass, tok); err == nil {
			tok = de
		}
	}
	if !strings.HasPrefix(tok, deviceTokenPrefix) {
		return "", false
	}
	rest := strings.TrimPrefix(tok, deviceTokenPrefix)
	i := strings.IndexByte(rest, '_')
	if i <= 0 {
		return "", false
	}
	return rest[:i], true
}
