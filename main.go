package main

import (
	"flag"
	"fmt"
	"log"

	"github.com/manishrjain/asanawarrior/asana"
)

func main() {
	flag.Parse()
	fmt.Println("vim-go")
	tasks, err := asana.GetTasks()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(len(tasks), "results found")
	for _, t := range tasks {
		fmt.Printf("%v\n", t)
	}
}
