package models

import (
	"database/sql"
	"github.com/aagat/attic/config"
	"log"
)

// Global reference for use within the package.
var dbg *sql.DB

type DB struct {
	DB *sql.DB
}

func NewDB(c *config.Config) (*DB, error) {
	dbg = c.DB.(*sql.DB)
	return &DB{DB: c.DB.(*sql.DB)}, nil
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

func (db *DB) UpdateBookmarkById(b *Bookmark) error {
	statement, err := db.DB.Prepare(`UPDATE bookmarks SET created=?,
updated=?, verified=?, title=?, description=?, url=?, hash=?, alive =? , archived=? WHERE id=?;`)
	if err != nil {
		return err
	}

	_, err = statement.Exec(b.Created, b.Updated, b.Verified, b.Title, b.Description, b.Url, b.Hash, b.Alive, b.Archived, b.Id)
	if err != nil {
		return err
	}

	return nil
}

func (db *DB) UpdateBookmarkByHash(b *Bookmark) error {
	statement, err := db.DB.Prepare(`UPDATE bookmarks SET created=?,
updated=?, verified=?, title=?, description=?, url=?, hash=?, alive =? , archived=? WHERE hash=?;`)
	if err != nil {
		return err
	}

	_, err = statement.Exec(b.Created, b.Updated, b.Verified, b.Title, b.Description, b.Url, b.Hash, b.Alive, b.Archived, b.Hash)
	if err != nil {
		return err
	}

	return nil
}
