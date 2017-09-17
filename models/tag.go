package models

type Tag struct {
	Id         int    `json:"-"`
	BookmarkID int    `json:"bookmark_id"`
	Tag        string `json:"tag"`
}
