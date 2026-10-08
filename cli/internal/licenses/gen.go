//go:build ignore

// Regenerates licenses.txt: filedrop's own license, the Go standard library's
// and those of every module linked into the CLI on any release target OS.
// Run via `go generate ./internal/licenses` after changing dependencies.
package main

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

const goLicenses = "github.com/google/go-licenses/v2@v2.0.1"

// The targets the release workflow builds for. Dependencies differ between
// them (cobra pulls in mousetrap only on Windows, x/crypto links x/sys/cpu only
// on amd64), so every one is checked rather than just the host
var targets = []string{
	"linux/amd64", "linux/arm64",
	"darwin/amd64", "darwin/arm64",
	"windows/amd64", "windows/arm64",
}

// One line per library: name, version, license, path to its license file
const report = "{{range .}}{{.Name}}\t{{.Version}}\t{{.LicenseName}}\t{{.LicensePath}}\n{{end}}"

// The standard library is compiled in too but go-licenses skips it. Its
// license is the same text as golang.org/x/*'s (and GOROOT/LICENSE isn't
// always installed), so it's listed in the group containing this module.
const stdlibSibling = "golang.org/x/sys"

// Libraries sharing a license text, listed together under one copy of it
type group struct {
	libs     []string // "name version"
	licenses []string
	text     string
}

func main() {
	log.SetFlags(0)
	tmp, err := os.MkdirTemp("", "filedrop-licenses")
	check(err)
	defer os.RemoveAll(tmp)

	// Built for the host: `GOOS=windows go run` would build a Windows binary
	cmd := exec.Command("go", "install", goLicenses)
	cmd.Env = append(os.Environ(), "GOBIN="+tmp)
	cmd.Stderr = os.Stderr
	check(cmd.Run())
	tmpl := filepath.Join(tmp, "report.tmpl")
	check(os.WriteFile(tmpl, []byte(report), 0o644))

	// go-licenses names libraries by their packages' common import path,
	// which varies by target (golang.org/x/sys vs golang.org/x/sys/unix), so
	// they're listed by module instead
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Path}}", "all").Output()
	check(err)
	modules := strings.Fields(string(out))

	byText := map[string]*group{}
	for _, target := range targets {
		goos, goarch, _ := strings.Cut(target, "/")
		var stdout, stderr bytes.Buffer
		cmd := exec.Command(filepath.Join(tmp, "go-licenses"), "report", "filedrop-cli",
			"--ignore", "filedrop-cli", "--template", tmpl)
		cmd.Dir = "../.."
		cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+goarch)
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err := cmd.Run(); err != nil {
			log.Fatalf("go-licenses (%s): %v\n%s", target, err, stderr.Bytes())
		}
		for line := range strings.SplitSeq(strings.TrimSpace(stdout.String()), "\n") {
			f := strings.Split(line, "\t")
			if len(f) != 4 {
				log.Fatalf("unexpected go-licenses output: %q", line)
			}
			name, version, license, path := f[0], f[1], f[2], f[3]
			if license == "Unknown" || path == "Unknown" {
				log.Fatalf("no license found for %s (%s)", name, target)
			}
			text := strings.TrimSpace(string(read(path)))
			g := byText[text]
			if g == nil {
				g = &group{text: text}
				byText[text] = g
			}
			mod := module(modules, name)
			lib := mod
			if version != "Unknown" {
				lib += " " + version
			}
			if mod == stdlibSibling {
				g.libs = addUnique(g.libs, "Go standard library")
			}
			g.libs = addUnique(g.libs, lib)
			g.licenses = addUnique(g.licenses, license)
		}
	}

	groups := make([]*group, 0, len(byText))
	for _, g := range byText {
		slices.Sort(g.libs)
		groups = append(groups, g)
	}
	slices.SortFunc(groups, func(a, b *group) int { return strings.Compare(a.libs[0], b.libs[0]) })
	if !slices.ContainsFunc(groups, func(g *group) bool { return slices.Contains(g.libs, "Go standard library") }) {
		log.Fatalf("%s not linked, so the Go standard library's license is missing", stdlibSibling)
	}

	var txt bytes.Buffer
	txt.WriteString("filedrop\n\n")
	txt.Write(bytes.TrimSpace(read("../../../LICENSE")))
	txt.WriteString("\n\n\nThe filedrop CLI includes the following third-party software.\n")
	rule := strings.Repeat("-", 80)
	for _, g := range groups {
		fmt.Fprintf(&txt, "\n%s\n%s\nLicense: %s\n%s\n\n%s\n", rule, strings.Join(g.libs, "\n"),
			strings.Join(g.licenses, ", "), rule, g.text)
	}
	check(os.WriteFile("licenses.txt", txt.Bytes(), 0o644))
}

// The module providing the package (path prefix) name, the longest match
// winning so nested modules aren't attributed to their parent
func module(modules []string, name string) string {
	best := ""
	for _, m := range modules {
		if (name == m || strings.HasPrefix(name, m+"/")) && len(m) > len(best) {
			best = m
		}
	}
	if best == "" {
		log.Fatalf("no module provides %s", name)
	}
	return best
}

func addUnique(list []string, s string) []string {
	if slices.Contains(list, s) {
		return list
	}
	return append(list, s)
}

func read(path string) []byte {
	b, err := os.ReadFile(path)
	check(err)
	return b
}

func check(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
