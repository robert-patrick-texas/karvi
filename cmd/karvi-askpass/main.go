package main

import (
	"github.com/robert-patrick-texas/karvi/internal/askpass"
	"os"
)

func main() { os.Exit(askpass.HelperMain(os.Args[1:])) }
