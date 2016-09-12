package main

import (
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/manishrjain/asanawarrior/asana"
	"github.com/manishrjain/asanawarrior/taskwarrior"
	"github.com/manishrjain/asanawarrior/x"
	"github.com/pkg/errors"
)

var duration = flag.Int("dur", 1, "How often to run sync, specified in minutes.")

type Match struct {
	Xid    uint64
	Asana  x.WarriorTask
	TaskWr x.WarriorTask
}

// generateMatches matches all tasks from Asana to Taskwarrior, and stores non-matches as
// individual entries from each, without the other being present.
func generateMatches(atasks []x.WarriorTask, twtasks []x.WarriorTask) []*Match {
	amap := make(map[uint64]*Match)
	for _, at := range atasks {
		m := &Match{
			Xid:   at.Xid,
			Asana: at,
		}
		amap[at.Xid] = m
	}

	matches := make([]*Match, 0, 1000)
	// Iterate over task warrior tasks.
	for _, tw := range twtasks {
		if m, ok := amap[tw.Xid]; !ok {
			// Not in Asana.
			match := &Match{
				TaskWr: tw,
			}
			matches = append(matches, match)

		} else {
			// Also in Asana.
			m.TaskWr = tw
			delete(amap, tw.Xid)
			matches = append(matches, m)
		}
	}

	// Left over tasks from Asana not in task warrior.
	for _, m := range amap {
		matches = append(matches, m)
	}
	return matches
}

func approxAfter(t1, t2 time.Time) bool {
	return t1.Sub(t2) > time.Second
}

func syncMatch(m *Match) error {
	if m.Xid == 0 {
		// Task not present in Asana, but present in TW.

		if m.TaskWr.Xid > 0 {
			// This task used to have an Asana ID. But, we can't find the corresponding Asana task.
			// It can happen when Asana task was deleted.
			// If so, delete the task from TW as well.
			return taskwarrior.Delete(m.TaskWr)
		}

		fmt.Printf("Create in Asana: [%q]\n", m.TaskWr.Name)
		final, err := asana.AddNew(m.TaskWr)
		if err != nil {
			return err
		}
		return taskwarrior.OverwriteUuid(final, m.TaskWr.Uuid)
	}

	if m.TaskWr.Xid == 0 {
		fmt.Printf("Create in Taskwarrior: [%q]\n", m.Asana.Name)
		return taskwarrior.AddNew(m.Asana)
	}

	if m.Asana.Xid != m.TaskWr.Xid {
		log.Fatalf("Xids should be matched: %+v\n", m)
	}

	if approxAfter(m.Asana.Modified, m.TaskWr.Modified) {
		fmt.Printf("Overwrite Taskwarrior: [%q] [time diff: %v]\n",
			m.Asana.Name, m.Asana.Modified.Sub(m.TaskWr.Modified))
		return taskwarrior.OverwriteUuid(m.Asana, m.TaskWr.Uuid)
	}

	if approxAfter(m.TaskWr.Modified, m.Asana.Modified) {
		fmt.Printf("Overwrite Asana: [%q] [time diff: %v]\n",
			m.TaskWr.Name, m.TaskWr.Modified.Sub(m.Asana.Modified))

		if err := asana.UpdateTask(m.TaskWr, m.Asana); err != nil {
			return errors.Wrap(err, "syncMatch overwrite asana")
		}
		return nil

		// Now that we have updated Asana, let's get it back and overwrite Taskwarrior.
		// This is important, otherwise, TW modification time always remains greater than Asana.
		// That can happen if we only added or remoted tags. Those don't seem to update
		// Asana modification timestamp.
		// Which causes this section to get triggered on every sync.
		// UPDATE: TW doesn't allow modification ts to jump back. So, this following code is NOOP.
		/*
			wt, err := asana.GetOneTask(m.Xid)
			if err != nil {
				return errors.Wrap(err, "syncMatch GetOneTask")
			}
			return taskwarrior.OverwriteUuid(wt, m.TaskWr.Uuid)
		*/
	}

	// Should be in sync. No checks are being done currently on individual fields.
	return nil
}

func runSync() {
	atasks, err := asana.GetTasks()
	// atasks, err := asana.GetTasks(1)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Asana results found:", len(atasks))

	twtasks, err := taskwarrior.GetTasks()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Taskwarrior results found:", len(twtasks))

	matches := generateMatches(atasks, twtasks)
	for _, m := range matches {
		if err := syncMatch(m); err != nil {
			log.Printf("syncMatch error: %v %+v", err, m)
		}
	}
	fmt.Println("All synced up. DONE.")
}

func main() {
	flag.Parse()
	fmt.Println("Asanawarrior v0.7 - Bringing the power of Taskwarrior to Asana")

	// Initiate a sync right away.
	fmt.Println()
	fmt.Println("Starting sync at", time.Now())
	runSync()

	// And then do it at regular intervals.
	for t := range time.Tick(time.Duration(*duration) * time.Minute) {
		fmt.Println()
		fmt.Println("Starting sync at", t)
		runSync()
	}
}
