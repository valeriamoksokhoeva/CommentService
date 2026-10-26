package domain

type Post struct {
	ID         string
	Message    string
	Author     string
	CanComment bool
}

type Comment struct {
	ID       string
	PostID   string
	ParentID *string
	Message  string
	Author   string
}

type CommentTree struct {
	Comment Comment
	Replies []*CommentTree
}
