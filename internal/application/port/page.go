package port

type Page[T any] struct {
	Items      []T
	Page       int
	PageSize   int
	Total      int
	NextCursor string
}
