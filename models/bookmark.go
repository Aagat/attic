package models

import (
	"crypto/sha1"
	"encoding/hex"
	"strings"
	"time"
)

type Bookmark struct {
	Id          int          `json:"id"`
	Created     time.Time    `json:"created"`
	Updated     time.Time    `json:"last_updated"`
	Verified    time.Time    `json:"last_verified"`
	Title       string       `json:"title"`
	Description string       `json:"description"`
	Url         string       `json:"url"`
	Hash        string       `json:"hash"`
	Tags        []string     `json:"tags"`
	Alive       bool         `json:"alive"`
	Archived    bool         `json:"archived"`
	Meta        BookmarkMeta `json:"meta"`
}

type BookmarkMeta struct {
	Id          int       `json:"-"`
	Created     time.Time `json:"created"`
	Bookmark    string    `json:"-"`
	Title       string    `json:"title" meta:"og:title,title"`
	Description string    `json:"description" meta:"og:description,description"`
	RawKeywords string    `json:"-" meta:"keywords"`
	Keywords    []string  `json:"keywords"`
	Type        string    `json:"type" meta:"og:type"`
}

func (b *Bookmark) TagsToString() string {
	return strings.Join(b.Tags, ",")
}

func (b *Bookmark) TagsToArray(s string) {
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

	_, err = statement.Exec(b.Created, b.Updated, b.Verified, b.Title, b.Description, b.Url, b.Hash, b.TagsToString(), b.Alive, b.Archived)
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

func (b *BookmarkMeta) KeywordsToString() string {
	return strings.Join(b.Keywords, ",")
}

func (b *BookmarkMeta) KeywordsToArray(s string) {
	if len(s) != 0 {
		b.Keywords = strings.Split(s, ",")
	} else {
		b.Keywords = []string{}
	}
}

func (b *BookmarkMeta) Insert() error {
	statement, err := dbg.Prepare("INSERT OR REPLACE INTO bookmarks_meta (created, bookmark, title, description, keywords, type) VALUES (?,?,?,?,?,?)")
	if err != nil {
		return err
	}

	_, err = statement.Exec(time.Now(), b.Bookmark, b.Title, b.Description, b.KeywordsToString(), b.Type)
	if err != nil {
		return err
	}

	return nil
}
