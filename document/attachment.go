// SPDX-License-Identifier: MIT

package document

// Attachment is an embeddable document field that references a single file
// stored via a den.Storage backend.
//
// Typical use: embed directly when the containing document IS a file, or
// add it as a named field when a document HAS one or more files.
//
//	// IS a file — Attachment embedded
//	type Media struct {
//	    document.Base
//	    document.Attachment
//	    AltText string
//	}
//
//	// HAS files — Attachment as named fields
//	type Product struct {
//	    document.Base
//	    Hero      document.Attachment
//	    Thumbnail document.Attachment
//	}
//
// The fields are populated by den.Storage.Store and are never edited by hand
// afterwards — StoragePath, SHA256, and Size are intrinsic to the stored
// bytes. Mime may be overridden by application code if the detected type is
// wrong, but changing it does not re-hash or move the file.
//
// When a document that embeds or contains an Attachment is hard-deleted,
// Den asks the configured Storage to remove the referenced bytes.
type Attachment struct {
	StoragePath string `json:"storage_path"     validate:"required,max=500"`
	Mime        string `json:"mime"             validate:"required,max=100"`
	Size        int64  `json:"size"             validate:"required,min=1"`
	SHA256      string `json:"sha256,omitempty" validate:"omitempty,len=64"`
}

// IsZero reports whether the Attachment has no content — StoragePath unset
// and Size zero. Used to distinguish "no file attached yet" from a pointer
// to a real file when the field is embedded by value.
func (a Attachment) IsZero() bool {
	return a.StoragePath == "" && a.Size == 0
}
