package strainer

import (
	"os"
	"strconv"
)

func readTestFile(name string) (string, error) {
	content, err := os.ReadFile(name)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

func itoa(value int) string { return strconv.Itoa(value) }
