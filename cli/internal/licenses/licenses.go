// Package licenses embeds the license notices that ship with the CLI binary.
package licenses

import _ "embed"

//go:generate go run gen.go

// Text is filedrop's license followed by those of the Go standard library
// and every dependency linked into the binary
//
//go:embed licenses.txt
var Text string
