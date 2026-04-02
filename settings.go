package den

import "time"

// Settings configures per-collection behavior.
type Settings struct {
	CollectionName       string
	OmitEmpty            bool
	UseRevision          bool
	UseCache             bool
	CacheCapacity        int
	CacheExpiration      time.Duration
	NestingDepthPerField map[string]int
	Indexes              []IndexDefinition
}

// DenSettable is implemented by document types that provide custom settings.
type DenSettable interface {
	DenSettings() Settings
}

// getSettings extracts Settings from a document type, if it implements DenSettable.
func getSettings(docType any) Settings {
	if s, ok := docType.(DenSettable); ok {
		return s.DenSettings()
	}
	return Settings{}
}
