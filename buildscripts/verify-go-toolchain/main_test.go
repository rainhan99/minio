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
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
)

func TestVerifySourcePolicy(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*testing.T, string)
		wantErr string
	}{
		{name: "valid"},
		{
			name: "rejects old go directive",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, "go.mod", "go 1.25.0", "go 1.24.0")
			},
			wantErr: "go directive must be 1.25.0",
		},
		{
			name: "rejects old toolchain directive",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, "go.mod", "toolchain go1.26.6", "toolchain go1.24.8")
			},
			wantErr: "toolchain directive must be go1.26.6",
		},
		{
			name: "rejects old minimum build version",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, "buildscripts/checkdeps.sh", `GO_VERSION="1.25.0"`, `GO_VERSION="1.16"`)
			},
			wantErr: "checkdeps minimum Go version must be 1.25.0",
		},
		{
			name: "rejects floating release workflow version",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, ".github/workflows/go.yml", "1.26.6", "1.26.x")
			},
			wantErr: "must pin Go 1.26.6",
		},
		{
			name: "rejects missing local toolchain mode",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, ".github/workflows/go.yml", "GOTOOLCHAIN: local", "GOTOOLCHAIN: auto")
			},
			wantErr: "must set GOTOOLCHAIN to local",
		},
		{
			name: "rejects wrong compatibility version",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, ".github/workflows/go-compat.yml", "1.25.13", "1.25.x")
			},
			wantErr: "must pin Go 1.25.13",
		},
		{
			name: "rejects missing compatibility workflow",
			mutate: func(t *testing.T, root string) {
				if err := os.Remove(filepath.Join(root, ".github", "workflows", "go-compat.yml")); err != nil {
					t.Fatalf("remove compatibility workflow: %v", err)
				}
			},
			wantErr: "go-compat.yml must exist",
		},
		{
			name: "rejects old docker builder",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, "Dockerfile.release", "golang:1.26.6-alpine", "golang:1.24-alpine")
			},
			wantErr: "must use golang:1.26.6-alpine",
		},
		{
			name: "rejects missing release target",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, "Makefile", "build-release:", "legacy-release:")
			},
			wantErr: "Makefile must define build-release",
		},
		{
			name: "rejects floating golangci lint installer",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, "Makefile", "GOLANGCI_VERSION ?= v2.12.2", "GOLANGCI_VERSION ?= latest")
			},
			wantErr: "GOLANGCI_VERSION must be v2.12.2",
		},
		{
			name: "rejects installer from master",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, "Makefile",
					"GOBIN=$(GOLANGCI_DIR) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)",
					"curl https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh")
			},
			wantErr: "getdeps must install pinned golangci-lint",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := writeSourcePolicyFixture(t)
			if test.mutate != nil {
				test.mutate(t, root)
			}

			err := verifySourcePolicy(root)
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("verifySourcePolicy() unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("verifySourcePolicy() expected error containing %q", test.wantErr)
			}
			if !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("verifySourcePolicy() error = %q, want substring %q", err, test.wantErr)
			}
		})
	}
}

func TestVerifySourcePolicyReportsAllViolations(t *testing.T) {
	root := writeSourcePolicyFixture(t)
	replaceFixture(t, root, "go.mod", "go 1.25.0", "go 1.24.0")
	replaceFixture(t, root, "buildscripts/checkdeps.sh", `GO_VERSION="1.25.0"`, `GO_VERSION="1.16"`)

	err := verifySourcePolicy(root)
	if err == nil {
		t.Fatal("verifySourcePolicy() expected an error")
	}
	for _, want := range []string{"go directive must be 1.25.0", "checkdeps minimum Go version must be 1.25.0"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("verifySourcePolicy() error = %q, want substring %q", err, want)
		}
	}
}

func TestRunDoesNotNestViolationHeading(t *testing.T) {
	root := writeSourcePolicyFixture(t)
	replaceFixture(t, root, "go.mod", "go 1.25.0", "go 1.24.0")

	err := run(options{root: root})
	if err == nil {
		t.Fatal("run() expected an error")
	}
	if got := strings.Count(err.Error(), "Go toolchain policy violations:"); got != 1 {
		t.Fatalf("run() violation heading count = %d, want 1; error: %v", got, err)
	}
}

