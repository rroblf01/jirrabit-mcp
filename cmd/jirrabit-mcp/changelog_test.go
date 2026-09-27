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

// TestAnUnreleasedSectionExistsWhenThereIsUnshippedWork is the other half: new
// work with nowhere to go tends to land in the newest released section.
func TestNewEntriesGoUnderUnreleased(t *testing.T) {
	// Si no hay ningun tag, el fichero entero es trabajo sin publicar y el
	// Unreleased no aporta nada todavia.
	if out, err := exec.Command("git", "tag", "-l").Output(); err == nil && strings.TrimSpace(string(out)) == "" {
		t.Skip("sin tags: todo el contenido es [Unreleased]")
	}

	current, err := readChangelog()
	if err != nil {
		t.Fatalf("no se puede leer el CHANGELOG: %v", err)
	}
	if !strings.Contains(string(current), "## [Unreleased]") {
		t.Error("hay tags publicados pero no hay seccion [Unreleased]: " +
			"el trabajo nuevo se ira al lado de una version ya publicada")
	}
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
