// Command schema-check validates karvi command streams and final job artifacts.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/robert-patrick-texas/karvi/records"
)

func main() {
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: schema-check <job-directory>")
		os.Exit(2)
	}
	if err := check(flag.Arg(0)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("valid: true")
}

// check validates the job directory: every command
// record, its sequence, the summary at schema 2, and the manifest at schema
// 2 against its own plan, header, and package projection; every command
// record's device is a plan target.
func check(dir string) error {
	var manifest records.Manifest
	if err := readJSON(filepath.Join(dir, "manifest.json"), &manifest); err != nil {
		return err
	}
	if err := manifest.Validate(); err != nil {
		return fmt.Errorf("manifest.json: %w", err)
	}
	stream, err := os.Open(filepath.Join(dir, "commands.jsonl"))
	if err != nil {
		return err
	}
	defer stream.Close()
	s := bufio.NewScanner(stream)
	s.Buffer(make([]byte, 64*1024), 128<<20)
	sequence := int64(0)
	for s.Scan() {
		var record records.CommandRecord
		if err := json.Unmarshal(s.Bytes(), &record); err != nil {
			return fmt.Errorf("commands.jsonl line %d: %w", sequence+1, err)
		}
		if err := record.Validate(); err != nil {
			return fmt.Errorf("commands.jsonl line %d: %w", sequence+1, err)
		}
		sequence++
		if record.Sequence != sequence {
			return fmt.Errorf("commands.jsonl sequence %d, expected %d", record.Sequence, sequence)
		}
		if !manifest.IsPlanTarget(record.Device.ID) {
			return fmt.Errorf("commands.jsonl line %d: device %q is not a plan target", sequence, record.Device.ID)
		}
	}
	if err := s.Err(); err != nil {
		return err
	}
	var summary records.Summary
	if err := readJSON(filepath.Join(dir, "summary.json"), &summary); err != nil {
		return err
	}
	if summary.SchemaVersion != records.JobSchemaVersion || summary.ActivityID == "" || summary.ExitName == "" || summary.PlanID == "" || summary.PlanDigest == "" {
		return fmt.Errorf("summary.json is incomplete")
	}
	if manifest.ActivityID != summary.ActivityID || summary.PlanDigest != manifest.Plan.PlanDigest.String() {
		return fmt.Errorf("manifest/summary identity mismatch")
	}
	return nil
}

func readJSON(path string, dst any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}
