package search

import (
	"github.com/blevesearch/bleve"
	"github.com/blevesearch/bleve/analysis/lang/en"
	"github.com/blevesearch/bleve/mapping"
)

func buildMapping() mapping.IndexMapping {
	// Define type of indexing you want
	enFieldMapping := bleve.NewTextFieldMapping()
	enFieldMapping.Analyzer = en.AnalyzerName

	dateFieldMapping := bleve.NewDateTimeFieldMapping()

	// Define type of indexing you want for given field
	bookmarkMapping := bleve.NewDocumentMapping()
	bookmarkMapping.AddFieldMappingsAt("title", enFieldMapping)
	bookmarkMapping.AddFieldMappingsAt("tags", enFieldMapping)
	bookmarkMapping.AddFieldMappingsAt("notes", enFieldMapping)
	bookmarkMapping.AddFieldMappingsAt("created", dateFieldMapping)
	bookmarkMapping.AddFieldMappingsAt("last_updated", dateFieldMapping)
	bookmarkMapping.AddFieldMappingsAt("last_verified", dateFieldMapping)

	mapping := bleve.NewIndexMapping()
	mapping.DefaultMapping = bookmarkMapping
	mapping.DefaultAnalyzer = en.AnalyzerName

	return mapping
}
