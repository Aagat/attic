package models

import (
	"crypto/sha1"
	"encoding/hex"
	"log"
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

func (db *DB) GetAllBookmarks() (*[]Bookmark, error) {

	bookmarks := []Bookmark{}

	rows, err := db.DB.Query("select * from bookmarks")

	if err != nil {
		log.Fatal(err)
	}

	defer rows.Close()

	for rows.Next() {
		b := Bookmark{}
		err := rows.Scan(
			&b.Id,
			&b.Created,
			&b.Updated,
			&b.Verified,
			&b.Title,
			&b.Description,
			&b.Url,
			&b.Hash,
			&b.Alive,
			&b.Archived)

		if err != nil {
			log.Fatal(err)
		}

		bookmarks = append(bookmarks, b)
	}

	err = rows.Err()
	if err != nil {
		log.Fatal(err)
	}

	return &bookmarks, nil
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

func (b *Bookmark) FillMissing() {
	hash := sha1.New()
	hash.Write([]byte(b.Url))
	b.Hash = hex.EncodeToString(hash.Sum(nil))

	b.Created = time.Now()
	b.Updated = time.Now()
	b.Verified = time.Now()
	b.Alive = true
	b.Archived = false
}

func (db *DB) GetBookmarkById(id int) (*Bookmark, error) {
	b := Bookmark{}
	statement, err := db.DB.Prepare("select * from bookmarks where id = ?")
	if err != nil {
		return nil, err
	}

	err = statement.QueryRow(id).Scan(
		&b.Id,
		&b.Created,
		&b.Updated,
		&b.Verified,
		&b.Title,
		&b.Description,
		&b.Url,
		&b.Hash,
		&b.Alive,
		&b.Archived,
	)

	if err != nil {
		return nil, err
	}

	return &b, nil
}

func (db *DB) GetBookmarkByHash(hash string) (*Bookmark, error) {
	b := Bookmark{}
	statement, err := db.DB.Prepare("select * from bookmarks where hash = ?")
	if err != nil {
		return nil, err
	}

	err = statement.QueryRow(hash).Scan(
		&b.Id,
		&b.Created,
		&b.Updated,
		&b.Verified,
		&b.Title,
		&b.Description,
		&b.Url,
		&b.Hash,
		&b.Alive,
		&b.Archived,
	)

	if err != nil {
		return nil, err
	}

	return &b, nil
}
