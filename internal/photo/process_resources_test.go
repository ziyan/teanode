package photo

import (
	"strings"
	"testing"
)

// The room a copy of a file needs is twice its size and a reserve, after the
// room promised to copies being made already, and is given back when the copy
// is done with.
func TestAWorkSpacePromiseCountsTwiceTheSizeAReserveAndCopiesInFlight(test *testing.T) {
	var space workSpace
	freeBytes := int64(3 << 30)
	free := func(string) (int64, bool) { return freeBytes, true }
	const copyBytes = int64(600 << 20)

	first, err := space.promise(free, "work", copyBytes)
	if err != nil {
		test.Fatalf("a copy that fits was refused: %s", err)
	}
	if space.promisedBytes != 2*copyBytes {
		test.Fatalf("promised %d, want %d", space.promisedBytes, 2*copyBytes)
	}
	// 3 GiB free less 1.2 GiB promised leaves 1.8 GiB, which is not the 1.2 GiB
	// a second copy needs and the 1 GiB reserve besides.
	if _, err := space.promise(free, "work", copyBytes); err == nil || !strings.Contains(err.Error(), "promised") {
		test.Fatalf("a second copy that does not fit beside the first was accepted: %v", err)
	}
	if space.promisedBytes != 2*copyBytes {
		test.Fatalf("a refused promise changed what is promised: %d", space.promisedBytes)
	}
	first()
	first()
	if space.promisedBytes != 0 {
		test.Fatalf("%d bytes are still promised after the copy was done with, twice", space.promisedBytes)
	}
	second, err := space.promise(free, "work", copyBytes)
	if err != nil {
		test.Fatalf("a copy was refused once the first was done with: %s", err)
	}
	second()

	// A file that is the whole disk, or twice a disk's free room, is refused
	// even when nothing else is going on; one that would fit twice over only
	// without the reserve is too.
	for _, bytes := range []int64{freeBytes, freeBytes / 2, (freeBytes-workDirectoryReserveBytes)/2 + 1} {
		if _, err := space.promise(free, "work", bytes); err == nil {
			test.Fatalf("a file of %d bytes was accepted with %d free", bytes, freeBytes)
		}
	}

	// A platform that cannot say how much room there is refuses nothing.
	unknown := func(string) (int64, bool) { return 0, false }
	release, err := space.promise(unknown, "work", 1<<40)
	if err != nil {
		test.Fatal(err)
	}
	release()
	if space.promisedBytes != 0 {
		test.Fatalf("%d", space.promisedBytes)
	}
	// And a file that takes exactly what is left, less the reserve, is accepted.
	exact, err := space.promise(free, "work", (freeBytes-workDirectoryReserveBytes)/2)
	if err != nil {
		test.Fatalf("a file that takes exactly the room there is: %s", err)
	}
	exact()
}
