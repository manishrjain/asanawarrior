package main

import (
	"flag"
	"fmt"
	"log"

	"github.com/manishrjain/asanawarrior/asana"
	"github.com/manishrjain/asanawarrior/taskwarrior"
)

func main() {
	flag.Parse()
	fmt.Println("vim-go")

	tasks, err := asana.GetTasks(1)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(len(tasks), "results found")
	for _, t := range tasks {
		if err := taskwarrior.AddNew(t); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("%+v\n", t)
	}

	tasks, err = taskwarrior.GetTasks()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(len(tasks), "results found")
	for _, t := range tasks {
		fmt.Printf("%+v\n", t)
	}
}
