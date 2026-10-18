package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/xtls/libxray/share"
	"github.com/xtls/xray-core/common/cmdarg"
	"github.com/xtls/xray-core/common/errors"
	clog "github.com/xtls/xray-core/common/log"
	"github.com/xtls/xray-core/common/platform"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf/serial"
	"github.com/xtls/xray-core/main/commands/base"
)

var cmdRun = &base.Command{
	UsageLine: "{{.Exec}} run [-c config.json] [-confdir dir] [-k link] [-m mode] [-listen addr]",
	Short:     "Run Xray with config, the default command",
	Long: `
Run Xray with config, the default command.

The -config=file, -c=file flags set the config files for
Xray. Multiple assign is accepted.

The -confdir=dir flag sets a dir with multiple json config

The -key=link, -k=link flags set share links (vless://, vmess://, trojan://,
shadowsocks://, socks://, hysteria://) or a subscription instead of config
files. The config is generated in memory, no config file is written.
Multiple assign is accepted.

The -keysecret=secret flag sets the Age identity to decrypt an encrypted
share link or subscription. Only needed with -key.

The -mode=mode, -m=mode flags add a local inbound to the generated config:
  tun    TUN device, routes the whole system through the share link
  socks  SOCKS5 proxy on 127.0.0.1:10808
  http   HTTP proxy on 127.0.0.1:10809
Multiple assign is accepted, e.g. "run -k link -m tun -m socks".
Without -mode only the outbounds are generated, with no inbound.
Only works with -key.

The -listen=addr flag sets the listen address for the socks and http
modes. Default "127.0.0.1". Use "0.0.0.0" to accept connections from
other hosts, e.g. when running inside a container.

The following environment variables set defaults for the run command.
Flags always take precedence over environment variables.

  XRAY_CONFIG_PATH   Same as -config, multiple paths separated by newline
  XRAY_KEY           Same as -key, multiple share links separated by newline
  XRAY_ROUTE_MODE    Same as -mode, multiple modes separated by newline or comma
  XRAY_LISTEN_ADDR   Same as -listen

The -format=json flag sets the format of config files.
Default "auto".

The config root env object sets process environment variables after all config
files are parsed. Variables needed to locate or parse config files must be set
in the process environment before Xray starts.

The -test flag tells Xray to test config files only,
without launching the server.

The -dump flag tells Xray to print the merged config.
	`,
}

func init() {
	cmdRun.Run = executeRun // break init loop
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
}

var (
	configFiles cmdarg.Arg // "Config file for Xray.", the option is customed type, parse in main
	configDir   string
	keyLinks    cmdarg.Arg // "Share link or subscription for Xray.", the option is customed type, parse in main
	keySecret   string
	keyModes    cmdarg.Arg // "Local inbound for Xray.", the option is customed type, parse in main
	keyListen   = cmdRun.Flag.String("listen", keyLocalListen, "Listen address for the socks and http modes.")
	dump        = cmdRun.Flag.Bool("dump", false, "Dump merged config only, without launching Xray server.")
	test        = cmdRun.Flag.Bool("test", false, "Test config file only, without launching Xray server.")
	format      = cmdRun.Flag.String("format", "auto", "Format of input file.")

	/* We have to do this here because Golang's Test will also need to parse flag, before
	 * main func in this file is run.
	 */
	_ = func() bool {
		cmdRun.Flag.Var(&configFiles, "config", "Config path for Xray.")
		cmdRun.Flag.Var(&configFiles, "c", "Short alias of -config")
		cmdRun.Flag.StringVar(&configDir, "confdir", "", "A dir with multiple json config")
		cmdRun.Flag.Var(&keyLinks, "key", "Share link for Xray.")
		cmdRun.Flag.Var(&keyLinks, "k", "Short alias of -key")
		cmdRun.Flag.StringVar(&keySecret, "keysecret", "", "Age identity to decrypt an encrypted share link.")
		cmdRun.Flag.Var(&keyModes, "mode", "Local inbound of the generated config: tun, socks, http.")
		cmdRun.Flag.Var(&keyModes, "m", "Short alias of -mode")

		return true
	}()
)

// applyEnvDefaults fills the unset run flags from the XRAY_* environment
// variables. Flags passed on the command line always take precedence.
// The -listen flag can't be detected by value (it has a default), so the
// parsed flag set is consulted instead.
func applyEnvDefaults() {
	fromCommandLine := make(map[string]bool)
	cmdRun.Flag.Visit(func(f *flag.Flag) {
		fromCommandLine[f.Name] = true
	})
	listenSet := fromCommandLine["listen"]

	if !listenSet {
		if v := strings.TrimSpace(os.Getenv("XRAY_LISTEN_ADDR")); v != "" {
			*keyListen = v
		}
	}
	if !fromCommandLine["config"] && !fromCommandLine["c"] {
		for _, p := range splitNonEmpty(os.Getenv("XRAY_CONFIG_PATH"), "\n") {
			configFiles.Set(p)
		}
	}
	if !fromCommandLine["key"] && !fromCommandLine["k"] {
		for _, l := range splitNonEmpty(os.Getenv("XRAY_KEY"), "\n") {
			keyLinks.Set(l)
		}
	}
	if !fromCommandLine["mode"] && !fromCommandLine["m"] {
		for _, m := range splitNonEmpty(os.Getenv("XRAY_ROUTE_MODE"), "\n", ",") {
			keyModes.Set(m)
		}
	}
}

