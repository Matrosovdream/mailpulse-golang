package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Normalize runs before validation, so an omitted page or size must become a
// sane default rather than failing the min= tag.
func TestPageRequest_Normalize(t *testing.T) {
	cases := []struct {
		in   PageRequest
		page int
		size int
	}{
		{PageRequest{}, 1, 20},
		{PageRequest{Page: 0, Size: 0}, 1, 20},
		{PageRequest{Page: -5, Size: -1}, 1, 20},
		{PageRequest{Page: 3, Size: 50}, 3, 50},
		{PageRequest{Page: 1, Size: 1000}, 1, 100},
	}

	for _, testCase := range cases {
		request := testCase.in
		request.Normalize()

		assert.Equal(t, testCase.page, request.Page)
		assert.Equal(t, testCase.size, request.Size)
	}
}

func TestPageRequest_Offset(t *testing.T) {
	assert.Equal(t, 0, (&PageRequest{Page: 1, Size: 20}).Offset())
	assert.Equal(t, 20, (&PageRequest{Page: 2, Size: 20}).Offset())
	assert.Equal(t, 100, (&PageRequest{Page: 3, Size: 50}).Offset())
}

func TestNewPageMetadata(t *testing.T) {
	// a partial last page still counts as a page
	assert.Equal(t, int64(3), NewPageMetadata(1, 20, 41).TotalPage)
	assert.Equal(t, int64(2), NewPageMetadata(1, 20, 40).TotalPage)
	assert.Equal(t, int64(0), NewPageMetadata(1, 20, 0).TotalPage)

	metadata := NewPageMetadata(2, 20, 41)
	assert.Equal(t, 2, metadata.Page)
	assert.Equal(t, int64(41), metadata.TotalItem)
}

func TestAuth_Roles(t *testing.T) {
	auth := Auth{Roles: []string{"user", "superadmin"}}

	assert.True(t, auth.HasRole("user"))
	assert.True(t, auth.IsSuperadmin())
	plain := Auth{Roles: []string{"user"}}
	assert.False(t, plain.IsSuperadmin())

	empty := Auth{}
	assert.False(t, empty.IsSuperadmin())
}
