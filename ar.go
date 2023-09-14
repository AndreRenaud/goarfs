package goarfs

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"strconv"
	"strings"
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

type ARFSRaw interface {
	io.Reader
	io.ReaderAt
	io.Seeker
}

type ARFS struct {
	rawFile ARFSRaw

	fileHeaders map[string]*fileHeader
}

// Make sure we implement all the various fs.FS interfaces
var _ fs.FS = (*ARFS)(nil)
var _ fs.ReadDirFS = (*ARFS)(nil)
var _ fs.ReadFileFS = (*ARFS)(nil)
var _ fs.StatFS = (*ARFS)(nil)

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

// FromFile loads an AR file from the operating system filesystem and returns
// the fs.FS compatible interface from it. It will return an error if the AR file
// is corrupt/invalid.
func FromFile(filename string) (*ARFS, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	a := &ARFS{rawFile: f}
	if err := a.parse(); err != nil {
		f.Close()
		return nil, err
	}
	return a, nil
}

func FromInterface(raw ARFSRaw) (*ARFS, error) {
	a := &ARFS{rawFile: raw}
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

		offset, err := a.rawFile.Seek(0, io.SeekCurrent)
		if err != nil {
			return err
		}

		sectionReader := io.NewSectionReader(a.rawFile, offset, size)

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

		// file entries are aligned to two-byte offsets
		size += size & 1
		if _, err := a.rawFile.Seek(size, io.SeekCurrent); err != nil {
			return err
		}
	}

}

func (a *ARFS) Close() error {
	// If our input is closable, then do that
	if closer, ok := a.rawFile.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

func (a *ARFS) Open(name string) (fs.File, error) {
	header, ok := a.fileHeaders[name]
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

func (a *ARFS) ReadFile(name string) ([]byte, error) {
	f, err := a.Open(name)
	if err != nil {
		return nil, err
	}
	return io.ReadAll(f)
}

func (a *ARFS) Stat(name string) (fs.FileInfo, error) {
	fh, ok := a.fileHeaders[name]
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
	return nil
}

func (fh *fileHeader) Type() fs.FileMode {
	return fs.FileMode(fh.mode)
}

func (fh *fileHeader) Info() (fs.FileInfo, error) {
	return fh, nil
}
