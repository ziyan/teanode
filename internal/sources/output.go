package sources

import (
	"bufio"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
)

// parsed is what one call answered: the items it listed, and the whole
// answer, which a template reaches as response and paging reads its token
// from.
type parsed struct {
	items    []map[string]any
	response any
}

// parseOutput reads what a command printed or a request answered.
func parseOutput(shape Parsing, output []byte) (*parsed, error) {
	switch shape.Kind {
	case "text":
		return &parsed{items: []map[string]any{{"text": string(output)}}, response: string(output)}, nil
	case "jsonl":
		var items []map[string]any
		scanner := bufio.NewScanner(bytes.NewReader(output))
		scanner.Buffer(make([]byte, 0, 1<<20), 64<<20)
		for scanner.Scan() {
			line := bytes.TrimSpace(scanner.Bytes())
			if len(line) == 0 {
				continue
			}
			value, err := decodeJSON(line)
			if err != nil {
				return nil, fmt.Errorf("a line of what it printed is not JSON: %w", err)
			}
			item, ok := value.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("a line of what it printed is not a JSON object")
			}
			items = append(items, item)
		}
		if err := scanner.Err(); err != nil {
			return nil, err
		}
		return &parsed{items: items}, nil
	case "lines":
		pattern, err := regexp.Compile(shape.Pattern)
		if err != nil {
			return nil, err
		}
		var items []map[string]any
		for _, line := range strings.Split(string(output), "\n") {
			match := pattern.FindStringSubmatch(strings.TrimRight(line, "\r"))
			if match == nil {
				continue
			}
			item := map[string]any{}
			for index, name := range pattern.SubexpNames() {
				if name != "" {
					item[name] = match[index]
				}
			}
			items = append(items, item)
		}
		return &parsed{items: items, response: string(output)}, nil
	case "json":
		value, err := decodeJSON(output)
		if err != nil {
			return nil, fmt.Errorf("what it printed is not JSON: %w", err)
		}
		items, err := itemsAt(value, shape.Items)
		if err != nil {
			return nil, err
		}
		return &parsed{items: items, response: value}, nil
	case "xml":
		value, err := decodeXML(output)
		if err != nil {
			return nil, fmt.Errorf("what it answered is not XML: %w", err)
		}
		items, err := itemsAt(value, shape.Items)
		if err != nil {
			return nil, err
		}
		return &parsed{items: items, response: value}, nil
	}
	return nil, fmt.Errorf("%q is not a way of reading output", shape.Kind)
}

// decodeJSON keeps numbers as they were written, so an identifier or a
// time in milliseconds is not rounded on the way through.
func decodeJSON(data []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}

// itemsAt finds the list of items at a path: dotted keys, "." for the
// whole answer, a path ending in ".*" for the values of a map, and
// alternatives separated by " | ", the first that is there winning. A
// single object where a list was expected is a list of one.
func itemsAt(value any, path string) ([]map[string]any, error) {
	if strings.TrimSpace(path) == "" {
		path = "."
	}
	for _, alternative := range strings.Split(path, "|") {
		found, ok := at(value, strings.TrimSpace(alternative))
		if !ok {
			continue
		}
		return asItems(found), nil
	}
	// Nothing at the path is an empty answer, not a malformed one: a
	// search that matched nothing often leaves the list out.
	return nil, nil
}

func at(value any, path string) (any, bool) {
	if path == "." || path == "" {
		return value, true
	}
	current := value
	for _, name := range strings.Split(strings.TrimPrefix(path, "."), ".") {
		if name == "*" {
			asMap, ok := current.(map[string]any)
			if !ok {
				return current, current != nil
			}
			keys := make([]string, 0, len(asMap))
			for key := range asMap {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			values := make([]any, 0, len(keys))
			for _, key := range keys {
				values = append(values, asMap[key])
			}
			current = values
			continue
		}
		asMap, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		next, present := asMap[name]
		if !present {
			return nil, false
		}
		current = next
	}
	return current, true
}

func asItems(value any) []map[string]any {
	switch typed := value.(type) {
	case []any:
		items := make([]map[string]any, 0, len(typed))
		for _, each := range typed {
			if item, ok := each.(map[string]any); ok {
				items = append(items, item)
			}
		}
		return items
	case map[string]any:
		return []map[string]any{typed}
	}
	return nil
}

// decodeXML turns a document into maps a template can walk: an element
// is its children by name (a list where a name repeats), its attributes
// by name, and its text, which is the element itself where it has
// nothing else and "#text" where it has.
func decodeXML(data []byte) (any, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = false
	decoder.Entity = xml.HTMLEntity
	type frame struct {
		name     string
		children map[string]any
		text     strings.Builder
	}
	root := &frame{children: map[string]any{}}
	stack := []*frame{root}
	for {
		next, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch element := next.(type) {
		case xml.StartElement:
			opened := &frame{name: element.Name.Local, children: map[string]any{}}
			for _, attribute := range element.Attr {
				opened.children[attribute.Name.Local] = attribute.Value
			}
			stack = append(stack, opened)
		case xml.CharData:
			stack[len(stack)-1].text.Write(element)
		case xml.EndElement:
			if len(stack) < 2 {
				continue
			}
			closed := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			var value any
			body := strings.TrimSpace(closed.text.String())
			if len(closed.children) == 0 {
				value = body
			} else {
				if body != "" {
					closed.children["#text"] = body
				}
				value = closed.children
			}
			parent := stack[len(stack)-1].children
			switch existing := parent[closed.name].(type) {
			case nil:
				parent[closed.name] = value
			case []any:
				parent[closed.name] = append(existing, value)
			default:
				parent[closed.name] = []any{existing, value}
			}
		}
	}
	return root.children, nil
}
