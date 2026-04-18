package den

import "reflect"

// Settings configures per-collection behavior.
type Settings struct {
	CollectionName string
	UseRevision    bool
	Indexes        []IndexDefinition
}

// DenSettable is implemented by document types that provide custom settings.
type DenSettable interface {
	DenSettings() Settings
}

// getSettings extracts Settings from a document type, if it implements
// DenSettable. When the user passes a value but DenSettings is defined on
// a pointer receiver the direct assertion misses, so we synthesize a
// pointer to the value and retry before giving up.
func getSettings(docType any) Settings {
	if s, ok := docType.(DenSettable); ok {
		return s.DenSettings()
	}

	rv := reflect.ValueOf(docType)
	if !rv.IsValid() || rv.Kind() == reflect.Pointer {
		return Settings{}
	}
	ptr := reflect.New(rv.Type())
	ptr.Elem().Set(rv)
	if s, ok := ptr.Interface().(DenSettable); ok {
		return s.DenSettings()
	}
	return Settings{}
}
