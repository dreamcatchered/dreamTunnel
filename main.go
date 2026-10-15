package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	"os"
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

// main dispatches subcommands: http, tcp, server, login, token, config.
func usage() {
	fmt.Fprintf(os.Stderr, `dreamTunnel — instant public tunnels

Usage:
  dt http <local-port> [--sub name]   expose a local HTTP service
  dt tcp <local-port> [--sub name]    expose a raw TCP service
  dt login                            write server+token into config
  dt token <value>                    print the obfuscated token form
  dt config                           show current config
  dt server [--config /etc/dreamtunnel/server.conf]   run the relay server

Options for "dt http" / "dt tcp":
  --sub <name>     requested subdomain label
  --server a:b     control endpoint (default tunnel.swag.best:444)
  --json           emit NDJSON events on stdout (ready/disconnected/...)

Environment:
  DT_TOKEN         access token (built-in public token is used when unset)
  DT_SERVER        control endpoint
  DT_SUB           requested subdomain label
  DT_CONFIG        config file path (default ~/.dreamtunnel/config.env)
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
	if cfg.Token != "" {
		fmt.Println("token: <set, len", len(cfg.Token), ">")
	} else if DefaultPublicToken != "" {
		fmt.Println("token: <built-in public token>")
	} else {
		fmt.Println("token: <not set>")
	}
	fmt.Println("sub:", cfg.Sub)
	fmt.Println("session:", cfg.Sess)
	fmt.Println("config:", clientConfigPath())
	return nil
}
