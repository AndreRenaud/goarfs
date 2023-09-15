# Go AR FS

[![Go Reference](https://pkg.go.dev/badge/github.com/AndreRenaud/goarfs.svg)](https://pkg.go.dev/github.com/AndreRenaud/goarfs)
![workflow badge](https://github.com/AndreRenaud/goarfs/actions/workflows/go.yaml/badge.svg)

![goarfs logo](goarfs.png)

goarfs implements the fs.FS interface for [AR archive](https://en.wikipedia.org/wiki/Ar_(Unix))

It implements the following interfaces:
* [fs.FS](https://pkg.go.dev/io/fs#FS)
* [fs.ReadDirFS](https://pkg.go.dev/io/fs#ReadDirFS)
* [fs.ReadFileFS](https://pkg.go.dev/io/fs#ReadFileFS)
* [fs.StatFS](https://pkg.go.dev/io/fs#StatFS)

## Example usage:

```go
arfs, err := goarfs.FromFile("myfile.ar")
if err != nil {
    panic(err)
}
defer arfs.Close()
data, err := arfs.ReadFile("internalfilename.txt")
if err != nil {
    panic(err)
}
fmt.Printf("Got data %s", data)
```