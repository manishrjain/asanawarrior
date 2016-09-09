package main

import (
	"flag"
	"fmt"

	"github.com/manishrjain/asanawarrior/asana"
)

func main() {
	flag.Parse()
	fmt.Println("vim-go")
	asana.Users()
}
