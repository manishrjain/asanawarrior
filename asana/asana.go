package asana

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strings"

	"github.com/manishrjain/asanawarrior/x"
)

var token = flag.String("token", "", "Token provided by Asana.")

const (
	prefix = "https://app.asana.com/api/1.0"
)

func runGetter(i interface{}, suffix string, fields ...string) error {
	url := fmt.Sprintf("%s/%s?opt_fields=%s", prefix, suffix, strings.Join(fields, ","))
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Fatal(err)
	}
	req.Header.Add("Authorization", "Bearer "+*token)
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, i); err != nil {
		return err
	}
	return nil
}

type Basic struct {
	Id    uint64 `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}
type BasicData struct {
	Data []Basic `json:"data"`
}

func getVarious(suffix string, opts ...string) ([]Basic, error) {
	var bd BasicData
	if err := runGetter(&bd, suffix, opts...); err != nil {
		return nil, err
	}
	return bd.Data, nil
}

type Task struct {
	Basic
	Assignee  Basic   `json:"assignee"`
	Tags      []Basic `json:"tags"`
	Completed bool
}

type tasks struct {
	Data []Task `json:"data"`
}

func GetTasks() ([]x.WarriorTask, error) {
	projects, err := getVarious("projects")
	if err != nil {
		return nil, err
	}

	alltags, err := getVarious("tags")
	if err != nil {
		return nil, err
	}

	tagmap := make(map[uint64]string)
	for _, t := range alltags {
		tagmap[t.Id] = t.Name
	}

	allusers, err := getVarious("users", "email")
	if err != nil {
		return nil, err
	}
	usermap := make(map[uint64]string)
	for _, u := range allusers {
		email := strings.Split(u.Email, "@")
		usermap[u.Id] = email[0]
	}

	wtasks := make([]x.WarriorTask, 0, 100)
	var section string
	for _, proj := range projects {
		var t tasks
		if err := runGetter(&t, fmt.Sprintf("projects/%d/tasks", proj.Id), "assignee,name,tags,completed"); err != nil {
			return nil, err
		}
		for _, task := range t.Data {
			if strings.HasSuffix(task.Name, ":") {
				section = strings.Map(func(r rune) rune {
					alph := 'A' <= r && r <= 'Z' || 'a' <= r && r <= 'z' || '0' <= r && r <= '9'
					if alph {
						return r
					}
					return -1
				}, task.Name)

				continue
			}
			wt := x.WarriorTask{
				Name:      task.Name,
				Project:   proj.Name,
				Xid:       task.Id,
				Pid:       proj.Id,
				Completed: task.Completed,
				Assignee:  usermap[task.Assignee.Id],
			}
			for _, tag := range task.Tags {
				wt.Tags = append(wt.Tags, tagmap[tag.Id])
			}
			wt.Tags = append(wt.Tags, section)
			wtasks = append(wtasks, wt)
		}
	}
	return wtasks, nil
}
