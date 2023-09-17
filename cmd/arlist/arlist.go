package main

import (
	"flag"
	"fmt"
	"log"
	"syscall"

	"github.com/AndreRenaud/goarfs"
)

func main() {
	arfile := flag.String("arfile", "", "AR file to list")
	filename := flag.String("filename", "", "File in archive to dump info about")

	flag.Parse()

	ar, err := goarfs.FromFile(*arfile)
	if err != nil {
		log.Fatalf("fromfile: %s", err)
	}
	defer ar.Close()

	files, err := ar.ReadDir("/")
	if err != nil {
		log.Fatalf("readdir: %s", err)
	}
	fmt.Printf("AR File %q contains %d files\n", *arfile, len(files))
	for _, f := range files {
		info, err := f.Info()
		if err != nil {
			log.Fatalf("info on %s: %s", f.Name(), err)
		}

		sys := info.Sys()
		var uid uint32
		var gid uint32
		if stat, ok := sys.(*syscall.Stat_t); ok {
			uid = stat.Uid
			gid = stat.Gid
		}

		fmt.Printf("%s %3d %3d %8d %s %s\n", info.Mode(), uid, gid, info.Size(), info.ModTime(), f.Name())
	}

	if *filename != "" {
		data, err := ar.ReadFile(*filename)
		if err != nil {
			log.Fatalf("open %q: %s", *filename, err)
		}
		fmt.Print(string(data))
	}
}
