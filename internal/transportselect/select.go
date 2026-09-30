// Package transportselect resolves operator-facing transport selectors into a
// concrete executable or compiled-in SSH implementation. Keeping this policy
// outside adapters prevents CLI, inventory, and dispatch code from depending
// on ScrapliGo or any future SSH library.
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
	"github.com/robert-patrick-texas/karvi/internal/transport/native"
)

const (
	KindSystem = "system"
	KindNative = "native"
	KindTelnet = "telnet"
)

var aliasPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

// Selection is the fully resolved transport used for one activity or device.
// Selector is the operator-facing slot (for example native or alternate1),
// while Implementation is the stable adapter identifier recorded in output.
type Selection struct {
	Selector       string `json:"selector"`
	Kind           string `json:"kind"`
	Implementation string `json:"implementation"`
	Binary         string `json:"binary,omitempty"`
	ConfigKey      string `json:"config_key,omitempty"`
}

func DefaultSelector(mode string) string {
	switch mode {
	case "login", "command":
		return "system"
	case "run":
		return "native"
	default:
		return "native"
	}
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
	if selector == "preferred" {
		selector = "native"
	}
	return resolveAlias(cfg, selector)
}

func resolveAlias(cfg configload.Snapshot, selector string) (Selection, error) {
	selector = strings.ToLower(strings.TrimSpace(selector))
	if selector == "telnet" {
		return Selection{Selector: selector, Kind: KindTelnet, Implementation: "telnet"}, nil
	}
	if !aliasPattern.MatchString(selector) {
		return Selection{}, fmt.Errorf("transport_selector_invalid: invalid transport selector %q", selector)
	}

	// A compiled-in implementation identifier may be selected directly. Named
	// slots remain preferred because they let governance remap native later.
	if native.Available(selector) {
		return Selection{Selector: selector, Kind: KindNative, Implementation: selector}, nil
	}

	key := "ssh.transports." + selector
	value := strings.TrimSpace(cfg.String(key))
	if value == "" {
		return Selection{}, fmt.Errorf("transport_mapping_missing: selector %q has no %s mapping", selector, key)
	}
	if value == "default" {
		switch selector {
		case "system":
			value = "ssh"
		case "native":
			value = "scrapligo-v1"
		default:
			return Selection{}, fmt.Errorf("transport_custom_slot_default: %s may not use default because custom slots have no implicit implementation", key)
		}
	}

	if selector == "system" {
		binary, err := resolveExecutable(value)
		if err != nil {
			return Selection{}, executableError("transport_system_executable_unavailable", key, value, err)
		}
		return Selection{Selector: selector, Kind: KindSystem, Implementation: "system", Binary: binary, ConfigKey: key}, nil
	}
	if strings.HasPrefix(value, "exec:") {
		spec := strings.TrimPrefix(value, "exec:")
		binary, err := resolveExecutable(spec)
		if err != nil {
			return Selection{}, executableError("transport_exec_executable_unavailable", key, value, err)
		}
		return Selection{Selector: selector, Kind: KindSystem, Implementation: "system", Binary: binary, ConfigKey: key}, nil
	}
	implementation := strings.TrimPrefix(value, "builtin:")
	if !aliasPattern.MatchString(implementation) {
		return Selection{}, fmt.Errorf("transport_implementation_invalid: %s has invalid implementation %q", key, implementation)
	}
	if !native.Available(implementation) {
		return Selection{}, fmt.Errorf("native_transport_unavailable: %s selects %q, which is not compiled into this karvi executable", key, implementation)
	}
	return Selection{Selector: selector, Kind: KindNative, Implementation: implementation, ConfigKey: key}, nil
}

// ValidateConfigured checks operator-authored transport declarations at config
// load time. Built-in defaults are intentionally lazy: an executable stays
// usable through system SSH even if its default native preference has no
// registered provider. Explicit non-default declarations fail closed.
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
		value, _ := v.Data.(string)
		if strings.EqualFold(strings.TrimSpace(value), "default") {
			continue
		}
		selector := strings.TrimPrefix(key, "ssh.transports.")
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

// NativeStatus describes the effective native preference for contextual help
// without turning an unavailable built-in default into a configuration error.
func NativeStatus(cfg configload.Snapshot) (implementation string, available bool) {
	value := strings.TrimSpace(cfg.String("ssh.transports.native"))
	if value == "" || value == "default" {
		value = "scrapligo-v1"
	}
	value = strings.TrimPrefix(value, "builtin:")
	return value, native.Available(value)
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
