// Copyright (c) 2015-2026 MinIO, Inc.
//
// This file is part of MinIO Object Storage stack
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package main

import (
	"debug/buildinfo"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"sort"
	"strings"

	"golang.org/x/mod/modfile"
)

const (
	minimumGoDirective   = "1.25.0"
	compatibilityVersion = "1.25.13"
	releaseToolchain     = "go1.26.6"
	golangciLintVersion  = "v2.12.2"
)

var concreteGoVersion = regexp.MustCompile(`(?m)go-version:\s*(?:\[\s*)?["']?([0-9]+\.[0-9]+(?:\.[0-9]+|\.x))`)

type options struct {
	root          string
	binary        string
	revision      string
	goos          string
	goarch        string
	allowModified bool
}

func main() {
	opts := options{}
	flag.StringVar(&opts.root, "root", ".", "repository root to verify")
	flag.StringVar(&opts.binary, "binary", "", "optional Go binary to verify")
	flag.StringVar(&opts.revision, "revision", "", "expected VCS revision for -binary")
	flag.StringVar(&opts.goos, "goos", "", "expected GOOS for -binary")
	flag.StringVar(&opts.goarch, "goarch", "", "expected GOARCH for -binary")
	flag.BoolVar(&opts.allowModified, "allow-modified", false, "allow a development binary built from a modified worktree")
	flag.Parse()

	if err := run(opts); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(opts options) error {
	var problems []string
	if err := verifySourcePolicy(opts.root); err != nil {
		problems = appendErrorProblems(problems, err)
	}
	if opts.binary != "" {
		info, err := buildinfo.ReadFile(opts.binary)
		if err != nil {
			problems = append(problems, fmt.Sprintf("read build info from %s: %v", opts.binary, err))
		} else if err = verifyBuildInfo(info, opts); err != nil {
			problems = appendErrorProblems(problems, err)
		}
	}
	return joinProblems(problems)
}

func verifySourcePolicy(root string) error {
	var problems []string

	goMod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		problems = append(problems, fmt.Sprintf("read go.mod: %v", err))
	} else {
		parsed, parseErr := modfile.Parse("go.mod", goMod, nil)
		if parseErr != nil {
			problems = append(problems, fmt.Sprintf("parse go.mod: %v", parseErr))
		} else {
			if parsed.Go == nil || parsed.Go.Version != minimumGoDirective {
				problems = append(problems, fmt.Sprintf("go directive must be %s", minimumGoDirective))
			}
			if parsed.Toolchain == nil || parsed.Toolchain.Name != releaseToolchain {
				problems = append(problems, fmt.Sprintf("toolchain directive must be %s", releaseToolchain))
			}
		}
	}

	checkdeps, err := os.ReadFile(filepath.Join(root, "buildscripts", "checkdeps.sh"))
	if err != nil {
		problems = append(problems, fmt.Sprintf("read buildscripts/checkdeps.sh: %v", err))
	} else if !strings.Contains(string(checkdeps), `GO_VERSION="`+minimumGoDirective+`"`) {
		problems = append(problems, fmt.Sprintf("checkdeps minimum Go version must be %s", minimumGoDirective))
	}

	problems = append(problems, verifyWorkflowPolicy(root)...)
	problems = append(problems, verifyDockerPolicy(root)...)

	makefile, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		problems = append(problems, fmt.Sprintf("read Makefile: %v", err))
	} else {
		contents := string(makefile)
		if !strings.Contains(contents, "RELEASE_GO_TOOLCHAIN ?= "+releaseToolchain) {
			problems = append(problems, "Makefile must pin RELEASE_GO_TOOLCHAIN to "+releaseToolchain)
		}
		if !strings.Contains(contents, "build-release:") {
			problems = append(problems, "Makefile must define build-release")
		}
		if !strings.Contains(contents, "GOTOOLCHAIN=$(RELEASE_GO_TOOLCHAIN)") {
			problems = append(problems, "build-release must select RELEASE_GO_TOOLCHAIN explicitly")
		}
		if !strings.Contains(contents, "GOLANGCI_VERSION ?= "+golangciLintVersion) {
			problems = append(problems, "GOLANGCI_VERSION must be "+golangciLintVersion)
		}
		pinnedLinterInstall := "go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)"
		if !strings.Contains(contents, pinnedLinterInstall) {
			problems = append(problems, "getdeps must install pinned golangci-lint")
		}
	}

	return joinProblems(problems)
}

