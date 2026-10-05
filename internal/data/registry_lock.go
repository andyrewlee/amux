package data

import (
	"os"

	"github.com/andyrewlee/amux/internal/fsatomic"
)

// lockRegistryFile/unlockRegistryFile keep the registry's historical names
// for the shared sibling-.lock primitive now living in internal/fsatomic so
// process and config can take the same transaction lock.
func lockRegistryFile(lockPath string, shared bool) (*os.File, error) {
	return fsatomic.LockFile(lockPath, shared)
}

func unlockRegistryFile(file *os.File) {
	fsatomic.UnlockFile(file)
}
