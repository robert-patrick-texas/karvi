// Package transportselect resolves operator-facing transport selectors into a
// transport: the compiled-in native or Telnet transport, or the system
// transport's executable.
package transportselect

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

const (
	KindSystem = "system"
	KindNative = "native"
	KindTelnet = "telnet"
)

var aliasPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

// Selection is the fully resolved transport used for one activity or device.
// Selector is the operator-facing name (for example native or alternate1),
// while Kind is the transport recorded in output.
type Selection struct {
	Selector  string `json:"selector"`
	Kind      string `json:"kind"`
	Binary    string `json:"binary,omitempty"`
	ConfigKey string `json:"config_key,omitempty"`
}

// DefaultSelector is what a mode's "default" resolves to: system for a
// login, which attaches the operator's terminal to OpenSSH, and native for
// command and run.
func DefaultSelector(mode string) string {
	if mode == "login" {
		return "system"
	}
	return "native"
}

func modeKey(mode string) (string, error) {
	switch mode {
	case "login", "command", "run":
		return "ssh." + mode + ".transport", nil
	default:
		return "", fmt.Errorf("transport_mode_unknown: unknown activity mode %q", mode)
	}
}

// Resolve applies CLI/inventory override, mode configuration, and mode default
// in that order, then resolves the selected named slot.
func Resolve(cfg configload.Snapshot, mode, requested string) (Selection, error) {
	key, err := modeKey(mode)
	if err != nil {
		return Selection{}, err
	}
	selector := strings.ToLower(strings.TrimSpace(requested))
	if selector == "" {
		selector = strings.ToLower(strings.TrimSpace(cfg.String(key)))
	}
	if selector == "" || selector == "default" {
		selector = DefaultSelector(mode)
	}
	return resolveAlias(cfg, selector)
}

// resolveAlias resolves native and telnet to their compiled-in transports,
// and any other selector through its ssh.transports slot: system's
// executable, or a custom slot's exec:PATH.
func resolveAlias(cfg configload.Snapshot, selector string) (Selection, error) {
	selector = strings.ToLower(strings.TrimSpace(selector))
	switch selector {
	case KindNative:
		return Selection{Selector: selector, Kind: KindNative}, nil
	case KindTelnet:
		return Selection{Selector: selector, Kind: KindTelnet}, nil
	}
	if !aliasPattern.MatchString(selector) {
		return Selection{}, fmt.Errorf("transport_selector_invalid: invalid transport selector %q", selector)
	}

	key := "ssh.transports." + selector
	value := strings.TrimSpace(cfg.String(key))
	if value == "" {
		return Selection{}, fmt.Errorf("transport_mapping_missing: selector %q has no %s mapping", selector, key)
	}
	if selector == KindSystem {
		if value == "default" {
			value = "ssh"
		}
		binary, err := resolveExecutable(value)
		if err != nil {
			return Selection{}, executableError("transport_system_executable_unavailable", key, value, err)
		}
		return Selection{Selector: selector, Kind: KindSystem, Binary: binary, ConfigKey: key}, nil
	}
	if value == "default" {
		return Selection{}, fmt.Errorf("transport_custom_slot_default: %s may not use default, which only the system slot accepts", key)
	}
	spec, ok := strings.CutPrefix(value, "exec:")
	if !ok {
		return Selection{}, fmt.Errorf("transport_mapping_invalid: %s=%q is not exec:PATH", key, value)
	}
	binary, err := resolveExecutable(spec)
	if err != nil {
		return Selection{}, executableError("transport_exec_executable_unavailable", key, value, err)
	}
	return Selection{Selector: selector, Kind: KindSystem, Binary: binary, ConfigKey: key}, nil
}

// ValidateConfigured checks operator-authored transport declarations at config
// load time: a slot named for a built-in transport, and every explicit
// non-default mapping and mode selector, fail closed. A custom slot mapped to
// default fails when a mode selects it.
func ValidateConfigured(cfg configload.Snapshot) error {
	mappingKeys := make([]string, 0)
	for suffix := range cfg.Prefix("ssh.transports") {
		mappingKeys = append(mappingKeys, "ssh.transports."+suffix)
	}
	sort.Strings(mappingKeys)
	for _, key := range mappingKeys {
		v, ok := cfg.Provenance(key)
		if !ok || v.Source.Layer == "builtin" {
			continue
		}
		selector := strings.TrimPrefix(key, "ssh.transports.")
		if selector == KindNative || selector == KindTelnet {
			return fmt.Errorf("transport_mapping_invalid: %s: %s is a built-in transport, not a slot", key, selector)
		}
		value, _ := v.Data.(string)
		if strings.EqualFold(strings.TrimSpace(value), "default") {
			continue
		}
		if _, err := resolveAlias(cfg, selector); err != nil {
			return err
		}
	}
	for _, mode := range []string{"login", "command", "run"} {
		key, _ := modeKey(mode)
		v, ok := cfg.Provenance(key)
		if !ok || v.Source.Layer == "builtin" {
			continue
		}
		selector, _ := v.Data.(string)
		if strings.EqualFold(strings.TrimSpace(selector), "default") || strings.TrimSpace(selector) == "" {
			continue
		}
		if _, err := Resolve(cfg, mode, selector); err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
	}
	return nil
}

func resolveExecutable(spec string) (string, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", errorcodes.Errorf("transport_executable_spec_empty", "empty executable specification")
	}
	if strings.ContainsRune(spec, os.PathSeparator) {
		path := spec
		if !filepath.IsAbs(path) {
			executable, err := os.Executable()
			if err != nil {
				return "", errorcodes.Errorf("karvi_executable_unlocatable", "locate karvi executable: %w", err)
			}
			if resolved, err := filepath.EvalSymlinks(executable); err == nil {
				executable = resolved
			}
			path = filepath.Join(filepath.Dir(executable), path)
		}
		path = filepath.Clean(path)
		if err := checkExecutable(path); err != nil {
			return "", err
		}
		return path, nil
	}
	path, err := exec.LookPath(spec)
	if err != nil {
		return "", err
	}
	if err := checkExecutable(path); err != nil {
		return "", err
	}
	return path, nil
}

func checkExecutable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errorcodes.Errorf("transport_executable_not_regular", "%s is not a regular file", path)
	}
	if info.Mode().Perm()&0111 == 0 {
		return errorcodes.Errorf("transport_executable_not_executable", "%s is not executable", path)
	}
	return nil
}

// executableError keeps the specific code of an unusable executable and uses
// fallback when the executable cannot be found.
func executableError(fallback, key, value string, err error) error {
	code := errorcodes.Of(err)
	if code == "" {
		code = fallback
	}
	return errorcodes.Errorf(code, "%s=%q is unavailable: %w", key, value, err)
}
