// SPDX-License-Identifier: MIT

package document

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAttachment_IsZero(t *testing.T) {
	assert.True(t, Attachment{}.IsZero())
	assert.True(t, (Attachment{Mime: "image/jpeg"}).IsZero(), "Mime alone is still zero")
	assert.False(t, (Attachment{StoragePath: "x"}).IsZero())
	assert.False(t, (Attachment{Size: 1}).IsZero())
}

// TestAttachment_EmbeddedStructComposes confirms Attachment plays nicely
// with the other document embeds — no field collisions when users compose
// Base + SoftDelete + Attachment on a single struct.
func TestAttachment_EmbeddedStructComposes(t *testing.T) {
	type Media struct {
		Base
		SoftDelete
		Attachment
		AltText string
	}

	var m Media
	m.ID = "media-1"
	m.StoragePath = "2026/04/abc.jpg"
	m.Mime = "image/jpeg"
	m.Size = 2048
	m.AltText = "a photo"

	assert.Equal(t, "media-1", m.ID)
	assert.Equal(t, "2026/04/abc.jpg", m.StoragePath)
	assert.False(t, m.IsZero())
	assert.False(t, m.IsDeleted())
}
