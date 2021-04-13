package main

import (
	"fmt"
	"os"

	"github.com/pantherman594/ynab-tracker/cmd"
)

func main() {
  err := cmd.Execute()
  if err != nil {
    fmt.Fprintln(os.Stderr, err);
    os.Exit(1)
  }
}
