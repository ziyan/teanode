package storageusage

import "github.com/ziyan/teanode/internal/config"

// The labels of the disks, as keys: the interface names them in the
// operator's language.
const (
	DiskDataDirectory = "dataDirectory"
	DiskFileDirectory = "fileDirectory"
	DiskObjectStore   = "objectStore"
)

// Disk is where the server's own files are, measured when the server can.
type Disk struct {
	DiskLabel string

	// IsMeasured is false for storage the server cannot ask how full it is,
	// such as an object store, and for a platform that cannot tell. The
	// sizes are then zero and mean nothing.
	IsMeasured     bool
	DiskTotalBytes int64
	DiskFreeBytes  int64
}

// DiskUsedBytes is what the disk holds, whoever wrote it.
func (self Disk) DiskUsedBytes() int64 {
	return max(0, self.DiskTotalBytes-self.DiskFreeBytes)
}

// Disks measures the file systems the server writes to: its data
// directory, the directory the blob store keeps files in when that is on
// another file system, and says that an object store is not measured. On a
// small server the data directory, PostgreSQL's data and the object store's
// are one disk, so one measure covers them; a database on another machine
// is not measured at all.
func Disks(configuration *config.Configuration) []Disk {
	var disks []Disk
	seen := map[string]bool{}
	measure := func(label, directory string) {
		if directory == "" {
			return
		}
		totalBytes, freeBytes, identity, isMeasured := measureDirectory(directory)
		if !isMeasured || seen[identity] {
			if !isMeasured {
				disks = append(disks, Disk{DiskLabel: label})
			}
			return
		}
		seen[identity] = true
		disks = append(disks, Disk{DiskLabel: label, IsMeasured: true, DiskTotalBytes: totalBytes, DiskFreeBytes: freeBytes})
	}
	measure(DiskDataDirectory, configuration.DataDirectory())
	if directory := FileDirectory(configuration); directory != "" {
		measure(DiskFileDirectory, directory)
	}
	if IsObjectStoreOnly(configuration) {
		disks = append(disks, Disk{DiskLabel: DiskObjectStore})
	}
	return disks
}

// FileDirectory is the directory the blob store keeps its files in, or empty
// when it keeps none on this machine.
func FileDirectory(configuration *config.Configuration) string {
	if configuration.Storage.Directory == "" {
		return ""
	}
	return configuration.Path(configuration.Storage.Directory)
}

// IsObjectStoreOnly is whether the files are in an object store and none are
// kept on this machine, so their growth does not fill any disk the server
// can measure.
func IsObjectStoreOnly(configuration *config.Configuration) bool {
	return configuration.Storage.S3.Enabled && configuration.Storage.Directory == ""
}
