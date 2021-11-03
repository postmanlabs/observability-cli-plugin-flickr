module github.com/flickr/sitecode-akita-plugin

go 1.16

require github.com/akitasoftware/akita-cli v0.18.5

require (
	github.com/akitasoftware/akita-ir v0.0.0-20211020161529-944af4d11d6e
	github.com/akitasoftware/akita-libs v0.0.0-20211020162041-fe02207174fb
)

replace github.com/google/martian/v3 v3.0.1 => github.com/akitasoftware/martian/v3 v3.0.1-0.20210608174341-829c1134e9de
