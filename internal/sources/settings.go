package sources

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// CheckSettings checks what a person filled in for a source of this type
// and answers it in the shape the runner reads: a list as a list, a number
// as a number, a flag as a flag. A setting the type does not declare is
// refused, since a misspelt one would otherwise be quietly ignored, and so
// is one left out that has no default, which the type cannot run without.
func (self *Type) CheckSettings(values map[string]any) (map[string]any, error) {
	declared := map[string]Setting{}
	for _, setting := range self.Settings {
		declared[setting.Name] = setting
	}
	for name := range values {
		if _, ok := declared[name]; !ok {
			return nil, fmt.Errorf("%s has no setting called %q", self.Name, name)
		}
	}
	checked := map[string]any{}
	for _, setting := range self.Settings {
		value, given := values[setting.Name]
		if !given || value == nil {
			if setting.Default == nil {
				return nil, fmt.Errorf("%s needs the setting %s: %s", self.Name, setting.Name, setting.Description)
			}
			continue
		}
		normalized, err := setting.check(value)
		if err != nil {
			return nil, fmt.Errorf("the setting %s: %w", setting.Name, err)
		}
		checked[setting.Name] = normalized
	}
	return checked, nil
}

func (self Setting) check(value any) (any, error) {
	switch self.Type {
	case "string", "path":
		text, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("is text")
		}
		if err := matches(self.Pattern, text); err != nil {
			return nil, err
		}
		return text, nil
	case "boolean":
		switch typed := value.(type) {
		case bool:
			return typed, nil
		case string:
			parsed, err := strconv.ParseBool(strings.TrimSpace(typed))
			if err != nil {
				return nil, fmt.Errorf("is true or false")
			}
			return parsed, nil
		}
		return nil, fmt.Errorf("is true or false")
	case "integer":
		var number float64
		switch typed := value.(type) {
		case float64:
			number = typed
		case int:
			number = float64(typed)
		case json.Number:
			parsed, err := typed.Float64()
			if err != nil {
				return nil, fmt.Errorf("is a whole number")
			}
			number = parsed
		case string:
			parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
			if err != nil {
				return nil, fmt.Errorf("is a whole number")
			}
			number = parsed
		default:
			return nil, fmt.Errorf("is a whole number")
		}
		if number != math.Trunc(number) {
			return nil, fmt.Errorf("is a whole number")
		}
		if self.Minimum != nil && number < float64(*self.Minimum) {
			return nil, fmt.Errorf("is at least %d", *self.Minimum)
		}
		if self.Maximum != nil && number > float64(*self.Maximum) {
			return nil, fmt.Errorf("is at most %d", *self.Maximum)
		}
		return int(number), nil
	case "array":
		var list []any
		switch typed := value.(type) {
		case []any:
			list = typed
		case []string:
			for _, each := range typed {
				list = append(list, each)
			}
		case string:
			// A list written as one line, comma-separated, as a form or a
			// command line gives it.
			for _, each := range strings.Split(typed, ",") {
				if each = strings.TrimSpace(each); each != "" {
					list = append(list, each)
				}
			}
		default:
			return nil, fmt.Errorf("is a list")
		}
		checked := make([]any, 0, len(list))
		for _, each := range list {
			text, ok := each.(string)
			if !ok {
				return nil, fmt.Errorf("is a list of text")
			}
			if err := matches(itemPattern(self), text); err != nil {
				return nil, err
			}
			checked = append(checked, text)
		}
		return checked, nil
	}
	return nil, fmt.Errorf("has the type %q", self.Type)
}

func matches(pattern, text string) error {
	if pattern == "" {
		return nil
	}
	compiled, err := regexp.Compile(pattern)
	if err != nil {
		return err
	}
	if !compiled.MatchString(text) {
		return fmt.Errorf("%q does not match %s", text, pattern)
	}
	return nil
}