func verifyWorkflowPolicy(root string) []string {
	workflowDir := filepath.Join(root, ".github", "workflows")
	entries, err := os.ReadDir(workflowDir)
	if err != nil {
		return []string{fmt.Sprintf("read .github/workflows: %v", err)}
	}

	var problems []string
	foundCompatibilityWorkflow := false
	for _, entry := range entries {
		if entry.IsDir() || (filepath.Ext(entry.Name()) != ".yml" && filepath.Ext(entry.Name()) != ".yaml") {
			continue
		}
		path := filepath.Join(workflowDir, entry.Name())
		contents, readErr := os.ReadFile(path)
		if readErr != nil {
			problems = append(problems, fmt.Sprintf("read %s: %v", filepath.ToSlash(path), readErr))
			continue
		}
		text := string(contents)
		if !strings.Contains(text, "actions/setup-go@") {
			continue
		}
		if !strings.Contains(text, "GOTOOLCHAIN: local") {
			problems = append(problems, entry.Name()+" must set GOTOOLCHAIN to local")
		}

		expected := "1.26.6"
		if entry.Name() == "go-compat.yml" || entry.Name() == "go-compat.yaml" {
			foundCompatibilityWorkflow = true
			expected = compatibilityVersion
		}
		versions := concreteGoVersion.FindAllStringSubmatch(text, -1)
		if len(versions) == 0 {
			problems = append(problems, fmt.Sprintf("%s must pin Go %s", entry.Name(), expected))
			continue
		}
		for _, match := range versions {
			if match[1] != expected {
				problems = append(problems, fmt.Sprintf("%s must pin Go %s, found %s", entry.Name(), expected, match[1]))
			}
		}
	}
	if !foundCompatibilityWorkflow {
		problems = append(problems, "go-compat.yml must exist")
	}
	return problems
}

func verifyDockerPolicy(root string) []string {
	paths, err := filepath.Glob(filepath.Join(root, "Dockerfile*"))
	if err != nil {
		return []string{fmt.Sprintf("find Dockerfiles: %v", err)}
	}

	var problems []string
	for _, path := range paths {
		contents, readErr := os.ReadFile(path)
		if readErr != nil {
			problems = append(problems, fmt.Sprintf("read %s: %v", filepath.Base(path), readErr))
			continue
		}
		text := string(contents)
		if !strings.Contains(text, "FROM golang:") {
			continue
		}
		if !strings.Contains(text, "FROM golang:1.26.6-alpine") {
			problems = append(problems, filepath.Base(path)+" must use golang:1.26.6-alpine")
		}
	}
	return problems
}

func verifyBuildInfo(info *debug.BuildInfo, opts options) error {
	if info == nil {
		return errors.New("build info is missing")
	}
	var problems []string
	if info.GoVersion != releaseToolchain {
		problems = append(problems, fmt.Sprintf("compiler must be %s, found %s", releaseToolchain, info.GoVersion))
	}

	settings := make(map[string]string, len(info.Settings))
	for _, setting := range info.Settings {
		settings[setting.Key] = setting.Value
	}
	problems = appendExpectedSetting(problems, settings, "CGO_ENABLED", "0")
	if opts.goos != "" {
		problems = appendExpectedSetting(problems, settings, "GOOS", opts.goos)
	}
	if opts.goarch != "" {
		problems = appendExpectedSetting(problems, settings, "GOARCH", opts.goarch)
	}
	if opts.revision != "" {
		problems = appendExpectedSetting(problems, settings, "vcs.revision", opts.revision)
	}
	if !opts.allowModified {
		problems = appendExpectedSetting(problems, settings, "vcs.modified", "false")
	}

	defaultGODEBUG := parseGODEBUG(settings["DefaultGODEBUG"])
	for _, expected := range []struct {
		name  string
		value string
	}{
		{name: "cryptocustomrand", value: "1"},
		{name: "urlstrictcolons", value: "0"},
		{name: "tlssecpmlkem", value: "0"},
	} {
		if defaultGODEBUG[expected.name] != expected.value {
			problems = append(problems, fmt.Sprintf("DefaultGODEBUG %s must be %s, found %s", expected.name, expected.value, defaultGODEBUG[expected.name]))
		}
	}
	for _, name := range []string{
		"containermaxprocs",
		"decoratemappings",
		"tlssha1",
		"updatemaxprocs",
		"x509sha256skid",
	} {
		if value, ok := defaultGODEBUG[name]; ok {
			problems = append(problems, fmt.Sprintf("DefaultGODEBUG must not override %s, found %s", name, value))
		}
	}

	return joinProblems(problems)
}

func appendExpectedSetting(problems []string, settings map[string]string, name, expected string) []string {
	if settings[name] != expected {
		return append(problems, fmt.Sprintf("%s must be %s, found %s", name, expected, settings[name]))
	}
	return problems
}

func parseGODEBUG(value string) map[string]string {
	settings := make(map[string]string)
	for _, field := range strings.Split(value, ",") {
		name, setting, ok := strings.Cut(field, "=")
		if ok {
			settings[name] = setting
		}
	}
	return settings
}

func joinProblems(problems []string) error {
	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	return &policyError{problems: problems}
}

type policyError struct {
	problems []string
}

func (e *policyError) Error() string {
	return "Go toolchain policy violations:\n- " + strings.Join(e.problems, "\n- ")
}

func appendErrorProblems(problems []string, err error) []string {
	var policyErr *policyError
	if errors.As(err, &policyErr) {
		return append(problems, policyErr.problems...)
	}
	return append(problems, err.Error())
}