// splitNonEmpty splits s by the given separators and returns the
// trimmed non-empty parts, or nil if there are none.
func splitNonEmpty(s string, seps ...string) []string {
	parts := []string{s}
	for _, sep := range seps {
		var next []string
		for _, p := range parts {
			next = append(next, strings.Split(p, sep)...)
		}
		parts = next
	}
	var out []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func executeRun(cmd *base.Command, args []string) {
	applyEnvDefaults()

	if *dump {
		clog.ReplaceWithSeverityLogger(clog.Severity_Warning)
		errCode := dumpConfig()
		os.Exit(errCode)
	}

	printVersion()
	server, err := startXray()
	if err != nil {
		fmt.Println("Failed to start:", err)
		// Configuration error. Exit with a special value to prevent systemd from restarting.
		os.Exit(23)
	}

	if *test {
		fmt.Println("Configuration OK.")
		os.Exit(0)
	}

	if err := server.Start(); err != nil {
		fmt.Println("Failed to start:", err)
		os.Exit(-1)
	}
	defer server.Close()

	// Explicitly triggering GC to remove garbage from config loading.
	runtime.GC()
	debug.FreeOSMemory()

	{
		osSignals := make(chan os.Signal, 1)
		signal.Notify(osSignals, os.Interrupt, syscall.SIGTERM)
		<-osSignals
	}
}

func fileExists(file string) bool {
	info, err := os.Stat(file)
	return err == nil && !info.IsDir()
}

func dirExists(file string) bool {
	if file == "" {
		return false
	}
	info, err := os.Stat(file)
	return err == nil && info.IsDir()
}

func getRegepxByFormat() string {
	switch strings.ToLower(*format) {
	case "json":
		return `^.+\.(json|jsonc)$`
	case "toml":
		return `^.+\.toml$`
	case "yaml", "yml":
		return `^.+\.(yaml|yml)$`
	default:
		return `^.+\.(json|jsonc|toml|yaml|yml)$`
	}
}

func readConfDir(dirPath string) {
	confs, err := os.ReadDir(dirPath)
	if err != nil {
		log.Fatalln(err)
	}
	for _, f := range confs {
		matched, err := regexp.MatchString(getRegepxByFormat(), f.Name())
		if err != nil {
			log.Fatalln(err)
		}
		if matched {
			configFiles.Set(path.Join(dirPath, f.Name()))
		}
	}
}

func getConfigFilePath(verbose bool) cmdarg.Arg {
	if dirExists(configDir) {
		if verbose {
			log.Println("Using confdir from arg:", configDir)
		}
		readConfDir(configDir)
	} else if envConfDir := platform.GetConfDirPath(); dirExists(envConfDir) {
		if verbose {
			log.Println("Using confdir from env:", envConfDir)
		}
		readConfDir(envConfDir)
	}

	if len(configFiles) > 0 {
		return configFiles
	}

	if workingDir, err := os.Getwd(); err == nil {
		suffixes := []string{".json", ".jsonc", ".toml", ".yaml", ".yml"}
		for _, suffix := range suffixes {
			configFile := filepath.Join(workingDir, "config"+suffix)
			if fileExists(configFile) {
				if verbose {
					log.Println("Using default config: ", configFile)
				}
				return cmdarg.Arg{configFile}
			}
		}
	}

	if configFile := platform.GetConfigurationPath(); fileExists(configFile) {
		if verbose {
			log.Println("Using config from env: ", configFile)
		}
		return cmdarg.Arg{configFile}
	}

	if verbose {
		log.Println("Using config from STDIN")
	}
	return cmdarg.Arg{"stdin:"}
}

func getConfigFormat() string {
	f := core.GetFormatByExtension(*format)
	if f == "" {
		f = "auto"
	}
	return f
}

const (
	keyLocalListen = "127.0.0.1"
	keySocksPort   = 10808
	keyHTTPPort    = 10809
)

// tunInbound returns a TUN inbound routing the whole system through the
// outbounds of the generated config.
func tunInbound() map[string]any {
	settings := map[string]any{
		"mtu":                    1500,
		"autoSystemRoutingTable": []string{"0.0.0.0/0", "::/0"},
	}
	switch runtime.GOOS {
	case "windows":
		settings["dns"] = []string{"1.1.1.1", "8.8.8.8"}
		settings["autoSystemWfpBlockLeak"] = []string{"dns", "misconfigtun"}
	case "linux":
		settings["gateway"] = []string{"10.0.0.1/24"}
		settings["autoSystemDnsToGateway"] = true
	}
	return map[string]any{
		"tag":      "tun",
		"protocol": "tun",
		"settings": settings,
	}
}

func socksInbound() map[string]any {
	settings := map[string]any{
		"auth": "noauth",
		"udp":  true,
	}
	if *keyListen != "" && *keyListen != "0.0.0.0" && *keyListen != "::" {
		settings["ip"] = *keyListen
	}
	return map[string]any{
		"tag":      "socks",
		"listen":   *keyListen,
		"port":     keySocksPort,
		"protocol": "socks",
		"settings": settings,
	}
}

func httpInbound() map[string]any {
	return map[string]any{
		"tag":      "http",
		"listen":   *keyListen,
		"port":     keyHTTPPort,
		"protocol": "http",
		"settings": map[string]any{},
	}
}

// getKeyInbounds turns the modes into inbound objects.
func getKeyInbounds(modes cmdarg.Arg) ([]map[string]any, error) {
	var inbounds []map[string]any
	for _, mode := range modes {
		switch strings.ToLower(mode) {
		case "tun":
			inbounds = append(inbounds, tunInbound())
		case "socks", "socks5":
			inbounds = append(inbounds, socksInbound())
		case "http", "https":
			inbounds = append(inbounds, httpInbound())
		default:
			return nil, errors.New("unknown mode: ", mode, ", expected tun, socks or http")
		}
	}
	return inbounds, nil
}

// getKeyConfigJSON converts the share links into an Xray config, kept in memory,
// with the inbounds of the modes added.
func getKeyConfigJSON() (json.RawMessage, error) {
	raw, err := share.ConvertShareLinksToXrayJson(strings.Join(keyLinks, "\n"), keySecret)
	if err != nil {
		return nil, errors.New("failed to convert share link").Base(err)
	}

	inbounds, err := getKeyInbounds(keyModes)
	if err != nil {
		return nil, err
	}
	if len(inbounds) == 0 {
		return raw, nil
	}

	document := make(map[string]json.RawMessage)
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, errors.New("failed to parse generated config").Base(err)
	}
	var existing []json.RawMessage
	if len(document["inbounds"]) > 0 {
		if err := json.Unmarshal(document["inbounds"], &existing); err != nil {
			return nil, errors.New("failed to parse generated inbounds").Base(err)
		}
	}
	for _, inbound := range inbounds {
		encoded, err := json.Marshal(inbound)
		if err != nil {
			return nil, errors.New("failed to generate inbound").Base(err)
		}
		existing = append(existing, encoded)
	}
	encodedInbounds, err := json.Marshal(existing)
	if err != nil {
		return nil, errors.New("failed to generate inbounds").Base(err)
	}
	document["inbounds"] = encodedInbounds

	merged, err := json.Marshal(document)
	if err != nil {
		return nil, errors.New("failed to generate config").Base(err)
	}
	return merged, nil
}

