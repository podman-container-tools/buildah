package main

import (
	"bytes"
	"compress/gzip"
	"debug/elf"
	"io"
	"log"
	"os"
	"strings"
)

func main() {
	for _, arg := range os.Args[1:] {
		compressed, err := os.ReadFile(arg)
		if err != nil {
			log.Fatal("reading: ", err)
		}
		var reader io.Reader
		reader, err = gzip.NewReader(bytes.NewReader(compressed))
		if err != nil {
			reader = bytes.NewReader(compressed)
		}
		contents, err := io.ReadAll(reader)
		if err != nil {
			log.Fatal("decompressing: ", err, " in ", arg)
		}
		elfFile, err := elf.NewFile(bytes.NewReader(contents))
		if err != nil {
			log.Fatal("parsing: ", err, " in ", arg)
		}
		for _, section := range elfFile.Sections {
			if strings.Contains(section.Name, ".go") || strings.Contains(section.Name, "gopclntab") {
				log.Fatal("found section ", section.Name, " in ", arg, ", appears to have been unintentionally cross-rebuilt with Go")
			}
		}
		log.Print(arg, ": OK - appears to have been built using native assembler")
	}
}
