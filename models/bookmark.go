package models

import (
	"time"
)

type Bookmark struct {
	Id          int       `json:"id"`
	Created     time.Time `json:"created"`
	Updated     time.Time `json:"last_updated"`
	Verified    time.Time `json:"last_verified"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Url         string    `json:"url"`
	Hash        string    `json:"hash"`
	Tags        []Tag     `json:"tags"`
	Alive       bool      `json:"alive"`
	Archived    bool      `json:"archived"`
}

func (b *Bookmark) Save() error {
	statement, err := DB.Prepare("INSERT INTO bookmarks (created, updated, verified, title, description, url, hash, alive, archived) VALUES (?,?,?,?,?,?,?,?,?)")
	if err != nil {
		return err
	}

	_, err = statement.Exec(b.Created, b.Updated, b.Verified, b.Title, b.Description, b.Url, b.Hash, b.Alive, b.Archived)
	if err != nil {
		return err
	}

	return nil
}
