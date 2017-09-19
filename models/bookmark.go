package models

import (
	"crypto/sha1"
	"encoding/hex"
	"strings"
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
	Tags        []string  `json:"tags"`
	Alive       bool      `json:"alive"`
	Archived    bool      `json:"archived"`
}

type BookmarkMeta struct {
	Id          int    `json:"id"`
	BookmarkId  string `json:"bookmark"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Keywords    string `json:"keywords"`
	Type        string `json:"type"`
}

func (b *Bookmark) MarshalTags() string {
	return strings.Join(b.Tags, ",")
}

func (b *Bookmark) UnmarshalTags(s string) {
	if len(s) != 0 {
		b.Tags = strings.Split(s, ",")
	} else {
		b.Tags = []string{}
	}
}

func (b *Bookmark) Insert() error {
	statement, err := dbg.Prepare("INSERT OR IGNORE INTO bookmarks (created, updated, verified, title, description, url, hash, tags, alive, archived) VALUES (?,?,?,?,?,?,?,?,?,?)")
	if err != nil {
		return err
	}

	_, err = statement.Exec(b.Created, b.Updated, b.Verified, b.Title, b.Description, b.Url, b.Hash, b.MarshalTags(), b.Alive, b.Archived)
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
	b.SetUpdatedTimestamp()
	b.SetUpdatedTimestamp()

	b.Created = time.Now()
	b.Alive = true
	b.Archived = false

	if b.Tags == nil {
		b.Tags = []string{}
	}
}

func (b *Bookmark) SetUpdatedTimestamp() {
	b.Updated = time.Now()
}

func (b *Bookmark) SetVerifiedTimestamp() {
	b.Updated = time.Now()
}
