package computer

import (
	"path/filepath"
	"regexp"
	"strings"
)

// The definitions a code file declares.
//
// Why this exists beside the vectors. A log line pasted into a chat says
// `mwesexecutor.py:97 ResetPayloadAngularOffset`, and the question it
// raises -- which file is that in, and who wrote it -- is one an
// embedding answers badly and a string match answers exactly. So every
// code file's top-level names go in a table of their own, and a search
// whose words look like an identifier asks that first.
//
// One expression per language rather than a parser: the name is all that
// is wanted, a wrong match costs one row, and a parser for eleven
// languages is a dependency and a maintenance burden for a lookup table.

type symbolPattern struct {
	kind    string
	pattern *regexp.Regexp
}

var symbolPatterns = map[string][]symbolPattern{
	".go": {
		{"func", regexp.MustCompile(`(?m)^func\s+(?:\([^)]*\)\s*)?([A-Za-z_]\w*)`)},
		{"type", regexp.MustCompile(`(?m)^type\s+([A-Za-z_]\w*)`)},
		{"const", regexp.MustCompile(`(?m)^(?:const|var)\s+([A-Z]\w*)`)},
	},
	".py": {
		{"def", regexp.MustCompile(`(?m)^\s*def\s+([A-Za-z_]\w*)`)},
		{"class", regexp.MustCompile(`(?m)^\s*class\s+([A-Za-z_]\w*)`)},
	},
	".ts": {
		{"function", regexp.MustCompile(`(?m)^\s*(?:export\s+)?(?:async\s+)?function\s+([A-Za-z_$][\w$]*)`)},
		{"class", regexp.MustCompile(`(?m)^\s*(?:export\s+)?(?:abstract\s+)?class\s+([A-Za-z_$][\w$]*)`)},
		{"type", regexp.MustCompile(`(?m)^\s*(?:export\s+)?(?:type|interface|enum)\s+([A-Za-z_$][\w$]*)`)},
		{"const", regexp.MustCompile(`(?m)^\s*(?:export\s+)?(?:const|let)\s+([A-Za-z_$][\w$]*)\s*[:=]`)},
	},
	".c": {
		{"func", regexp.MustCompile(`(?m)^[A-Za-z_][\w\s*]*\s+\**([A-Za-z_]\w*)\s*\([^;]*\)\s*\{`)},
		{"struct", regexp.MustCompile(`(?m)^(?:typedef\s+)?struct\s+([A-Za-z_]\w*)`)},
	},
	".java": {
		{"class", regexp.MustCompile(`(?m)^\s*(?:public|private|protected)?\s*(?:final\s+|abstract\s+)?(?:class|interface|enum|record)\s+([A-Za-z_$][\w$]*)`)},
		{"method", regexp.MustCompile(`(?m)^\s+(?:public|private|protected)\s+(?:static\s+)?[\w<>\[\],.\s]+\s+([A-Za-z_$][\w$]*)\s*\(`)},
	},
	".rs": {
		{"fn", regexp.MustCompile(`(?m)^\s*(?:pub\s+)?(?:async\s+)?fn\s+([A-Za-z_]\w*)`)},
		{"type", regexp.MustCompile(`(?m)^\s*(?:pub\s+)?(?:struct|enum|trait|type)\s+([A-Za-z_]\w*)`)},
	},
	".rb": {
		{"def", regexp.MustCompile(`(?m)^\s*def\s+(?:self\.)?([A-Za-z_]\w*[?!]?)`)},
		{"class", regexp.MustCompile(`(?m)^\s*(?:class|module)\s+([A-Za-z_]\w*)`)},
	},
	".php": {
		{"function", regexp.MustCompile(`(?m)^\s*(?:public\s+|private\s+|protected\s+|static\s+)*function\s+([A-Za-z_]\w*)`)},
		{"class", regexp.MustCompile(`(?m)^\s*(?:abstract\s+|final\s+)?(?:class|interface|trait)\s+([A-Za-z_]\w*)`)},
	},
	".sql": {
		{"table", regexp.MustCompile(`(?mi)^\s*CREATE\s+(?:TABLE|VIEW|INDEX|FUNCTION)\s+(?:IF\s+NOT\s+EXISTS\s+)?"?([A-Za-z_]\w*)"?`)},
	},
	".sh": {
		{"func", regexp.MustCompile(`(?m)^\s*(?:function\s+)?([A-Za-z_]\w*)\s*\(\)\s*\{`)},
	},
}

// Extensions that share another's shape.
var sameAs = map[string]string{
	".cc": ".c", ".cpp": ".c", ".cxx": ".c", ".h": ".c", ".hpp": ".c", ".m": ".c",
	".tsx": ".ts", ".js": ".ts", ".jsx": ".ts", ".mjs": ".ts",
	".pyi": ".py", ".kt": ".java", ".cs": ".java", ".scala": ".java",
	".bash": ".sh", ".zsh": ".sh", ".psql": ".sql",
}

// symbolsMost bounds what one file contributes: a generated file can
// declare tens of thousands of names, and a lookup table of them is
// neither useful nor small.
const symbolsMost = 400

// symbolsIn is every top-level definition a file declares, with the line
// it is on.
func symbolsIn(path, text string) []ScanSymbol {
	extension := strings.ToLower(filepath.Ext(path))
	if other, found := sameAs[extension]; found {
		extension = other
	}
	patterns := symbolPatterns[extension]
	if len(patterns) == 0 {
		return nil
	}
	lines := lineOffsets(text)
	var symbols []ScanSymbol
	seen := map[string]bool{}
	for _, entry := range patterns {
		for _, match := range entry.pattern.FindAllStringSubmatchIndex(text, symbolsMost) {
			if len(match) < 4 {
				continue
			}
			name := text[match[2]:match[3]]
			if name == "" || seen[entry.kind+":"+name] {
				continue
			}
			seen[entry.kind+":"+name] = true
			symbols = append(symbols, ScanSymbol{
				Symbol: name, Kind: entry.kind, Line: lineAt(lines, match[2]),
			})
			if len(symbols) >= symbolsMost {
				return symbols
			}
		}
	}
	return symbols
}

// lineOffsets is where each line starts, so a match can be turned into a
// line number without counting from the beginning every time.
func lineOffsets(text string) []int {
	offsets := []int{0}
	for index := 0; index < len(text); index++ {
		if text[index] == '\n' {
			offsets = append(offsets, index+1)
		}
	}
	return offsets
}

// lineAt is which line an offset falls on, by bisection.
func lineAt(offsets []int, offset int) int {
	low, high := 0, len(offsets)-1
	for low < high {
		middle := (low + high + 1) / 2
		if offsets[middle] <= offset {
			low = middle
		} else {
			high = middle - 1
		}
	}
	return low + 1
}

// LooksLikeSymbol says whether a word from a question is an identifier
// rather than an ordinary word: CamelCase, snake_case, a dotted path, or
// something with a digit in the middle. Asked of every word of a search,
// which is why it is cheap and strict.
func LooksLikeSymbol(word string) bool {
	if len(word) < 4 {
		return false
	}
	if strings.ContainsAny(word, "_.") && !strings.HasPrefix(word, ".") {
		return true
	}
	// CamelCase: a lowercase letter followed by an uppercase one.
	for index := 1; index < len(word); index++ {
		previous, current := rune(word[index-1]), rune(word[index])
		if previous >= 'a' && previous <= 'z' && current >= 'A' && current <= 'Z' {
			return true
		}
	}
	return false
}
