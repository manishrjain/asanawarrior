package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/0xAX/notificator"
	"github.com/boltdb/bolt"
	"github.com/manishrjain/asanawarrior/asana"
	"github.com/manishrjain/asanawarrior/taskwarrior"
	"github.com/manishrjain/asanawarrior/x"
	"github.com/pkg/errors"
)

var duration = flag.Int("dur", 1, "How often to run sync, specified in minutes.")
var dbpath = flag.String("db", os.Getenv("HOME")+"/.task/asanawarrior.db",
	"File path for db which stores certain sync information.")
var notifyInterval = flag.Int("interval", 10,
	"Minimum duration in seconds between successive notifications. Set to zero for no notifications.")

var db *bolt.DB
var bucketName = []byte("aw")
var notify *notificator.Notificator

type Match struct {
	Xid    uint64
	Asana  x.WarriorTask
	TaskWr x.WarriorTask
}

type notification struct {
	Title string
	Text  string
}

var notifications = make(chan notification, 100)

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

func asanaKey(xid uint64) []byte {
	return []byte(fmt.Sprintf("asana-%d", xid))
}

func taskwKey(uuid string) []byte {
	return []byte(fmt.Sprintf("taskw-%s", uuid))
}

func storeInDb(asanaTask, twTask x.WarriorTask) {
	if err := db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketName)
		if err := b.Put(asanaKey(asanaTask.Xid), []byte(asanaTask.Modified.Format(time.RFC3339))); err != nil {
			return err
		}
		if err := b.Put(taskwKey(twTask.Uuid), []byte(twTask.Modified.Format(time.RFC3339))); err != nil {
			return err
		}
		return nil

	}); err != nil {
		log.Fatalf("Write to db failed with error: %v", err)
	}
}

func deleteFromDb(twTask x.WarriorTask) {
	if err := db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketName)
		if err := b.Delete(asanaKey(twTask.Xid)); err != nil {
			return err
		}
		if err := b.Delete(taskwKey(twTask.Uuid)); err != nil {
			return err
		}
		return nil
	}); err != nil {
		log.Fatalf("Delete from db failed with error: %v", err)
	}
}

func getSyncTimestamps(xid uint64, uuid string) (time.Time, time.Time) {
	var at, tt time.Time
	db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketName)
		ats := string(b.Get(asanaKey(xid)))
		tts := string(b.Get(taskwKey(uuid)))
		var err error
		if at, err = time.Parse(time.RFC3339, ats); err != nil {
			log.Fatalf("Unable to find asana ts: %v %v", xid, uuid)
		}
		if tt, err = time.Parse(time.RFC3339, tts); err != nil {
			log.Fatalf("Unable to find taskwarrior ts: %v %v", xid, uuid)
		}
		return nil
	})
	return at, tt
}

