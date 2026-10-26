package planner

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/output"
	"github.com/robert-patrick-texas/karvi/internal/resolver"
)

// TargetCommands builds one target's command list for a run with a --cf
// folder on its line, given the target's name in the folder (its collection
// file's, output.FileName): the line's commands in the order written, the
// folder's file for that name in its place, each command with its
// declarations (EXAMPLES chapter 79). A folder that holds no file for the
// name gives *MissingCommandsFile.
type TargetCommands func(name string) (executionplan.CommandList, error)

// MissingCommandsFile is a --cf folder that holds no file for a target.
type MissingCommandsFile struct {
	Folder, Name string
}

func (m *MissingCommandsFile) Error() string {
	return fmt.Sprintf("commands_file_missing: %s does not exist", filepath.Join(m.Folder, m.Name))
}

// targetCommandLists builds every target's list once the targets are
// known, by target ID. Two targets with one name in the folders are refused
// first (commands_file_name_collision), since both would read one file;
// then every target's list is built, the targets whose file a folder lacks
// gathered into one refusal (commands_file_missing), and any other failure
// returned at once, naming its target. Each list's own bounds are checked
// as the plan's commands' are. folders are the --cf folders as given, for
// the messages.
func targetCommandLists(cfg configload.Snapshot, targets []executionplan.ExecutionTarget, crop bool, folders []string, build TargetCommands) (map[string]executionplan.CommandList, error) {
	names := make([]string, len(targets))
	byName := map[string][]string{}
	for i, t := range targets {
		names[i] = output.FileName(t.Device.CanonicalName, crop)
		byName[names[i]] = append(byName[names[i]], t.Device.CanonicalName)
	}
	for i := range targets {
		if devices := byName[names[i]]; len(devices) > 1 {
			return nil, errorcodes.Errorf("commands_file_name_collision", "%s is the file of %d devices\n  %s", filepath.Join(folders[0], names[i]), len(devices), strings.Join(devices, "\n  "))
		}
	}
	lists := make(map[string]executionplan.CommandList, len(targets))
	var lacking []string               // the folders lacking a file, in order
	missingIn := map[string][]string{} // each one's "device (name)" lines
	for i, t := range targets {
		list, err := build(names[i])
		var m *MissingCommandsFile
		if errors.As(err, &m) {
			if _, seen := missingIn[m.Folder]; !seen {
				lacking = append(lacking, m.Folder)
			}
			missingIn[m.Folder] = append(missingIn[m.Folder], fmt.Sprintf("%s (%s)", t.Device.CanonicalName, m.Name))
			continue
		}
		if code := errorcodes.Of(err); code != "" {
			return nil, errorcodes.Errorf(code, "%s: %s", t.Device.CanonicalName, strings.TrimPrefix(err.Error(), code+": "))
		} else if err != nil {
			return nil, err
		}
		if err := checkCommandBounds(cfg, list.TimeoutsNS, list.MaxBytes); err != nil {
			return nil, err
		}
		lists[t.TargetID] = list
	}
	if len(lacking) > 0 {
		return nil, missingFiles(lacking, missingIn)
	}
	return lists, nil
}

// missingFiles is the refusal of the targets a folder holds no file for:
// each folder in order with its devices stacked, each device with the
// file it lacks, resolver.ListedFailures devices in all and then the count
// left.
func missingFiles(folders []string, devices map[string][]string) error {
	var b strings.Builder
	listed, total := 0, 0
	for _, folder := range folders {
		total += len(devices[folder])
	}
	for i, folder := range folders {
		if listed == resolver.ListedFailures {
			break
		}
		if i > 0 {
			b.WriteString("\n")
		}
		n := len(devices[folder])
		noun := "devices"
		if n == 1 {
			noun = "device"
		}
		fmt.Fprintf(&b, "%s has no file for %d %s", folder, n, noun)
		for _, d := range devices[folder] {
			if listed == resolver.ListedFailures {
				break
			}
			fmt.Fprintf(&b, "\n  %s", d)
			listed++
		}
	}
	if total > listed {
		fmt.Fprintf(&b, "\n  and %d more", total-listed)
	}
	return errorcodes.Errorf("commands_file_missing", "%s", b.String())
}
