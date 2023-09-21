// Package goarfs is an implementation of the fs.FS interface
// to make it easy to access data inside AR archives seamlessly as a
// replacement for direct filesystem access.
// This is a convenience to make it easy to ship data files around together as
// as single file, but still access the individual pieces inside.
package goarfs

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	headerSize = 60
)

var (
	goodSignature    = []byte("!<arch>\n") // todo: make it a const
	headerTerminator = []byte{0x60, 0xa}

	ErrTooShort      = errors.New("AR file too short")
	ErrBadSignature  = errors.New("invalid AR signature")
	ErrBadFileHeader = errors.New("bad AR file header")
)

type ARFS struct {
	rawFile arfsReader

	fileHeaders map[string]*fileHeader
}

type arfsReader struct {
	io.ReadSeeker
}

// Make sure we implement all the various fs.FS interfaces
var _ fs.FS = (*ARFS)(nil)
var _ fs.ReadDirFS = (*ARFS)(nil)
var _ fs.ReadFileFS = (*ARFS)(nil)
var _ fs.StatFS = (*ARFS)(nil)
var _ fs.GlobFS = (*ARFS)(nil)

type fileHeader struct {
	name         string
	modification time.Time
	owner        uint32
	group        uint32
	mode         uint32
	size         uint32
	offset       int64

	sectionReader *io.SectionReader
}

