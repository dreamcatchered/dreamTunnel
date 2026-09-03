package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

// serverConfig loaded from /etc/dreamtunnel/server.conf (KEY=VALUE).
type serverConfig struct {
	Secret        string
	ControlAddr   string
	EdgeAddr      string
	StaticTLS     string
	Domain        string
	FilterEnabled bool
	IdleTimeout   time.Duration
	CertDir       string
	AdminAddr     string // admin panel listen, default 127.0.0.1:8484
	AdminUser     string
	AdminPass     string
}

func loadServerConfig(path string) (*serverConfig, error) {
	cfg := &serverConfig{
		ControlAddr: ":444",
		EdgeAddr:    ":8443",
		StaticTLS:   "127.0.0.1:443",
		Domain:      "maiuni.online",
		IdleTimeout: 10 * time.Minute,
		CertDir:     "/etc/dreamtunnel/certs/wildcard",
		AdminAddr:   "127.0.0.1:8484",
		AdminUser:   "admin",
	}
	env := dotEnv(path)
	if v, ok := env["DT_SECRET"]; ok && v != "" {
		cfg.Secret = v
	}
	if v, ok := env["DT_CONTROL_ADDR"]; ok && v != "" {
		cfg.ControlAddr = v
	}
	if v, ok := env["DT_EDGE_ADDR"]; ok && v != "" {
		cfg.EdgeAddr = v
	}
	if v, ok := env["DT_STATIC_TLS"]; ok && v != "" {
		cfg.StaticTLS = v
	}
	if v, ok := env["DT_DOMAIN"]; ok && v != "" {
		cfg.Domain = v
	}
	if v, ok := env["DT_FILTER_ENABLED"]; ok {
		cfg.FilterEnabled = v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
	}
	if v, ok := env["DT_IDLE_TIMEOUT"]; ok && v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.IdleTimeout = d
		}
	}
	if v, ok := env["DT_CERT_DIR"]; ok && v != "" {
		cfg.CertDir = v
	}
	if v, ok := env["DT_ADMIN_ADDR"]; ok && v != "" {
		cfg.AdminAddr = v
	}
	if v, ok := env["DT_ADMIN_USER"]; ok && v != "" {
		cfg.AdminUser = v
	}
	if v, ok := env["DT_ADMIN_PASS"]; ok && v != "" {
		cfg.AdminPass = v
	}
	if cfg.Secret == "" {
		return nil, fmt.Errorf("DT_SECRET is required in %s", path)
	}
	if cfg.AdminPass == "" {
		return nil, fmt.Errorf("DT_ADMIN_PASS is required in %s (admin panel)", path)
	}
	return cfg, nil
}

// runServer is the entrypoint of `dt server`.
func runServer(args []string) error {
	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	confPath := fs.String("config", "/etc/dreamtunnel/server.conf", "config file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := loadServerConfig(*confPath)
	if err != nil {
		return err
	}
	TunnelDomain = cfg.Domain

	wildTLS, err = tlsLoadWildcard(cfg.CertDir)
	if err != nil {
		log.Printf("[dt] warn: wildcard cert not loaded (%v); offline answers disabled", err)
		wildTLS = nil
	}

	filter := NewContentFilter(cfg.FilterEnabled)
	srv := NewTunnelServer(cfg.Secret, filter, cfg.IdleTimeout)

	cl, err := net.Listen("tcp", cfg.ControlAddr)
	if err != nil {
		return fmt.Errorf("control listen %s: %w", cfg.ControlAddr, err)
	}
	go func() {
		for {
			conn, err := cl.Accept()
			if err != nil {
				return
			}
			go srv.ServeControlTLSConn(conn)
		}
	}()
	log.Printf("[dt] control listening on %s (domain %s, filter=%v)", cfg.ControlAddr, cfg.Domain, cfg.FilterEnabled)

	edge := &EdgeRouter{srv: srv, staticTLS: cfg.StaticTLS}
	el, err := net.Listen("tcp", cfg.EdgeAddr)
	if err != nil {
		return fmt.Errorf("edge listen %s: %w", cfg.EdgeAddr, err)
	}
	go edge.Serve(el)
	log.Printf("[dt] edge listening on %s", cfg.EdgeAddr)

	// admin panel (loopback only; front with nginx TLS for remote access)
	admin := newAdminServer(srv, cfg.AdminUser, cfg.AdminPass)
	go http.ListenAndServe(cfg.AdminAddr, admin.Handler())
	log.Printf("[dt] admin panel on %s (login %s)", cfg.AdminAddr, cfg.AdminUser)

	select {}
}

