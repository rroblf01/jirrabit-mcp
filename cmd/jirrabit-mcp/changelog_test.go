package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// This test exists because it happened. A release was cut, and then two more
// commits landed that also edited the CHANGELOG. The new entries were appended
// to the already-published section, so the file claimed that v1.1.0 shipped a
// fix and a set of instructions it did not ship, and there was no Unreleased
// section left to record them in. Nobody noticed for a while, because a
// changelog that over-claims still reads perfectly well.
//
// It only bites after a release, so it skips when there is no tag. That is
// deliberate: a failing test on a repo with no releases yet would be noise.
func TestPublishedSectionsAreNotEditedAfterTheirTag(t *testing.T) {
	tags, err := tagsPointingAtReleases()
	if err != nil {
		t.Skipf("no se pueden leer los tags: %v", err)
	}
	if len(tags) == 0 {
		t.Skip("todavia no hay ninguna release publicada")
	}

	current, err := readChangelog()
	if err != nil {
		t.Fatalf("no se puede leer el CHANGELOG: %v", err)
	}

	for _, tag := range tags {
		published, err := exec.Command("git", "show", tag+":CHANGELOG.md").Output()
		if err != nil {
			t.Logf("%s: el CHANGELOG no existia en ese tag, nada que comparar", tag)
			continue
		}
		was, now := string(published), string(current)
		if was == now {
			continue
		}

		// Cada seccion publicada debe seguir byte a byte como se publico. Lo que
		// se anadio despues va en [Unreleased], que es justo para eso.
		for _, header := range releasedHeaders(was) {
			before, okBefore := section(was, header)
			after, okAfter := section(now, header)
			if !okAfter {
				t.Errorf("%s: la seccion %s desaparece del CHANGELOG actual tras haber sido publicada", tag, header)
				continue
			}
			if !okBefore {
				continue
			}
			if before != after {
				t.Errorf("%s: la seccion publicada %s fue editada despues del tag.\n"+
					"El trabajo nuevo va en [Unreleased]; una version ya publicada no se reescribe.\n"+
					"--- como se publico ---\n%s\n--- como esta ahora ---\n%s",
					tag, header, before, after)
			}
		}
	}
}

// TestNewEntriesGoUnderUnreleased is the other half: new work with nowhere to go
// tends to land in the newest released section.
//
// The condition is that there IS new work. Right after a release there is none,
// and demanding an [Unreleased] heading then would fail on every release and get
// ignored, which is worse than not having the test at all -- that is what the
// first version of this check did. "New work" means the file differs from the
// copy in the most recent tag, ignoring the reference links at the foot, which
// change whenever a release adds its own compare URL.
func TestNewEntriesGoUnderUnreleased(t *testing.T) {
	latest, err := newestTag()
	if err != nil || latest == "" {
		t.Skip("sin tags: todo el contenido es trabajo sin publicar todavia")
	}

	published, err := exec.Command("git", "show", latest+":CHANGELOG.md").Output()
	if err != nil {
		t.Skipf("el CHANGELOG no existia todavia en %s", latest)
	}
	current, err := readChangelog()
	if err != nil {
		t.Fatalf("no se puede leer el CHANGELOG: %v", err)
	}
	if withoutLinks(string(published)) == withoutLinks(string(current)) {
		t.Skipf("el CHANGELOG es identico al de %s: no hay trabajo sin publicar", latest)
	}

	if !strings.Contains(string(current), "## [Unreleased]") {
		t.Errorf("el CHANGELOG difiere del de %s pero no hay seccion [Unreleased]: "+
			"el trabajo nuevo se esta metiendo en una version ya publicada", latest)
	}
}

// newestTag is the most recent tag reachable from HEAD, which is the release the
// next one follows.
func newestTag() (string, error) {
	out, err := exec.Command("git", "describe", "--tags", "--abbrev=0").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// withoutLinks drops the reference-link block at the foot of the file.
func withoutLinks(text string) string {
	var kept []string
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "[") && strings.Contains(line, "]: http") {
			continue
		}
		kept = append(kept, strings.TrimRight(line, " \t"))
	}
	return strings.TrimRight(strings.Join(kept, "\n"), "\n")
}

// readChangelog finds the file from the repo root. `go test` runs with the
// package directory as the working directory, so a plain "CHANGELOG.md" looks
// for it one level too deep.
func readChangelog() ([]byte, error) {
	dir, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	for {
		candidate := filepath.Join(dir, "CHANGELOG.md")
		if _, statErr := os.Stat(candidate); statErr == nil {
			return os.ReadFile(candidate)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, fmt.Errorf("CHANGELOG.md no encontrado subiendo desde el directorio de pruebas")
		}
		dir = parent
	}
}

var headerRE = regexp.MustCompile(`(?m)^## .+$`)

func releasedHeaders(text string) []string {
	var out []string
	for _, m := range headerRE.FindAllString(text, -1) {
		if !strings.Contains(m, "[Unreleased]") {
			out = append(out, m)
		}
	}
	return out
}

// section returns the body of a "## ..." section, up to the next one.
//
// The reference links at the foot of the file are stripped first. They live
// inside whichever section is last, so leaving them in would make every release
// that adds a link -- which is every release, since each one needs a compare URL
// -- look like an edit to the previous version's notes.
func section(text, header string) (string, bool) {
	i := strings.Index(text, header)
	if i < 0 {
		return "", false
	}
	rest := text[i+len(header):]
	if j := headerRE.FindStringIndex(rest); j != nil {
		rest = rest[:j[0]]
	}
	var kept []string
	for _, line := range strings.Split(rest, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "[") && strings.Contains(line, "]: http") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.TrimRight(strings.Join(kept, "\n"), "\n"), true
}

func tagsPointingAtReleases() ([]string, error) {
	out, err := exec.Command("git", "tag", "-l", "v*").Output()
	if err != nil {
		return nil, err
	}
	var tags []string
	for _, t := range strings.Fields(string(out)) {
		tags = append(tags, t)
	}
	return tags, nil
}
