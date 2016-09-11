package asana

import (
	"strings"
	"sync"
)

type acache struct {
	sync.RWMutex
	projects []Basic
	tags     []Basic
	users    []Basic
	tagmap   map[uint64]string
	usermap  map[uint64]string
}

func (c *acache) update() error {
	c.Lock()
	defer c.Unlock()

	var err error
	c.projects, err = getVarious("projects")
	if err != nil {
		return err
	}

	c.tags, err = getVarious("tags")
	if err != nil {
		return err
	}
	c.tagmap = make(map[uint64]string)
	for _, t := range c.tags {
		c.tagmap[t.Id] = t.Name
	}

	c.users, err = getVarious("users", "email")
	if err != nil {
		return err
	}
	c.usermap = make(map[uint64]string)
	for _, u := range c.users {
		email := strings.Split(u.Email, "@")
		c.usermap[u.Id] = email[0]
	}
	return nil
}

func (c *acache) Projects() []Basic {
	c.RLock()
	defer c.RUnlock()
	projects := make([]Basic, len(c.projects))
	copy(projects, c.projects)
	return projects
}

func (c *acache) User(uid uint64) string {
	c.RLock()
	defer c.RUnlock()
	return c.usermap[uid]
}

func (c *acache) Tag(uid uint64) string {
	c.RLock()
	defer c.RUnlock()
	return c.tagmap[uid]
}
