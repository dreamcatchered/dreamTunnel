package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"strings"
	"text/tabwriter"
	"time"
)

// ------------------------------------------------------------------ dt enroll

// runEnroll registers this device with the server and stores the token it
// receives, without starting a tunnel. Useful for a device that needs to be
// re-registered, or to see which device id this machine got.
func runEnroll(args []string) error {
	cfg := loadClientConfig()
	if server := argValue(args, "--server"); server != "" {
		cfg.Server = server
	}
	if cfg.Server == "" {
		cfg.Server = DefaultServer
	}
	if DefaultPublicToken == "" {
		return fmt.Errorf("this build carries no enrollment token; use `dt login <token>` instead")
	}

	host, _, err := net.SplitHostPort(cfg.Server)
	if err != nil {
		return fmt.Errorf("bad server %q: %w", cfg.Server, err)
	}
	conn, err := dialControlTLS(cfg.Server, host)
	if err != nil {
		return fmt.Errorf("dial %s: %w", cfg.Server, err)
	}
	defer conn.Close()

	hello := HelloMsg{
		Version:  1,
		Token:    DefaultPublicToken,
		Enroll:   true,
		Mode:     "enroll",
		Label:    deviceLabel(),
		Platform: platformTag(),
	}
	hb, _ := json.Marshal(hello)
	if err := WriteFrame(conn, PktHello, hb); err != nil {
		return err
	}

	conn.SetReadDeadline(time.Now().Add(20 * time.Second))
	typ, payload, err := ReadFrame(conn)
	if err != nil {
		return err
	}
	switch typ {
	case PktDenied:
		return fmt.Errorf("server denied: %s", string(payload))
	case PktWelcome:
	default:
		return fmt.Errorf("unexpected frame %d", typ)
	}
	var wel WelcomeMsg
	if err := json.Unmarshal(payload, &wel); err != nil {
		return err
	}
	if wel.Issued == "" {
		return fmt.Errorf("server did not issue a device token (enrollment may be disabled)")
	}

	cfg.Token = obfuscateForConfig(wel.Issued)
	ensureSession(cfg)
	if err := saveClientConfig(cfg); err != nil {
		return err
	}
	fmt.Printf("[dt] устройство зарегистрировано, id %s\n", wel.DeviceID)
	fmt.Println("[dt] токен сохранён в", clientConfigPath())
	return nil
}

// obfuscateForConfig stores a token the way `dt login` does, so a copied
// config keeps behaving identically.
func obfuscateForConfig(token string) string {
	if ob, err := tokenObfuscate(ObfuscationPass, token); err == nil {
		return ob
	}
	return token
}

// ---------------------------------------------------------------- dt devices

// deviceRow mirrors one entry of the admin panel's device list.
type deviceRow struct {
	ID       string    `json:"id"`
	Label    string    `json:"label"`
	Platform string    `json:"platform"`
	IP       string    `json:"ip"`
	First    time.Time `json:"first"`
	Last     time.Time `json:"last"`
	Revoked  bool      `json:"revoked"`
	Online   bool      `json:"online"`
}

// runDevices manages the per-device tokens through the admin panel API, so
// the running server remains the single source of truth for them.
func runDevices(args []string) error {
	sub := "list"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	if sub == "help" || sub == "--help" {
		fmt.Println("usage: dt devices [list] | revoke <id> [--ban-ip] | restore <id>")
		return nil
	}

	confPath := argValue(args, "--config")
	if confPath == "" {
		confPath = "/etc/dreamtunnel/server.conf"
	}
	cfg, err := loadServerConfig(confPath)
	if err != nil {
		return err
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return err
	}
	client := &http.Client{Jar: jar, Timeout: 15 * time.Second}
	base := "http://" + cfg.AdminAddr

	if err := adminLogin(client, base, cfg.AdminUser, cfg.AdminPass); err != nil {
		return err
	}

	switch sub {
	case "list":
		devices, err := adminListDevices(client, base)
		if err != nil {
			return err
		}
		printDevices(devices)
		return nil
	case "revoke", "restore":
		id := ""
		for _, a := range args {
			if !strings.HasPrefix(a, "-") {
				id = strings.TrimPrefix(a, deviceTokenPrefix)
				break
			}
		}
		if id == "" {
			return fmt.Errorf("usage: dt devices %s <id>", sub)
		}
		ban := argBool(args, "--ban-ip")
		if err := adminSetDevice(client, base, sub, id, ban); err != nil {
			return err
		}
		if sub == "revoke" {
			fmt.Printf("[dt] устройство %s отозвано\n", id)
			if ban {
				fmt.Println("[dt] его IP добавлен в блокировки")
			}
		} else {
			fmt.Printf("[dt] устройство %s снова активно\n", id)
		}
		return nil
	}
	return fmt.Errorf("unknown devices subcommand %q (list, revoke, restore)", sub)
}

func adminLogin(client *http.Client, base, user, pass string) error {
	body, _ := json.Marshal(map[string]string{"User": user, "Pass": pass})
	resp, err := client.Post(base+"/api/login", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("admin api %s: %w (is `dt server` running?)", base, err)
	}
	defer resp.Body.Close()
	var out struct{ OK, Error string }
	json.NewDecoder(resp.Body).Decode(&out)
	if out.Error != "" {
		return fmt.Errorf("admin login: %s", out.Error)
	}
	return nil
}

func adminListDevices(client *http.Client, base string) ([]deviceRow, error) {
	resp, err := client.Get(base + "/api/state")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var st struct {
		Devices []deviceRow `json:"devices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		return nil, err
	}
	return st.Devices, nil
}

func adminSetDevice(client *http.Client, base, action, id string, banIP bool) error {
	body, _ := json.Marshal(map[string]interface{}{"id": id, "banIP": banIP})
	resp, err := client.Post(base+"/api/device/"+action, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var out struct{ OK, Error string }
	json.NewDecoder(resp.Body).Decode(&out)
	if out.Error != "" {
		return fmt.Errorf("%s", out.Error)
	}
	return nil
}

func printDevices(devices []deviceRow) {
	if len(devices) == 0 {
		fmt.Println("устройств пока нет")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tИМЯ\tПЛАТФОРМА\tIP\tРЕГИСТРАЦИЯ\tПОСЛЕДНИЙ РАЗ\tСТАТУС")
	for _, d := range devices {
		status := "не в сети"
		switch {
		case d.Revoked:
			status = "отозвано"
		case d.Online:
			status = "на связи"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			d.ID, dashIfEmpty(d.Label), dashIfEmpty(d.Platform), dashIfEmpty(d.IP),
			d.First.Local().Format("02.01.06 15:04"), d.Last.Local().Format("02.01.06 15:04"), status)
	}
	w.Flush()
}

func dashIfEmpty(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

// argValue reads "--name value" or "--name=value" out of an argument list.
func argValue(args []string, name string) string {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(a, name+"=") {
			return strings.TrimPrefix(a, name+"=")
		}
	}
	return ""
}

// argBool reports whether a boolean flag is present (and not explicitly off).
func argBool(args []string, name string) bool {
	for _, a := range args {
		if a == name {
			return true
		}
		if strings.HasPrefix(a, name+"=") {
			v := strings.TrimPrefix(a, name+"=")
			return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
		}
	}
	return false
}
