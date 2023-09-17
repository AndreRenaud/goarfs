package goarfs

import (
	"io"
	"testing"
)

func TestARFile(t *testing.T) {
	ar, err := FromFile("testdata/test1.ar")
	if err != nil {
		t.Fatal(err)
	}
	defer ar.Close()
	files, err := ar.ReadDir("/")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("foo.ar should only have two files, has %d", len(files))
	}

	f, err := ar.Open("test1.dat")
	if err != nil {
		t.Fatalf("cannot open test1.dat: %s", err)
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("cannot read from test1.dat: %s", err)
	}
	if len(data) != 26 {
		t.Fatalf("test1.dat should contain 26 bytes, contains %d", len(data))
	}

	s, err := ar.Stat("/test2.dat")
	if err != nil {
		t.Fatalf("cannot stat test2.dat: %s", err)
	}
	if s.Size() != 3 {
		t.Fatalf("test2.dat is not 3 bytes long: %d", s.Size())
	}
	if s.Name() != "test2.dat" {
		t.Fatalf("test2.dat is named incorrectly: %s", s.Name())
	}

	fileList, err := ar.Glob("*1.dat")
	if err != nil {
		t.Fatalf("glob: %s", err)
	}
	if len(fileList) != 1 || fileList[0] != "test1.dat" {
		t.Fatalf("glob returned wrong list: %#v", fileList)
	}
}

func TestExtended(t *testing.T) {
	ar, err := FromFile("testdata/extended.ar")
	if err != nil {
		t.Fatal(err)
	}
	defer ar.Close()
	zeros, err := ar.ReadFile("zeros")
	if err != nil {
		t.Fatalf("cannot read zeros: %s", err)
	}
	if len(zeros) != 1024 {
		t.Fatalf("zeros is the wrong size: %d", len(zeros))
	}
	for i, b := range zeros {
		if b != 0 {
			t.Fatalf("zeros has bad at at %d: 0x%x", i, b)
		}
	}
	stat, err := ar.Stat("this_is_a_file_with_a_massive_filename")
	if err != nil {
		t.Fatalf("cannot stat longfile: %s", err)
	}
	if stat.Size() != 127 {
		t.Fatalf("%q has wrong size: %d", stat.Name(), stat.Size())
	}
}
