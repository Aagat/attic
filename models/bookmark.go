package models

import (
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

func (b *Bookmark) Save() error {
	statement, err := dbg.Prepare("INSERT INTO bookmarks (created, updated, verified, title, description, url, hash, alive, archived) VALUES (?,?,?,?,?,?,?,?,?)")
	if err != nil {
		return err
	}

	_, err = statement.Exec(b.Created, b.Updated, b.Verified, b.Title, b.Description, b.Url, b.Hash, b.Alive, b.Archived)
	if err != nil {
		return err
	}

	return nil
}
