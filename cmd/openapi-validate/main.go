package main

import (
	"fmt"
	"os"

	"stellabill-backend/openapi"
)

func main() {
	if _, err := openapi.Load(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

