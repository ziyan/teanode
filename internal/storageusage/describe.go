package storageusage

import "fmt"

// DescribeBytes writes a size the way an operator reads a disk: 1024-based
// units, one decimal from a megabyte up.
func DescribeBytes(sizeBytes int64) string {
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	size := float64(sizeBytes)
	index := 0
	for size >= 1024 && index < len(units)-1 {
		size /= 1024
		index++
	}
	if index < 2 {
		return fmt.Sprintf("%.0f %s", size, units[index])
	}
	return fmt.Sprintf("%.1f %s", size, units[index])
}
