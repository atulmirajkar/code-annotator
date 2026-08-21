package gitdiff

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

const maxPatchBytes = 8 << 20

// ErrUnsupportedPatch reports patch data that cannot be safely aligned with
// the supplied base and current UTF-8 sources.
var ErrUnsupportedPatch = errors.New("unsupported Git patch")

// RowKind describes how one aligned side-by-side row relates to the base.
type RowKind string

const (
	RowUnchanged RowKind = "unchanged"
	RowAdded     RowKind = "added"
	RowModified  RowKind = "modified"
	RowDeleted   RowKind = "deleted"
)

// FileDiff is the complete aligned display sequence for one current file.
type FileDiff struct {
	Path       string
	BasePath   string
	BaseCommit string
	Rows       []Row
}

// Row pairs at most one base line with at most one current line. Current byte
// offsets exclude line terminators and remain zero for a deleted-only row.
type Row struct {
	Kind         RowKind
	OldLine      int
	NewLine      int
	CurrentStart int
	CurrentEnd   int
	BaseText     string
}

type sourceLine struct {
	text  string
	start int
	end   int
}

type hunkRange struct {
	// oldStart and oldCount are the -start,count range from a hunk header.
	oldStart int
	oldCount int
	// newStart and newCount are the +start,count range from a hunk header.
	newStart int
	newCount int
}

// Unified patches contain optional file headers followed by every changed
// region as a hunk. A hunk has this shape:
//
//	@@ -oldStart,oldCount +newStart,newCount @@ optional section
//	 unchanged context
//	-deleted base line
//	+added current line
//	\ No newline at end of file
//
// Counts default to one when omitted. With --unified=0, Git normally emits no
// context records, so unchanged regions between hunks are absent from the patch.
var hunkHeaderPattern = regexp.MustCompile(`^@@ -([0-9]+)(?:,([0-9]+))? \+([0-9]+)(?:,([0-9]+))? @@(?: .*)?$`)

// ParsePatch verifies a bounded textual unified patch against exact base and
// current bytes, then reconstructs complete side-by-side rows including the
// unchanged regions omitted by a zero-context patch.
func ParsePatch(documentPath, baseCommit string, base, current, patch []byte) (FileDiff, error) {
	if len(patch) > maxPatchBytes {
		return FileDiff{}, fmt.Errorf("%w: patch exceeds %d bytes", ErrUnsupportedPatch, maxPatchBytes)
	}
	if !validObjectID(baseCommit) {
		return FileDiff{}, fmt.Errorf("%w: invalid base commit", ErrUnsupportedPatch)
	}
	if !validText(base) || !validText(current) || !validText(patch) {
		return FileDiff{}, fmt.Errorf("%w: input is not UTF-8 text", ErrUnsupportedPatch)
	}
	if bytes.Contains(patch, []byte("GIT binary patch")) || bytes.Contains(patch, []byte("Binary files ")) {
		return FileDiff{}, fmt.Errorf("%w: binary patch", ErrUnsupportedPatch)
	}

	baseLines := splitSourceLines(base)
	currentLines := splitSourceLines(current)
	rows, hunks, err := alignPatch(baseLines, currentLines, string(patch))
	if err != nil {
		return FileDiff{}, err
	}
	if hunks == 0 && !bytes.Equal(base, current) {
		return FileDiff{}, fmt.Errorf("%w: content differs without textual hunks", ErrUnsupportedPatch)
	}
	return FileDiff{Path: documentPath, BasePath: documentPath, BaseCommit: strings.ToLower(baseCommit), Rows: rows}, nil
}

// alignPatch skips non-consuming file headers, consumes all hunks exactly once,
// and fills omitted unchanged lines from verified base/current source arrays.
// A patch need not contain the whole file, but it must contain every textual
// change; otherwise unchanged-region or final source validation fails.
func alignPatch(base, current []sourceLine, patch string) ([]Row, int, error) {
	patchLines := strings.Split(strings.TrimSuffix(patch, "\n"), "\n")
	if patch == "" {
		patchLines = nil
	}
	rows := make([]Row, 0, max(len(base), len(current)))
	oldCursor, newCursor, hunkCount := 0, 0, 0
	for index := 0; index < len(patchLines); {
		if !strings.HasPrefix(patchLines[index], "@@ ") {
			index++
			continue
		}
		rangeValue, err := parseHunkHeader(patchLines[index])
		if err != nil {
			return nil, 0, err
		}
		hunkCount++
		oldTarget := hunkIndex(rangeValue.oldStart, rangeValue.oldCount)
		newTarget := hunkIndex(rangeValue.newStart, rangeValue.newCount)
		if oldTarget < oldCursor || newTarget < newCursor || oldTarget > len(base) || newTarget > len(current) {
			return nil, 0, fmt.Errorf("%w: hunk range is outside or overlaps source", ErrUnsupportedPatch)
		}
		rows, oldCursor, newCursor, err = appendUnchanged(rows, base, current, oldCursor, newCursor, oldTarget, newTarget)
		if err != nil {
			return nil, 0, err
		}

		index++
		oldConsumed, newConsumed := 0, 0
		oldGroup, newGroup := []int{}, []int{}
		flush := func() {
			rows = appendChangedGroup(rows, base, current, oldGroup, newGroup)
			oldGroup, newGroup = nil, nil
		}
		for oldConsumed < rangeValue.oldCount || newConsumed < rangeValue.newCount {
			if index >= len(patchLines) {
				return nil, 0, fmt.Errorf("%w: truncated hunk", ErrUnsupportedPatch)
			}
			line := patchLines[index]
			if line == `\ No newline at end of file` {
				index++
				continue
			}
			if line == "" {
				return nil, 0, fmt.Errorf("%w: malformed hunk line", ErrUnsupportedPatch)
			}
			payload := strings.TrimSuffix(line[1:], "\r")
			switch line[0] {
			case '-':
				if oldCursor >= len(base) || base[oldCursor].text != payload {
					return nil, 0, fmt.Errorf("%w: deleted line does not match base", ErrUnsupportedPatch)
				}
				oldGroup = append(oldGroup, oldCursor)
				oldCursor++
				oldConsumed++
			case '+':
				if newCursor >= len(current) || current[newCursor].text != payload {
					return nil, 0, fmt.Errorf("%w: added line does not match current source", ErrUnsupportedPatch)
				}
				newGroup = append(newGroup, newCursor)
				newCursor++
				newConsumed++
			case ' ':
				flush()
				if oldCursor >= len(base) || newCursor >= len(current) || base[oldCursor].text != payload || current[newCursor].text != payload {
					return nil, 0, fmt.Errorf("%w: context line does not match source", ErrUnsupportedPatch)
				}
				rows = append(rows, unchangedRow(base[oldCursor], current[newCursor], oldCursor, newCursor))
				oldCursor++
				newCursor++
				oldConsumed++
				newConsumed++
			default:
				return nil, 0, fmt.Errorf("%w: unexpected hunk line", ErrUnsupportedPatch)
			}
			if oldConsumed > rangeValue.oldCount || newConsumed > rangeValue.newCount {
				return nil, 0, fmt.Errorf("%w: hunk line count exceeds header", ErrUnsupportedPatch)
			}
			index++
		}
		flush()
		for index < len(patchLines) && patchLines[index] == `\ No newline at end of file` {
			index++
		}
	}
	var err error
	rows, oldCursor, newCursor, err = appendUnchanged(rows, base, current, oldCursor, newCursor, len(base), len(current))
	if err != nil {
		return nil, 0, err
	}
	return rows, hunkCount, nil
}