// arfsReader
func (a *arfsReader) Close() error {
	// If our input is closable, then do that
	if closer, ok := a.ReadSeeker.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

func (a *arfsReader) ReadAt(p []byte, off int64) (int, error) {
	// If we're already a ReadSeeker, just use that
	if readat, ok := a.ReadSeeker.(io.ReaderAt); ok {
		return readat.ReadAt(p, off)
	}
	// Otherwise fake it using Seek & Read
	if _, err := a.Seek(off, io.SeekStart); err != nil {
		return 0, err
	}
	return a.Read(p)
}

// FromFile loads an AR file from the operating system filesystem and returns
// the fs.FS compatible interface from it. It will return an error if the AR file
// is corrupt/invalid.
func FromFile(filename string) (*ARFS, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	a := &ARFS{rawFile: arfsReader{f}}
	if err := a.parse(); err != nil {
		f.Close()
		return nil, err
	}
	return a, nil
}

func FromInterface(raw io.ReadSeeker) (*ARFS, error) {
	a := &ARFS{rawFile: arfsReader{raw}}
	if err := a.parse(); err != nil {
		return nil, err
	}
	return a, nil
}

func (a *ARFS) parse() error {
	a.fileHeaders = map[string]*fileHeader{}
	if _, err := a.rawFile.Seek(0, io.SeekStart); err != nil {
		return err
	}

	var signature [8]byte
	n, err := a.rawFile.Read(signature[:])
	if err != nil {
		return err
	}
	if n != len(signature[:]) {
		return ErrTooShort
	}

	if !bytes.Equal(signature[:], goodSignature) {
		return ErrBadSignature
	}

	for {
		var header [headerSize]byte

		n, err := a.rawFile.Read(header[:])
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if n != headerSize {
			return ErrTooShort
		}

		filename := strings.TrimSpace(string(header[0:16]))
		modStr := strings.TrimSpace(string(header[16:28]))
		ownerStr := strings.TrimSpace(string(header[28:34]))
		groupStr := strings.TrimSpace(string(header[34:40]))
		modeStr := strings.TrimSpace(string(header[40:48]))
		sizeStr := strings.TrimSpace(string(header[48:58]))
		terminator := header[58:60]

		if !bytes.Equal(terminator, headerTerminator) {
			return ErrBadFileHeader
		}

		modification, err := strconv.ParseInt(modStr, 10, 32)
		if err != nil {
			return errors.Join(ErrBadFileHeader, err)
		}
		owner, err := strconv.ParseInt(ownerStr, 10, 32)
		if err != nil {
			return errors.Join(ErrBadFileHeader, err)
		}
		group, err := strconv.ParseInt(groupStr, 10, 32)
		if err != nil {
			return errors.Join(ErrBadFileHeader, err)
		}
		mode, err := strconv.ParseInt(modeStr, 8, 32)
		if err != nil {
			return errors.Join(ErrBadFileHeader, err)
		}
		size, err := strconv.ParseInt(sizeStr, 10, 32)
		if err != nil {
			return errors.Join(ErrBadFileHeader, err)
		}
		// file entries are aligned to two-byte offsets
		nextPos := size + size&1

		offset, err := a.rawFile.Seek(0, io.SeekCurrent)
		if err != nil {
			return err
		}

		sectionReader := io.NewSectionReader(&a.rawFile, offset, size)

		// If it's an 'extended' entry, then adjust things slightly
		// extended entries have a name of the format '#n/m' where n is
		// incrementing from 1, and m is the number of bytes in the filename
		// that we will pull out of the data itself.
		if strings.HasPrefix(filename, "#1/") {
			length, err := strconv.ParseInt(strings.TrimPrefix(filename, "#1/"), 10, 32)
			if err != nil {
				return err
			}
			filenameData := make([]byte, length)
			n, err := sectionReader.Read(filenameData)
			if err != nil {
				return err
			}
			if n != int(length) {
				return fmt.Errorf("insufficient data for extended filename: %d vs %d", n, length)
			}

			size -= length
			sectionReader = io.NewSectionReader(&a.rawFile, offset+length, size)
			filename = strings.TrimRight(string(filenameData), "\x00")
		}

		a.fileHeaders[filename] = &fileHeader{
			name:          filename,
			modification:  time.Unix(modification, 0),
			owner:         uint32(owner),
			group:         uint32(group),
			mode:          uint32(mode),
			size:          uint32(size),
			offset:        offset,
			sectionReader: sectionReader,
		}

		if _, err := a.rawFile.Seek(nextPos, io.SeekCurrent); err != nil {
			return err
		}
	}
}

func (a *ARFS) Close() error {
	return a.rawFile.Close()
}

func (a *ARFS) getHeader(name string) (*fileHeader, bool) {
	// normalize the name
	name = path.Clean(name)
	name = strings.TrimPrefix(name, "/")
	name = strings.TrimPrefix(name, "./")

	header, ok := a.fileHeaders[name]
	return header, ok
}

func (a *ARFS) Open(name string) (fs.File, error) {
	header, ok := a.getHeader(name)
	if !ok {
		return nil, fs.ErrNotExist
	}

	return header, nil
}

func (a *ARFS) ReadDir(name string) ([]fs.DirEntry, error) {
	// ar archives don't have subfolders
	if name != "/" && name != "." {
		return nil, fs.ErrNotExist
	}
	var ret []fs.DirEntry
	for _, f := range a.fileHeaders {
		ret = append(ret, f)
	}

	return ret, nil
}

func (a *ARFS) Glob(pattern string) ([]string, error) {
	var fileList []string
	for name := range a.fileHeaders {
		match, err := filepath.Match(pattern, name)
		if err != nil {
			return nil, err
		}
		if match {
			fileList = append(fileList, name)
		}
	}
	return fileList, nil
}

func (a *ARFS) ReadFile(name string) ([]byte, error) {
	f, err := a.Open(name)
	if err != nil {
		return nil, err
	}
	return io.ReadAll(f)
}

func (a *ARFS) Stat(name string) (fs.FileInfo, error) {
	fh, ok := a.getHeader(name)
	if !ok {
		return nil, fs.ErrNotExist
	}
	return fh, nil
}

func (fh *fileHeader) Stat() (fs.FileInfo, error) {
	return fh, nil
}

func (fh *fileHeader) Read(data []byte) (int, error) {
	return fh.sectionReader.Read(data)
}

func (fh *fileHeader) Close() error {
	return nil
}

func (fh *fileHeader) ReadAt(p []byte, off int64) (n int, err error) {
	return fh.sectionReader.ReadAt(p, off)
}

func (fh *fileHeader) Seek(offset int64, whence int) (int64, error) {
	return fh.sectionReader.Seek(offset, whence)
}

func (fh *fileHeader) Name() string {
	return fh.name
}

func (fh *fileHeader) Size() int64 {
	return int64(fh.size)
}

func (fh *fileHeader) Mode() fs.FileMode {
	return fs.FileMode(fh.mode)
}

func (fh *fileHeader) ModTime() time.Time {
	return fh.modification
}

func (fh *fileHeader) IsDir() bool {
	return false
}

func (fh *fileHeader) Sys() any {
	// As per os.File.Sys() on Unix
	return &syscall.Stat_t{
		Uid: fh.owner,
		Gid: fh.group,
		// We don't fill in mode, because it has different types on BSD vs. Linux, and it's accessible
		// via .Mode() anyway
		// Mode: uint16(fh.mode),
		Size: int64(fh.size),
	}
}

func (fh *fileHeader) Type() fs.FileMode {
	return fs.FileMode(fh.mode)
}

func (fh *fileHeader) Info() (fs.FileInfo, error) {
	return fh, nil
}
