package search

import (
	"github.com/blevesearch/bleve"
	"log"
)

type Index struct {
	Index bleve.Index
}

func OpenIndex(path string) (*Index, error) {

	mapping := buildMapping()
	var err error
	index, err := bleve.New(path, mapping)

	if err != bleve.ErrorIndexPathExists && err != nil {
		return nil, err
	}

	return &Index{Index: index}, nil
}

func (i *Index) Add(key string, val interface{}) {
	i.Index.Index(key, val)
}
