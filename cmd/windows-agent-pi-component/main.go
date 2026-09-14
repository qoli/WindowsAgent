package main

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"strings"
)

type componentConfig struct {
	component      string
	nodePath       string
	runtimeDir     string
	agentDir       string
	sessionDir     string
	piWebDataDir   string
	piWebConfig    string
	helperPath     string
	sessiondListen string
	webListen      string
	logFile        string
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "windows-agent-pi-component:", err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	cfg, err := parseComponentConfig(arguments)
	if err != nil {
		return err
	}
	return runOwnedComponent(cfg)
}

func parseComponentConfig(arguments []string) (componentConfig, error) {
	flags := flag.NewFlagSet("windows-agent-pi-component", flag.ContinueOnError)
	var cfg componentConfig
	flags.StringVar(&cfg.component, "component", "", "PI WEB component: sessiond or web")
	flags.StringVar(&cfg.nodePath, "node", "", "absolute node.exe path")
	flags.StringVar(&cfg.runtimeDir, "runtime-dir", "", "absolute pinned runtime directory")
	flags.StringVar(&cfg.agentDir, "agent-dir", "", "absolute dedicated Pi profile directory")
	flags.StringVar(&cfg.sessionDir, "session-dir", "", "absolute Pi session directory")
	flags.StringVar(&cfg.piWebDataDir, "pi-web-data-dir", "", "absolute PI WEB data directory")
	flags.StringVar(&cfg.piWebConfig, "pi-web-config", "", "absolute PI WEB config path")
	flags.StringVar(&cfg.helperPath, "computer-use-helper", "", "absolute pi-computer-use helper path")
	flags.StringVar(&cfg.sessiondListen, "sessiond-listen", "127.0.0.1:8503", "session daemon loopback listener")
	flags.StringVar(&cfg.webListen, "web-listen", "127.0.0.1:8504", "PI WEB loopback listener")
	flags.StringVar(&cfg.logFile, "log-file", "", "absolute component log path")
	if err := flags.Parse(arguments); err != nil {
		return componentConfig{}, fmt.Errorf("parse flags: %w", err)
	}
	if flags.NArg() != 0 || (cfg.component != "sessiond" && cfg.component != "web") {
		return componentConfig{}, errors.New("component must be sessiond or web and positional arguments are forbidden")
	}
	for name, value := range map[string]string{
		"--node": cfg.nodePath, "--runtime-dir": cfg.runtimeDir, "--agent-dir": cfg.agentDir,
		"--session-dir": cfg.sessionDir, "--pi-web-data-dir": cfg.piWebDataDir,
		"--pi-web-config": cfg.piWebConfig, "--computer-use-helper": cfg.helperPath, "--log-file": cfg.logFile,
	} {
		if value == "" || !absoluteWindowsPath(value) {
			return componentConfig{}, fmt.Errorf("%s must be an absolute path", name)
		}
	}
	sessiondPort, err := loopbackPort(cfg.sessiondListen)
	if err != nil {
		return componentConfig{}, fmt.Errorf("invalid --sessiond-listen: %w", err)
	}
	webPort, err := loopbackPort(cfg.webListen)
	if err != nil {
		return componentConfig{}, fmt.Errorf("invalid --web-listen: %w", err)
	}
	if sessiondPort == webPort {
		return componentConfig{}, errors.New("session daemon and Web ports must differ")
	}
	return cfg, nil
}

func absoluteWindowsPath(value string) bool {
	if len(value) >= 3 && ((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) && value[1] == ':' && (value[2] == '\\' || value[2] == '/') {
		return true
	}
	return strings.HasPrefix(value, `\\`)
}

func loopbackPort(value string) (string, error) {
	host, port, err := net.SplitHostPort(value)
	if err != nil || port == "" {
		return "", errors.New("listener must contain a host and port")
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() || strings.Contains(port, "+") || strings.Contains(port, "-") {
		return "", errors.New("listener must use an explicit loopback IP")
	}
	return port, nil
}