func syncMatch(m *Match) error {
	if m.Xid == 0 {
		// Task not present in Asana, but present in TW.

		if m.TaskWr.Xid > 0 {
			if m.TaskWr.Deleted {
				// Already deleted from TW. Do nothing.
				return nil
			}

			// This task used to have an Asana ID. But, we can't find the corresponding Asana task.
			// It can happen when Asana task was deleted.
			// If so, delete the task from TW as well.
			fmt.Printf("Delete from Taskwarrior: [%q]\n", m.TaskWr.Name)
			pushNotification("Delete", m.TaskWr.Name)

			if err := taskwarrior.Delete(m.TaskWr); err != nil {
				return errors.Wrap(err, "Delete from Taskwarrior")
			}
			deleteFromDb(m.TaskWr)
			return nil
		}

		// Create in Asana.
		fmt.Printf("Create in Asana: [%q]\n", m.TaskWr.Name)
		asanaUpdated, err := asana.AddNew(m.TaskWr)
		if err != nil {
			return errors.Wrap(err, "create asana addnew")
		}

		// Update TW with the Xid.
		if err := taskwarrior.OverwriteUuid(asanaUpdated, m.TaskWr.Uuid); err != nil {
			return errors.Wrap(err, "create asana overwriteuuid")
		}
		taskwUpdated, err := taskwarrior.GetTask(m.TaskWr.Uuid)
		if err != nil {
			return errors.Wrap(err, "create asana GetTask")
		}

		// Store Asana and Taskwarrior timestamps as of this sync.
		storeInDb(asanaUpdated, taskwUpdated)
		return nil
	}

	if m.TaskWr.Xid == 0 {
		// No Asana xid found in Taskwarrior. So, create it.

		fmt.Printf("Create in Taskwarrior: [%q]\n", m.Asana.Name)
		pushNotification("Create", m.Asana.Name)
		uuid, err := taskwarrior.AddNew(m.Asana)
		if err != nil {
			return errors.Wrap(err, "syncMatch create in taskwarrior")
		}
		if len(uuid) == 0 {
			log.Fatalf("Unable to parse UUID of new task: %+v", m.Asana)
		}
		updated, err := taskwarrior.GetTask(uuid)
		if err != nil {
			return err
		}

		// Store Asana and Taskwarrior timestamps as of this sync.
		storeInDb(m.Asana, updated)
		return nil
	}

	if m.Asana.Xid != m.TaskWr.Xid {
		log.Fatalf("Xids should be matched: %+v\n", m)
	}

	// Task is present in both Asana and TW.
	if m.TaskWr.Deleted {
		fmt.Printf("Deleting task from Asana: [%q]\n", m.TaskWr.Name)
		pushNotification("Deleting from Asana", m.TaskWr.Name)
		if err := asana.Delete(m.Xid); err != nil {
			return errors.Wrap(err, "Delete task from Asana")
		}
		deleteFromDb(m.TaskWr)
		return nil
	}

	asanaTs, taskwTs := getSyncTimestamps(m.Asana.Xid, m.TaskWr.Uuid)
	if approxAfter(m.Asana.Modified, asanaTs) {
		// Asana was updated. Overwrite TW.
		fmt.Printf("Overwrite Taskwarrior: [%q] [time diff: %v]\n",
			m.Asana.Name, m.Asana.Modified.Sub(asanaTs))
		pushNotification("Update", m.Asana.Name)

		if err := taskwarrior.OverwriteUuid(m.Asana, m.TaskWr.Uuid); err != nil {
			return errors.Wrap(err, "Overwrite Taskwarrior")
		}
		updated, err := taskwarrior.GetTask(m.TaskWr.Uuid)
		if err != nil {
			return errors.Wrap(err, "Overwrite Taskwarrior GetTask")
		}

		storeInDb(m.Asana, updated)
		return nil
	}

	if approxAfter(m.TaskWr.Modified, taskwTs) {
		// TW was updated. Overwrite Asana.
		fmt.Printf("Overwrite Asana: [%q] [time diff: %v]\n",
			m.TaskWr.Name, m.TaskWr.Modified.Sub(taskwTs))

		if err := asana.UpdateTask(m.TaskWr, m.Asana); err != nil {
			return errors.Wrap(err, "syncMatch overwrite asana")
		}
		updated, err := asana.GetOneTask(m.Xid)
		if err != nil {
			return errors.Wrap(err, "syncMatch GetOneTask")
		}
		storeInDb(updated, m.TaskWr)
		return nil
	}
	return nil
}

func runSync() {
	atasks, err := asana.GetTasks()
	// atasks, err := asana.GetTasks(1)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%27s: %d active\n", "Asana results found", len(atasks))

	twtasks, err := taskwarrior.GetTasks()
	if err != nil {
		log.Fatal(err)
	}

	deleted := 0
	for _, t := range twtasks {
		if t.Deleted {
			deleted++
		}
	}
	fmt.Printf("%27s: %d active, %d deleted\n",
		"Taskwarrior results found", len(twtasks)-deleted, deleted)

	matches := generateMatches(atasks, twtasks)
	for _, m := range matches {
		if err := syncMatch(m); err != nil {
			log.Printf("syncMatch error: %v %+v", err, m)
		}
	}
	fmt.Println("All synced up. DONE.")
}

func pushNotification(title, text string) {
	if notify == nil {
		return
	}
	n := notification{Title: title, Text: text}

	select {
	case notifications <- n:
	default:
		// Let it go.
	}

}

func processNotifications() {
	ni := time.Duration(*notifyInterval)
	ticker := time.NewTicker(ni * time.Second)
	l := make([]notification, 0, 10)
	for {
		select {
		case <-ticker.C:
			if len(l) == 0 {
				// pass

			} else if len(l) == 1 {
				n := l[0]
				notify.Push("Asanawarrior "+n.Title, n.Text, "", notificator.UR_NORMAL)

			} else {
				notify.Push("Asanawarrior "+l[0].Title,
					fmt.Sprintf("%q and %d more updates", l[0].Text, len(l)-1), "", notificator.UR_NORMAL)
			}
			l = l[:0]

		case n := <-notifications:
			if *notifyInterval > 0 {
				l = append(l, n)
			}
		}
	}
}

func main() {
	flag.Parse()
	fmt.Println("Asanawarrior v0.7 - Bringing the power of Taskwarrior to Asana")
	notify = notificator.New(notificator.Options{
		AppName: "Asanawarrior",
	})
	go processNotifications()

	var err error
	db, err = bolt.Open(*dbpath, 0600, nil)
	if err != nil {
		log.Fatalf("Unable to open bolt db at %v. Error: %v", *dbpath, err)
	}
	defer db.Close()
	db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(bucketName)
		if err != nil {
			log.Fatalf("Unable to create bucket in bolt db.")
		}
		return nil
	})

	// Initiate a sync right away.
	fmt.Println()
	fmt.Println("Starting sync at", time.Now())
	runSync()

	// And then do it at regular intervals.
	ticker := time.NewTicker(time.Duration(*duration) * time.Minute)
	for t := range ticker.C {
		fmt.Println()
		fmt.Println("Starting sync at", t)
		runSync()
	}
}