// getConfigFromKeys converts share links into an Xray config kept in memory.
func getConfigFromKeys() (*core.Config, error) {
	raw, err := getKeyConfigJSON()
	if err != nil {
		return nil, err
	}
	c, err := serial.DecodeJSONConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, errors.New("failed to parse generated config").Base(err)
	}
	return c.Build()
}

func dumpConfig() int {
	if len(keyLinks) > 0 {
		raw, err := getKeyConfigJSON()
		if err != nil {
			fmt.Println(err)
			time.Sleep(1 * time.Second)
			return 23
		}
		fmt.Println(string(raw))
		return 0
	}

	files := getConfigFilePath(false)
	if config, err := core.GetMergedConfig(files); err != nil {
		fmt.Println(err)
		time.Sleep(1 * time.Second)
		return 23
	} else {
		fmt.Print(config)
	}
	return 0
}

func startXray() (core.Server, error) {
	var c *core.Config
	var err error

	if len(keyLinks) > 0 {
		c, err = getConfigFromKeys()
	} else if len(keyModes) > 0 {
		return nil, errors.New("no share link to route, modes need the -key flag")
	} else {
		configFiles := getConfigFilePath(true)
		c, err = core.LoadConfig(getConfigFormat(), configFiles)
		if err != nil {
			err = errors.New("failed to load config files: [", configFiles.String(), "]").Base(err)
		}
	}
	if err != nil {
		return nil, err
	}

	server, err := core.New(c)
	if err != nil {
		return nil, errors.New("failed to create server").Base(err)
	}

	return server, nil
}
