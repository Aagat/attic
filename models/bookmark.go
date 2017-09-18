package models

import (
	"crypto/sha1"
	"encoding/hex"
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

func (b *Bookmark) Insert() error {
	statement, err := dbg.Prepare("INSERT OR IGNORE INTO bookmarks (created, updated, verified, title, description, url, hash, alive, archived) VALUES (?,?,?,?,?,?,?,?,?)")
	if err != nil {
		return err
	}

	_, err = statement.Exec(b.Created, b.Updated, b.Verified, b.Title, b.Description, b.Url, b.Hash, b.Alive, b.Archived)
	if err != nil {
		return err
	}

	return nil
}

func (b *Bookmark) CalculateHash() {
	hash := sha1.New()
	hash.Write([]byte(b.Url))
	b.Hash = hex.EncodeToString(hash.Sum(nil))
}

func (b *Bookmark) FillMissing() {

	b.CalculateHash()

	b.Created = time.Now()
	b.Updated = time.Now()
	b.Verified = time.Now()
	b.Alive = true
	b.Archived = false
}
