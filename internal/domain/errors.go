package domain

import "errors"

var (
	ErrPostExists        = errors.New("post with this id already exists")
	ErrPostNotFound      = errors.New("post not found")
	ErrCommentsForbidden = errors.New("forbidden to write comments for this post")
	ErrCommentNotFound   = errors.New("comment not found")
	ErrCommentExists     = errors.New("comment already exists")
	ErrCommentsNoMatch   = errors.New("comment and its parent comment have different postID")
	ErrCommentTooLong    = errors.New("length of comment should be less than 2000 symbols")
	ErrInvalidPagination = errors.New("invalid pagination")
	ErrNotPostAuthor     = errors.New("only author's post may disable comments")
	ErrEmptyMessage      = errors.New("message must not be empty")
	ErrEmptyAuthor       = errors.New("author must not be empty")
)
