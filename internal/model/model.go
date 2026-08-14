package model

type WebResponse[T any] struct {
	Data   T             `json:"data"`
	Paging *PageMetadata `json:"paging,omitempty"`
	Errors string        `json:"errors,omitempty"`
}

type PageResponse[T any] struct {
	Data         []T          `json:"data,omitempty"`
	PageMetadata PageMetadata `json:"paging,omitempty"`
}

type PageMetadata struct {
	Page      int   `json:"page"`
	Size      int   `json:"size"`
	TotalItem int64 `json:"total_item"`
	TotalPage int64 `json:"total_page"`
}

// PageRequest is embedded by every list request. Normalize must run before
// validation so an omitted page or size does not fail the min tag.
type PageRequest struct {
	Page int `json:"page" validate:"min=1"`
	Size int `json:"size" validate:"min=1,max=100"`
}

func (p *PageRequest) Normalize() {
	if p.Page < 1 {
		p.Page = 1
	}
	if p.Size < 1 {
		p.Size = 20
	}
	if p.Size > 100 {
		p.Size = 100
	}
}

func (p *PageRequest) Offset() int {
	return (p.Page - 1) * p.Size
}

func NewPageMetadata(page, size int, total int64) PageMetadata {
	totalPage := total / int64(size)
	if total%int64(size) > 0 {
		totalPage++
	}
	return PageMetadata{Page: page, Size: size, TotalItem: total, TotalPage: totalPage}
}
