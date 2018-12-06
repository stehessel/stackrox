package zip

import (
	"io/ioutil"
)

// FileFlags store metadata about a file in a zip archive.
type FileFlags uint32

const (
	// Executable indicates that the file should be marked executable.
	Executable FileFlags = 1 << iota
	// Sensitive indicates that the file contains sensitive information and thus should not be world-readable.
	Sensitive
)

// File represents a file entry in a Zip archive.
type File struct {
	Name    string
	Content []byte
	Flags   FileFlags
}

// NewFile returns a File object with the given parameters
func NewFile(name string, content []byte, flags FileFlags) *File {
	return &File{
		Name:    name,
		Content: content,
		Flags:   flags,
	}
}

// NewFromFile creates a zip.File from an existing file.
func NewFromFile(srcFilename, tgtFilename string, flags FileFlags) (*File, error) {
	contents, err := ioutil.ReadFile(srcFilename)
	if err != nil {
		return nil, err
	}
	return NewFile(tgtFilename, contents, flags), nil
}