func TestVerifyBuildInfo(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*debug.BuildInfo, *options)
		wantErr string
	}{
		{name: "valid"},
		{
			name: "rejects wrong compiler",
			mutate: func(info *debug.BuildInfo, _ *options) {
				info.GoVersion = "go1.26.5"
			},
			wantErr: "compiler must be go1.26.6",
		},
		{
			name: "rejects cgo",
			mutate: func(info *debug.BuildInfo, _ *options) {
				setBuildSetting(info, "CGO_ENABLED", "1")
			},
			wantErr: "CGO_ENABLED must be 0",
		},
		{
			name: "rejects wrong platform",
			mutate: func(info *debug.BuildInfo, _ *options) {
				setBuildSetting(info, "GOARCH", "arm64")
			},
			wantErr: "GOARCH must be amd64",
		},
		{
			name: "rejects wrong revision",
			mutate: func(info *debug.BuildInfo, _ *options) {
				setBuildSetting(info, "vcs.revision", "different")
			},
			wantErr: "vcs.revision must be deadbeef",
		},
		{
			name: "rejects modified release",
			mutate: func(info *debug.BuildInfo, _ *options) {
				setBuildSetting(info, "vcs.modified", "true")
			},
			wantErr: "vcs.modified must be false",
		},
		{
			name: "allows modified development build",
			mutate: func(info *debug.BuildInfo, opts *options) {
				setBuildSetting(info, "vcs.modified", "true")
				opts.allowModified = true
			},
		},
		{
			name: "rejects pre Go 1.25 runtime default",
			mutate: func(info *debug.BuildInfo, _ *options) {
				setBuildSetting(info, "DefaultGODEBUG", defaultGODEBUGFixture+",containermaxprocs=0")
			},
			wantErr: "DefaultGODEBUG must not override containermaxprocs",
		},
		{
			name: "rejects enabled Go 1.26 URL default",
			mutate: func(info *debug.BuildInfo, _ *options) {
				setBuildSetting(info, "DefaultGODEBUG", strings.ReplaceAll(defaultGODEBUGFixture, "urlstrictcolons=0", "urlstrictcolons=1"))
			},
			wantErr: "DefaultGODEBUG urlstrictcolons must be 0",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			info := validBuildInfo()
			opts := options{revision: "deadbeef", goos: "linux", goarch: "amd64"}
			if test.mutate != nil {
				test.mutate(info, &opts)
			}

			err := verifyBuildInfo(info, opts)
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("verifyBuildInfo() unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("verifyBuildInfo() expected error containing %q", test.wantErr)
			}
			if !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("verifyBuildInfo() error = %q, want substring %q", err, test.wantErr)
			}
		})
	}
}

const defaultGODEBUGFixture = "cryptocustomrand=1,tlssecpmlkem=0,urlstrictcolons=0"

func validBuildInfo() *debug.BuildInfo {
	return &debug.BuildInfo{
		GoVersion: "go1.26.6",
		Settings: []debug.BuildSetting{
			{Key: "CGO_ENABLED", Value: "0"},
			{Key: "GOOS", Value: "linux"},
			{Key: "GOARCH", Value: "amd64"},
			{Key: "vcs.revision", Value: "deadbeef"},
			{Key: "vcs.modified", Value: "false"},
			{Key: "DefaultGODEBUG", Value: defaultGODEBUGFixture},
		},
	}
}

func setBuildSetting(info *debug.BuildInfo, key, value string) {
	for i := range info.Settings {
		if info.Settings[i].Key == key {
			info.Settings[i].Value = value
			return
		}
	}
	info.Settings = append(info.Settings, debug.BuildSetting{Key: key, Value: value})
}

func writeSourcePolicyFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"go.mod":                    "module github.com/minio/minio\n\ngo 1.25.0\n\ntoolchain go1.26.6\n",
		"buildscripts/checkdeps.sh": `GO_VERSION="1.25.0"` + "\n",
		"Makefile": strings.Join([]string{
			"GOLANGCI_VERSION ?= v2.12.2",
			"GOLANGCI_DIR = .bin/golangci/$(GOLANGCI_VERSION)",
			"GOLANGCI = $(GOLANGCI_DIR)/golangci-lint",
			"RELEASE_GO_TOOLCHAIN ?= go1.26.6",
			"getdeps:",
			"\tGOBIN=$(GOLANGCI_DIR) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)",
			"build-release:",
			"\tenv GOTOOLCHAIN=$(RELEASE_GO_TOOLCHAIN) $(MAKE) build",
		}, "\n"),
		"Dockerfile.release": "FROM golang:1.26.6-alpine AS build\n",
		".github/workflows/go.yml": strings.Join([]string{
			"env:",
			"  GOTOOLCHAIN: local",
			"jobs:",
			"  build:",
			"    steps:",
			"      - uses: actions/setup-go@v5",
			"        with:",
			"          go-version: 1.26.6",
		}, "\n"),
		".github/workflows/go-compat.yml": strings.Join([]string{
			"env:",
			"  GOTOOLCHAIN: local",
			"jobs:",
			"  build:",
			"    steps:",
			"      - uses: actions/setup-go@v5",
			"        with:",
			"          go-version: 1.25.13",
		}, "\n"),
	}
	for name, contents := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create fixture directory: %v", err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatalf("write fixture %s: %v", name, err)
		}
	}
	return root
}

func replaceFixture(t *testing.T, root, name, old, replacement string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	updated := strings.Replace(string(contents), old, replacement, 1)
	if updated == string(contents) {
		t.Fatalf("fixture %s does not contain %q", name, old)
	}
	if err = os.WriteFile(path, []byte(updated), 0o600); err != nil {
		t.Fatalf("update fixture %s: %v", name, err)
	}
}
