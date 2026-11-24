package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	doclibrary "doc-library/pkg/doclibrary"
)

// Pass a real documentation directory as the first argument.
// This is a Go caller example, not an alternate retrieval implementation.
func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: go run ./examples/go-client DIRECTORY WRITABLE_CACHE_DIRECTORY")
		os.Exit(2)
	}
	library, err := doclibrary.New(doclibrary.Config{
		Sources:  []doclibrary.Source{{ID: "manuals", Root: os.Args[1]}},
		CacheDir: os.Args[2],
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	response, err := library.Execute(context.Background(), doclibrary.Request{
		Mode: "search", Query: "LRSheetCompose", Limit: 3, MaxChars: 4000,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err := json.NewEncoder(os.Stdout).Encode(response); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}
