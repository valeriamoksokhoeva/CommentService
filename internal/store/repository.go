package store

import (
	"comment_service/internal/domain"
	"context"
)

type Repository interface {
	CreatePost(ctx context.Context, post domain.Post) error
	GetPost(ctx context.Context, postID string) (domain.Post, error)
	CreateComment(ctx context.Context, comment domain.Comment) error
	GetCommentTree(ctx context.Context, postID string, limit int, offset int) ([]domain.CommentTree, error)
	Posts(ctx context.Context) ([]domain.Post, error)
	DisableComments(ctx context.Context, postID string, author string) (domain.Post, error)
}
