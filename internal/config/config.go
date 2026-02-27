package config

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"

	"github.com/half0wl/railtail/internal/config/parser"
)

type ForwardTrafficType string

const (
	ForwardTrafficTypeTCP   ForwardTrafficType = "tcp"
	ForwardTrafficTypeHTTP  ForwardTrafficType = "http"
	ForwardTrafficTypeHTTPS ForwardTrafficType = "https"
)

var (
	ErrTargetAddrInvalid = errors.New("target-addr is invalid")
)

// Target represents a single listen-port to target-addr mapping.
type Target struct {
	ListenPort         string
	TargetAddr         string
	ForwardTrafficType ForwardTrafficType
}

type Config struct {
	TSHostname     string `flag:"ts-hostname" env:"TS_HOSTNAME" usage:"hostname to use for tailscale"`
	ListenPort     string `flag:"listen-port" env:"LISTEN_PORT" default:"" usage:"(legacy) port to listen on; use -target instead"`
	TargetAddr     string `flag:"target-addr" env:"TARGET_ADDR" default:"" usage:"(legacy) target address; use -target instead"`
	TSLoginServer  string `flag:"ts-login-server" env:"TS_LOGIN_SERVER" default:"" usage:"base url of the control server, If you are using Headscale for your control server, use your Headscale instance's URL"`
	TSStateDirPath string `flag:"ts-state-dir" env:"TS_STATEDIR_PATH" default:"/tmp/railtail" usage:"tailscale state dir"`
	TSAuthKey      string `env:"TS_AUTHKEY,TS_AUTH_KEY" usage:"tailscale auth key"`

	Targets []Target
}

func init() {
	flag.Bool("help", false, "Show help message")

	if checkForFlag("help") {
		cfg := &Config{}

		parser.ParseFlags(cfg)

		flag.Usage()
		os.Exit(0)
	}
}

// validateTargetAddr validates a target address and returns its ForwardTrafficType.
func validateTargetAddr(targetAddr string) (ForwardTrafficType, []error) {
	var errs []error
	protocol := strings.SplitN(targetAddr, "://", 2)[0]

	switch protocol {
	case "https", "http":
		ftt := ForwardTrafficType(protocol)

		u, err := url.Parse(targetAddr)
		if err != nil {
			errs = append(errs, fmt.Errorf("%w: %w", ErrTargetAddrInvalid, err))
		}

		if err == nil && u.Port() == "" {
			errs = append(errs, fmt.Errorf("%w: address %s: missing port in address", ErrTargetAddrInvalid, targetAddr))
		}

		return ftt, errs
	default:
		_, _, err := net.SplitHostPort(targetAddr)
		if err != nil {
			errs = append(errs, fmt.Errorf("%w: %w", ErrTargetAddrInvalid, err))
		}

		return ForwardTrafficTypeTCP, errs
	}
}

// parseTargetMapping parses a "listenPort=targetAddr" string into a Target.
func parseTargetMapping(mapping string) (Target, []error) {
	parts := strings.SplitN(mapping, "=", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return Target{}, []error{fmt.Errorf("invalid target format %q: expected listenPort=targetAddr", mapping)}
	}

	listenPort := parts[0]
	targetAddr := parts[1]

	ftt, errs := validateTargetAddr(targetAddr)
	if len(errs) > 0 {
		return Target{}, errs
	}

	return Target{
		ListenPort:         listenPort,
		TargetAddr:         targetAddr,
		ForwardTrafficType: ftt,
	}, nil
}

func LoadConfig() (*Config, []error) {
	cfg := &Config{}

	configErrs := parser.ParseConfig(cfg)
	if len(configErrs) > 0 {
		return nil, configErrs
	}

	// Collect target specs from new-style config
	var targetSpecs []string
	targetSpecs = append(targetSpecs, parser.TargetFlag...)
	if envTargets := parser.ParseTargetsEnv(); len(envTargets) > 0 {
		targetSpecs = append(targetSpecs, envTargets...)
	}

	hasNewConfig := len(targetSpecs) > 0
	hasLegacyConfig := cfg.ListenPort != "" || cfg.TargetAddr != ""

	if !hasNewConfig && !hasLegacyConfig {
		return nil, []error{fmt.Errorf("target configuration required: set TARGETS env or use -target flags, or set LISTEN_PORT + TARGET_ADDR")}
	}

	if hasNewConfig {
		var errs []error
		for _, spec := range targetSpecs {
			target, parseErrs := parseTargetMapping(spec)
			if len(parseErrs) > 0 {
				errs = append(errs, parseErrs...)
				continue
			}
			cfg.Targets = append(cfg.Targets, target)
		}
		if len(errs) > 0 {
			return nil, errs
		}
	} else if hasLegacyConfig {
		if cfg.ListenPort == "" {
			return nil, []error{fmt.Errorf("LISTEN_PORT is required when using legacy config: set LISTEN_PORT in env or use --listen-port")}
		}
		if cfg.TargetAddr == "" {
			return nil, []error{fmt.Errorf("TARGET_ADDR is required when using legacy config: set TARGET_ADDR in env or use --target-addr")}
		}

		ftt, validationErrs := validateTargetAddr(cfg.TargetAddr)
		if len(validationErrs) > 0 {
			return nil, validationErrs
		}

		cfg.Targets = []Target{{
			ListenPort:         cfg.ListenPort,
			TargetAddr:         cfg.TargetAddr,
			ForwardTrafficType: ftt,
		}}
	}

	return cfg, nil
}