func parseHunkHeader(header string) (hunkRange, error) {
	matches := hunkHeaderPattern.FindStringSubmatch(header)
	if matches == nil {
		return hunkRange{}, fmt.Errorf("%w: malformed hunk header", ErrUnsupportedPatch)
	}
	values := [4]int{}
	for index, raw := range []string{matches[1], matches[2], matches[3], matches[4]} {
		if raw == "" {
			values[index] = 1
			continue
		}
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			return hunkRange{}, fmt.Errorf("%w: malformed hunk range", ErrUnsupportedPatch)
		}
		values[index] = parsed
	}
	return hunkRange{oldStart: values[0], oldCount: values[1], newStart: values[2], newCount: values[3]}, nil
}

func hunkIndex(start, count int) int {
	if count == 0 {
		return start
	}
	return start - 1
}

// appendUnchanged verifies that both omitted ranges have the same length and
// content before representing them as unchanged rows.
func appendUnchanged(rows []Row, base, current []sourceLine, oldCursor, newCursor, oldTarget, newTarget int) ([]Row, int, int, error) {
	if oldTarget-oldCursor != newTarget-newCursor {
		return nil, 0, 0, fmt.Errorf("%w: unrepresented source difference", ErrUnsupportedPatch)
	}
	for oldCursor < oldTarget {
		if base[oldCursor].text != current[newCursor].text {
			return nil, 0, 0, fmt.Errorf("%w: unchanged lines differ", ErrUnsupportedPatch)
		}
		rows = append(rows, unchangedRow(base[oldCursor], current[newCursor], oldCursor, newCursor))
		oldCursor++
		newCursor++
	}
	return rows, oldCursor, newCursor, nil
}

func appendChangedGroup(rows []Row, base, current []sourceLine, oldLines, newLines []int) []Row {
	for index := 0; index < max(len(oldLines), len(newLines)); index++ {
		row := Row{}
		if index < len(oldLines) {
			oldIndex := oldLines[index]
			row.OldLine = oldIndex + 1
			row.BaseText = base[oldIndex].text
		}
		if index < len(newLines) {
			newIndex := newLines[index]
			row.NewLine = newIndex + 1
			row.CurrentStart = current[newIndex].start
			row.CurrentEnd = current[newIndex].end
		}
		switch {
		case row.OldLine > 0 && row.NewLine > 0:
			row.Kind = RowModified
		case row.OldLine > 0:
			row.Kind = RowDeleted
		default:
			row.Kind = RowAdded
		}
		rows = append(rows, row)
	}
	return rows
}

func unchangedRow(base, current sourceLine, oldIndex, newIndex int) Row {
	return Row{Kind: RowUnchanged, OldLine: oldIndex + 1, NewLine: newIndex + 1, CurrentStart: current.start, CurrentEnd: current.end, BaseText: base.text}
}

// splitSourceLines mirrors source rendering: visible bytes exclude LF and an
// optional preceding CR, and a terminal newline does not create another row.
func splitSourceLines(source []byte) []sourceLine {
	lines := []sourceLine{}
	for start := 0; start < len(source); {
		newline := bytes.IndexByte(source[start:], '\n')
		end := len(source)
		next := len(source)
		if newline >= 0 {
			end = start + newline
			next = end + 1
		}
		visibleEnd := end
		if visibleEnd > start && source[visibleEnd-1] == '\r' {
			visibleEnd--
		}
		lines = append(lines, sourceLine{text: string(source[start:visibleEnd]), start: start, end: visibleEnd})
		start = next
	}
	return lines
}

func validText(value []byte) bool {
	return utf8.Valid(value) && bytes.IndexByte(value, 0) < 0
}
