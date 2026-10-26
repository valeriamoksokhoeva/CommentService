package store

import (
	"comment_service/internal/domain"
	"context"
	"strings"

	"sync"
	"unicode/utf8"
)

type MemoryStore struct {
	mu sync.RWMutex

	posts          map[string]domain.Post
	comments       map[string]domain.Comment
	commentsByPost map[string][]string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		posts:          make(map[string]domain.Post),
		comments:       make(map[string]domain.Comment),
		commentsByPost: make(map[string][]string),
	}
}

func (s *MemoryStore) CreatePost(ctx context.Context, post domain.Post) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.posts[post.ID]; ok {
		return domain.ErrPostExists
	}

	s.posts[post.ID] = post
	return nil
}

func (s *MemoryStore) GetPost(ctx context.Context, postID string) (domain.Post, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	post, ok := s.posts[postID]
	if !ok {
		return domain.Post{}, domain.ErrPostNotFound
	}
	return post, nil
}

func (s *MemoryStore) CreateComment(ctx context.Context, comment domain.Comment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(comment.Message) == "" {
		return domain.ErrEmptyMessage
	}

	if strings.TrimSpace(comment.Author) == "" {
		return domain.ErrEmptyAuthor
	}

	if utf8.RuneCountInString(comment.Message) > 2000 {
		return domain.ErrCommentTooLong
	}

	post, ok := s.posts[comment.PostID]
	if !ok {
		return domain.ErrPostNotFound
	}
	if !post.CanComment {
		return domain.ErrCommentsForbidden
	}

	if comment.ParentID != nil {
		parent, ok := s.comments[*comment.ParentID]
		if !ok {
			return domain.ErrCommentNotFound
		}

		if parent.PostID != comment.PostID {
			return domain.ErrCommentsNoMatch
		}
	}

	if _, ok = s.comments[comment.ID]; ok {
		return domain.ErrCommentExists
	}
	s.comments[comment.ID] = comment
	s.commentsByPost[comment.PostID] = append(s.commentsByPost[comment.PostID], comment.ID)
	return nil
}

func (s *MemoryStore) GetCommentTree(ctx context.Context, postID string, limit int, offset int) ([]domain.CommentTree, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	_, ok := s.posts[postID]
	if !ok {
		return nil, domain.ErrPostNotFound
	}

	comments := s.commentsByPost[postID]

	temporaryTrees := make(map[string]*domain.CommentTree)
	res := []domain.CommentTree{}

	for _, commID := range comments {
		comm := s.comments[commID]
		temporaryTrees[commID] = &domain.CommentTree{Comment: comm, Replies: []*domain.CommentTree{}}
	}

	for _, commID := range comments {
		node := temporaryTrees[commID]
		comm := node.Comment

		if comm.ParentID != nil {
			parent, ok := temporaryTrees[*comm.ParentID]
			if !ok {
				return nil, domain.ErrCommentNotFound
			}
			parent.Replies = append(parent.Replies, node)

		}
	}

	for _, commID := range comments {
		node := temporaryTrees[commID]
		comm := node.Comment
		if comm.ParentID == nil {
			res = append(res, *node)
		}
	}

	start := offset
	if start >= len(res) {
		return []domain.CommentTree{}, nil
	}

	end := start + limit
	if end > len(res) {
		end = len(res)
	}

	return res[start:end], nil
}

func (s *MemoryStore) Posts(ctx context.Context) ([]domain.Post, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	posts := make([]domain.Post, 0, len(s.posts))
	for _, post := range s.posts {
		posts = append(posts, post)
	}

	return posts, nil
}

func (s *MemoryStore) DisableComments(ctx context.Context, postID string, author string) (domain.Post, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	post, ok := s.posts[postID]
	if !ok {
		return domain.Post{}, domain.ErrPostNotFound
	}
	if post.Author != author {
		return domain.Post{}, domain.ErrNotPostAuthor
	}

	post.CanComment = false
	s.posts[postID] = post

	return post, nil
}
