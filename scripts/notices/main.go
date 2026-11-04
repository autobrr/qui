// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Command notices writes THIRD_PARTY_NOTICES, the file that the release
// archives and the Docker image ship. It holds the license of the community
// themes, the notices of their sources, and the license files of the Go and npm
// dependencies.
//
// Run it from the repo root, after the web build and after the community theme
// fetch: go run ./scripts/notices
package main

import (
	"context"
	_ "embed"
	"encoding/json/v2"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

const (
	outputFile       = "THIRD_PARTY_NOTICES"
	communityLicense = "internal/themes/assets/premium/LICENSE"
)

// themeNotices holds the notices of the sources of the community themes. A theme
// that uses a new source needs a new entry in this file.
//
//go:embed themes.txt
var themeNotices string

// The build targets in .goreleaser.release.yml. Windows and the others import
// packages that Linux does not.
var goosTargets = []string{"linux", "windows", "darwin", "freebsd"}

var (
	licenseName = regexp.MustCompile(`(?i)^(licen[cs]e|unlicense|copying|copyright|notice|patents)([._-].*)?$`)
	sourceExt   = regexp.MustCompile(`(?i)\.(go|[cm]?[jt]s|json|map|css|html|ya?ml|sh|py)$`)
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "notices:", err)
		os.Exit(1)
	}
}

func run() error {
	community, err := os.ReadFile(communityLicense)
	if err != nil {
		return fmt.Errorf("the community themes are not fetched: %w", err)
	}
	goText, err := goNotices()
	if err != nil {
		return err
	}
	npmText, err := npmNotices()
	if err != nil {
		return err
	}

	var b strings.Builder
	b.WriteString("Third-party notices for qui\n")
	writeSection(&b, "Community themes (github.com/autobrr/qui-community-themes)", string(community))
	writeSection(&b, "Sources of the community themes", themeNotices)
	writeSection(&b, "Go and its modules", goText)
	writeSection(&b, "npm packages, including the packages that only the build uses", npmText)
	//nolint:gosec // the release archive and the Docker image ship this file, so everyone can read it
	return os.WriteFile(outputFile, []byte(b.String()), 0o644)
}

func writeSection(b *strings.Builder, title, text string) {
	fmt.Fprintf(b, "\n%s\n%s\n%s\n\n%s\n", strings.Repeat("=", 78), title, strings.Repeat("=", 78), strings.TrimSpace(text))
}

// licenseFiles returns the license files in dir and in each parent of dir, up
// to and including root. A module can hold a license for one folder only.
func licenseFiles(dir, root string) ([]string, error) {
	var files []string
	for {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if !licenseName.MatchString(entry.Name()) || sourceExt.MatchString(entry.Name()) {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			// Stat follows a symlink, and a package can link its license file.
			if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
				files = append(files, path)
			}
		}
		parent := filepath.Dir(dir)
		if dir == root || parent == dir {
			return files, nil
		}
		dir = parent
	}
}

// writeEntry writes the license files of one module or package. Without a file,
// it writes fallback, and warns because a person must look at that dependency.
func writeEntry(b *strings.Builder, title, root string, files []string, fallback string) error {
	fmt.Fprintf(b, "%s\n%s\n\n", title, strings.Repeat("-", len(title)))
	if len(files) == 0 {
		fmt.Fprintf(os.Stderr, "notices: no license file for %s\n", title)
		fmt.Fprintf(b, "%s\n\n", fallback)
		return nil
	}
	slices.Sort(files)
	for _, file := range files {
		text, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, file)
		if err != nil {
			return err
		}
		fmt.Fprintf(b, "[%s]\n\n%s\n\n", filepath.ToSlash(rel), strings.TrimSpace(string(text)))
	}
	return nil
}

func goNotices() (string, error) {
	type module struct {
		root  string
		files map[string]bool
	}
	modules := map[string]*module{}
	for _, goos := range goosTargets {
		cmd := exec.CommandContext(context.Background(), "go", "list", "-deps",
			"-f", `{{if and .Module (not .Module.Main)}}{{.Dir}}{{"\t"}}{{.Module.Path}} {{.Module.Version}}{{"\t"}}{{.Module.Dir}}{{end}}`,
			"./cmd/qui")
		cmd.Env = append(os.Environ(), "GOOS="+goos, "CGO_ENABLED=0")
		cmd.Stderr = os.Stderr
		out, err := cmd.Output()
		if err != nil {
			return "", fmt.Errorf("go list for %s: %w", goos, err)
		}
		for line := range strings.Lines(string(out)) {
			fields := strings.Split(strings.TrimSpace(line), "\t")
			if len(fields) != 3 {
				continue
			}
			pkgDir, title, root := fields[0], fields[1], fields[2]
			mod := modules[title]
			if mod == nil {
				mod = &module{root: root, files: map[string]bool{}}
				modules[title] = mod
			}
			files, err := licenseFiles(pkgDir, root)
			if err != nil {
				return "", err
			}
			for _, file := range files {
				mod.files[file] = true
			}
		}
	}

	cmd := exec.CommandContext(context.Background(), "go", "env", "GOROOT")
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("go env GOROOT: %w", err)
	}
	goroot := strings.TrimSpace(string(out))
	goFiles, err := licenseFiles(goroot, goroot)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	if err := writeEntry(&b, "Go", goroot, goFiles, ""); err != nil {
		return "", err
	}
	for _, title := range slices.Sorted(maps.Keys(modules)) {
		mod := modules[title]
		files := slices.Collect(maps.Keys(mod.files))
		if err := writeEntry(&b, title, mod.root, files, "The module has no license file."); err != nil {
			return "", err
		}
	}
	return b.String(), nil
}

func npmNotices() (string, error) {
	cmd := exec.CommandContext(context.Background(), "pnpm", "licenses", "list", "--json")
	cmd.Dir = "web"
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("pnpm licenses list: %w", err)
	}
	var byLicense map[string][]struct {
		Name     string   `json:"name"`
		Versions []string `json:"versions"`
		Paths    []string `json:"paths"`
		Author   string   `json:"author"`
	}
	if err := json.Unmarshal(out, &byLicense); err != nil {
		return "", fmt.Errorf("read pnpm licenses list: %w", err)
	}

	type pkg struct{ dir, fallback string }
	pkgs := map[string]pkg{}
	for license, list := range byLicense {
		for _, p := range list {
			for i, dir := range p.Paths {
				title := p.Name + " " + p.Versions[i]
				fallback := fmt.Sprintf("The package has no license file. It states the license %s.", license)
				if p.Author != "" {
					fallback += " Author: " + p.Author + "."
				}
				pkgs[title] = pkg{dir, fallback}
			}
		}
	}

	var b strings.Builder
	for _, title := range slices.Sorted(maps.Keys(pkgs)) {
		p := pkgs[title]
		files, err := licenseFiles(p.dir, p.dir)
		if err != nil {
			return "", err
		}
		if err := writeEntry(&b, title, p.dir, files, p.fallback); err != nil {
			return "", err
		}
	}
	return b.String(), nil
}
