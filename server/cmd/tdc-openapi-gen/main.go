// Command tdc-openapi-gen records what each route's handler binds and writes,
// for the OpenAPI document the server publishes. Run through go generate in
// internal/httpapi.
package main

import (
	"fmt"
	"os"

	"github.com/AnoRebel/tdc/server/internal/openapigen"
)

func main() {
	source, err := openapigen.Generate(".")
	if err != nil {
		fmt.Fprintln(os.Stderr, "tdc-openapi-gen:", err)
		os.Exit(1)
	}
	if err := os.WriteFile("openapi_bodies_gen.go", source, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "tdc-openapi-gen:", err)
		os.Exit(1)
	}
}
