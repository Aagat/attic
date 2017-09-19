package models

import (
	"database/sql"
	"github.com/aagat/attic/config"
	"log"
)

// Global reference for use within the package.
var dbg *sql.DB

type Models struct {
	DB *sql.DB
}

func Init(c *config.Config) (*Models, error) {
	dbg = c.DB.(*sql.DB)
	return &Models{DB: c.DB.(*sql.DB)}, nil
}

func (m *Models) GetAllBookmarks() (*[]Bookmark, error) {

	bookmarks := []Bookmark{}

	rows, err := m.DB.Query("select * from bookmarks")

	if err != nil {
		log.Fatal(err)
	}

	defer rows.Close()

	for rows.Next() {
		b := Bookmark{}
		var tags string
		err := rows.Scan(
			&b.Id,
			&b.Created,
			&b.Updated,
			&b.Verified,
			&b.Title,
			&b.Description,
			&b.Url,
			&b.Hash,
			&tags,
			&b.Alive,
			&b.Archived)

		if err != nil {
			log.Fatal(err)
		}

		b.UnmarshalTags(tags)

		bookmarks = append(bookmarks, b)
	}

	err = rows.Err()
	if err != nil {
		log.Fatal(err)
	}

	return &bookmarks, nil
}

func (m *Models) GetBookmarkById(id int) (*Bookmark, error) {
	b := Bookmark{}
	var tags string
	statement, err := m.DB.Prepare("select * from bookmarks where id = ?")
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
		&tags,
		&b.Alive,
		&b.Archived,
	)

	b.UnmarshalTags(tags)

	if err != nil {
		return nil, err
	}

	return &b, nil
}

func (m *Models) GetBookmarkByHash(hash string) (*Bookmark, error) {
	b := Bookmark{}
	var tags string
	statement, err := m.DB.Prepare("select * from bookmarks where hash = ?")
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
		&tags,
		&b.Alive,
		&b.Archived,
	)

	b.UnmarshalTags(tags)

	if err != nil {
		return nil, err
	}

	return &b, nil
}

func (m *Models) UpdateBookmarkById(b *Bookmark) error {
	statement, err := m.DB.Prepare(`UPDATE bookmarks SET created=?,
updated=?, verified=?, title=?, description=?, url=?, hash=?, tags=?, alive =? , archived=? WHERE id=?;`)
	if err != nil {
		return err
	}

	_, err = statement.Exec(b.Created, b.Updated, b.Verified, b.Title, b.Description, b.Url, b.Hash, b.MarshalTags(), b.Alive, b.Archived, b.Id)
	if err != nil {
		return err
	}

	return nil
}

func (m *Models) UpdateBookmarkByHash(b *Bookmark) error {
	statement, err := m.DB.Prepare(`UPDATE bookmarks SET created=?,
updated=?, verified=?, title=?, description=?, url=?, hash=?, tags=?, alive =? , archived=? WHERE hash=?;`)
	if err != nil {
		return err
	}

	_, err = statement.Exec(b.Created, b.Updated, b.Verified, b.Title, b.Description, b.Url, b.Hash, b.MarshalTags(), b.Alive, b.Archived, b.Hash)
	if err != nil {
		return err
	}

	return nil
}

func (m *Models) DeleteBookmarkById(id int) error {
	statement, err := m.DB.Prepare(`DELETE FROM bookmarks WHERE id=?`)
	if err != nil {
		return err
	}

	_, err = statement.Exec(id)
	if err != nil {
		return err
	}

	return nil

}

func (m *Models) DeleteBookmarkByHash(hash string) error {
	statement, err := m.DB.Prepare(`DELETE FROM bookmarks WHERE hash=?`)
	if err != nil {
		return err
	}

	_, err = statement.Exec(hash)
	if err != nil {
		return err
	}

	return nil

}
